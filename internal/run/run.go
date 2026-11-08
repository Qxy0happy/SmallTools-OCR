package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mineru-ocr/internal/app"
	"mineru-ocr/internal/artifact"
	"mineru-ocr/internal/discover"
	"mineru-ocr/internal/mineru"
	"mineru-ocr/internal/report"
	"mineru-ocr/internal/service"
	"mineru-ocr/internal/state"
)

// Deps overrides the run's collaborators; nil fields use the real implementations.
type Deps struct {
	NewClient     func(baseURL string) *mineru.Client
	NewSupervisor func(opts service.Options) service.Supervisor
	CleanupStale  func(records []service.Record) error
}

// Run executes one pass over cfg.Roots and returns the process exit code.
func Run(ctx context.Context, cfg app.Config, deps ...Deps) int {
	var d Deps
	if len(deps) > 0 {
		d = deps[0]
	}
	if d.NewClient == nil {
		d.NewClient = mineru.New
	}
	if d.NewSupervisor == nil {
		d.NewSupervisor = service.New
	}

	warn := func(msg string) {
		fmt.Fprintln(cfg.Err, msg)
	}
	files, err := discover.Find(cfg.Roots, warn)
	if err != nil {
		fmt.Fprintln(cfg.Err, err)
		return 2
	}

	if cfg.DryRun {
		for _, f := range files {
			fmt.Fprintf(cfg.Err, "would process %s\n", f)
		}
		r := &report.Report{
			SchemaVersion: 1,
			Roots:         cfg.Roots,
			Scanned:       len(files),
			Files:         []report.FileResult{},
		}
		if cfg.JSON {
			if err := report.WriteJSON(cfg.Out, r); err != nil {
				fmt.Fprintln(cfg.Err, err)
				return 2
			}
		}
		return 0
	}

	// resolve roots to absolute for state association
	absRoots := make([]string, 0, len(cfg.Roots))
	for _, r := range cfg.Roots {
		a, err := filepath.Abs(r)
		if err != nil {
			a = r
		}
		absRoots = append(absRoots, filepath.Clean(a))
	}
	if cfg.ServiceLog == "" && len(absRoots) > 0 {
		cfg.ServiceLog = filepath.Join(absRoots[0], ".mineru-service.log")
	}

	// load state per root
	states := map[string]*state.State{}
	var stateMu sync.Mutex
	rootForFile := map[string]string{}
	for _, r := range absRoots {
		st, err := state.Load(r)
		if err != nil {
			fmt.Fprintln(cfg.Err, err)
			return 2
		}
		states[r] = st
	}
	for _, f := range files {
		for _, r := range absRoots {
			if isUnder(f, r) {
				rootForFile[f] = r
				break
			}
		}
		if rootForFile[f] == "" && len(absRoots) > 0 {
			rootForFile[f] = absRoots[0]
		}
	}

	// fingerprint + skip decision
	var toProcess []string
	skipped := 0
	skipMap := map[string]bool{}
	hashes := map[string]string{}
	sizes := map[string]int64{}
	for _, f := range files {
		h, sz, err := state.Fingerprint(f)
		if err != nil {
			fmt.Fprintln(cfg.Err, err)
			return 2
		}
		hashes[f] = h
		sizes[f] = sz
		r := rootForFile[f]
		st := states[r]
		if st.ShouldSkip(f, h) {
			skipped++
			skipMap[f] = true
		} else {
			toProcess = append(toProcess, f)
		}
	}

	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()

	if d.CleanupStale == nil {
		d.CleanupStale = func(records []service.Record) error { return service.CleanupStale(records) }
	}

	// clear any service instance left behind by a previous crashed run
	var stale []service.Record
	for _, r := range absRoots {
		if st, ok := states[r]; ok && st.Service != nil {
			stale = append(stale, service.Record{PID: st.Service.PID, Port: st.Service.Port})
		}
	}
	if len(stale) > 0 {
		if err := d.CleanupStale(stale); err != nil {
			fmt.Fprintf(cfg.Err, "warning: stale service cleanup: %v\n", err)
		}
		for _, r := range absRoots {
			if st, ok := states[r]; ok && st.Service != nil {
				st.Service = nil
				_ = st.Save(r)
			}
		}
	}

	// service options
	logStream := cfg.Err
	if cfg.NoServiceLogStream {
		logStream = nil
	}
	opts := service.Options{
		Project:   cfg.Project,
		Tier:      cfg.Tier,
		Workers:   cfg.Workers,
		Port:      cfg.Port,
		LogPath:   cfg.ServiceLog,
		LogStream: logStream,
	}
	sup := d.NewSupervisor(opts)
	svcInfo, err := sup.Start(ctx)
	if err != nil {
		if cfg.ServiceLog != "" {
			for _, l := range service.ReadLogTail(cfg.ServiceLog, 50) {
				fmt.Fprintln(cfg.Err, l)
			}
		}
		fmt.Fprintln(cfg.Err, err)
		return 2
	}
	if svcInfo.PID > 0 {
		stateMu.Lock()
		for _, r := range absRoots {
			st := states[r]
			st.Service = &state.ServiceRecord{PID: svcInfo.PID, Port: svcInfo.Port, StartedAt: time.Now()}
			_ = st.Save(r)
		}
		stateMu.Unlock()
	}

	client := d.NewClient(fmt.Sprintf("http://127.0.0.1:%d", svcInfo.Port))

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}

	// health monitor
	lostCh := make(chan struct{})
	var lostOnce sync.Once
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		failures := 0
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				hctx, ccancel := context.WithTimeout(ctx, 3*time.Second)
				herr := sup.Health(hctx)
				ccancel()
				if herr != nil {
					failures++
					if failures >= 3 {
						lostOnce.Do(func() { close(lostCh); cancel() })
						return
					}
				} else {
					failures = 0
				}
			}
		}
	}()
	// results collection
	results := make(map[string]report.FileResult)
	var resMu sync.Mutex
	completedCount := 0
	jobs := make(chan string, len(toProcess))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				fr := processFile(ctx, client, path, cfg, hashes[path], sizes[path], rootForFile[path], states, &stateMu)
				resMu.Lock()
				results[path] = fr
				completedCount++
				idx := completedCount
				p := report.Progress{Out: cfg.Err, Total: len(toProcess)}
				p.Line(idx, fr)
				resMu.Unlock()
			}
		}()
	}

	// dispatcher
	go func() {
		for _, p := range toProcess {
			select {
			case <-lostCh:
				close(jobs)
				return
			case <-ctx.Done():
				close(jobs)
				return
			case jobs <- p:
			}
		}
		close(jobs)
	}()

	// wait workers with 30s cap on lost
	waitDone := make(chan struct{})
	go func() { wg.Wait(); close(waitDone) }()
	select {
	case <-waitDone:
	case <-time.After(30 * time.Second):
	}

	// determine canceled
	canceled := ctx.Err() != nil

	// lost?
	lost := false
	select {
	case <-lostCh:
		lost = true
	default:
	}

	// stop service
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	_ = sup.Stop(stopCtx)
	svcInfo.Stopped = true
	if svcInfo.PID > 0 {
		stateMu.Lock()
		for _, r := range absRoots {
			if st, ok := states[r]; ok && st.Service != nil {
				st.Service = nil
				_ = st.Save(r)
			}
		}
		stateMu.Unlock()
	}

	// build report
	r := &report.Report{
		SchemaVersion: 1,
		Roots:         cfg.Roots,
		Scanned:       len(files),
		Skipped:       skipped,
		Service:       svcInfo,
		Files:         []report.FileResult{},
	}
	completed := 0
	failed := 0
	for _, f := range files {
		if skipMap[f] {
			zipPath := artifact.ZipPathFor(f)
			fr := report.FileResult{Path: f, Status: report.StatusSkipped, ZipPath: zipPath}
			r.Add(fr)
			continue
		}
		fr, ok := results[f]
		if !ok {
			fr = report.FileResult{Path: f, Status: report.StatusFailed, Error: &report.Error{Code: "not_processed", Message: "not processed"}}
		}
		r.Add(fr)
		if fr.Status == report.StatusCompleted {
			completed++
		} else if fr.Status == report.StatusFailed {
			failed++
		}
	}
	r.Completed = completed
	r.Failed = failed

	exitCode := 0
	if lost {
		exitCode = 2
	} else if canceled {
		exitCode = 130
	} else if failed > 0 {
		exitCode = 1
	}
	r.ExitCode = exitCode

	if cfg.JSON {
		if err := report.WriteJSON(cfg.Out, r); err != nil {
			fmt.Fprintln(cfg.Err, err)
			return 2
		}
	} else {
		report.Summary(cfg.Out, r)
	}

	return exitCode
}

func isUnder(file, root string) bool {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return !strings.HasPrefix(rel, "..")
}

func processFile(ctx context.Context, client *mineru.Client, path string, cfg app.Config, hash string, size int64, root string, states map[string]*state.State, smu *sync.Mutex) report.FileResult {
	start := time.Now()
	ext := strings.ToLower(filepath.Ext(path))
	fileTier := ""
	if ext == ".pdf" {
		fileTier = cfg.Tier
	}
	stateTier := fileTier
	if stateTier == "" {
		stateTier = "flash"
	}
	fail := func(err error) report.FileResult {
		fr := report.FileResult{Path: path, Status: report.StatusFailed, DurationMS: time.Since(start).Milliseconds()}
		if errors.Is(err, context.Canceled) {
			fr.Error = &report.Error{Code: "canceled", Message: "canceled"}
		} else if me, ok := err.(*mineru.Error); ok {
			fr.Error = &report.Error{Code: me.Code, Message: me.Message}
		} else if errors.Is(err, mineru.ErrSourceChanged) {
			fr.Error = &report.Error{Code: "source_changed", Message: "source file changed during upload"}
		} else if err != nil {
			fr.Error = &report.Error{Code: "error", Message: err.Error()}
		}
		// update state failed
		if st, ok := states[root]; ok {
			smu.Lock()
			if st.Files == nil {
				st.Files = map[string]state.FileRecord{}
			}
			st.Files[path] = state.FileRecord{Size: size, Hash: hash, Tier: stateTier, Status: "failed", Error: &state.Error{Code: fr.Error.Code, Message: fr.Error.Message}, DurationMS: fr.DurationMS, UpdatedAt: time.Now()}
			_ = st.Save(root)
			smu.Unlock()
		}
		return fr
	}

	// Create
	taskID, uploadURL, err := client.Create(ctx, filepath.Base(path), size, fileTier)
	if err != nil {
		if ctx.Err() != nil {
			cancelTask(client, taskID)
		}
		return fail(err)
	}
	// Upload
	if err := client.Upload(ctx, uploadURL, path); err != nil {
		if ctx.Err() != nil {
			cancelTask(client, taskID)
		}
		return fail(err)
	}
	// Poll
	var resultURL string
	pollStart := time.Now()
poll:
	for {
		status, detail, err := client.Poll(ctx, taskID)
		if err != nil {
			if ctx.Err() != nil {
				cancelTask(client, taskID)
				return fail(context.Canceled)
			}
			return fail(err)
		}
		if detail.ResultURL != "" {
			resultURL = detail.ResultURL
		}
		current := status
		if current == "" {
			current = detail.Status
		}
		switch current {
		case "completed":
			if resultURL == "" {
				return fail(&mineru.Error{Code: "missing_result", Message: "task completed without result_url"})
			}
			break poll
		case "failed", "canceled":
			code, msg := "task_"+current, "task "+current
			if detail.Error != nil && detail.Error.Code != "" {
				code, msg = detail.Error.Code, detail.Error.Message
			}
			return fail(&mineru.Error{Code: code, Message: msg})
		}
		delay := time.Second
		if time.Since(pollStart) > 60*time.Second {
			delay = 5 * time.Second
		}
		select {
		case <-ctx.Done():
			cancelTask(client, taskID)
			return fail(context.Canceled)
		case <-time.After(delay):
		}
	}
	// Download
	zipPath := artifact.ZipPathFor(path)
	partPath := zipPath + ".part"
	if err := client.Download(ctx, resultURL, partPath); err != nil {
		_ = os.Remove(partPath)
		if ctx.Err() != nil {
			cancelTask(client, taskID)
		}
		return fail(err)
	}
	if err := artifact.Validate(partPath); err != nil {
		_ = os.Remove(partPath)
		return fail(err)
	}
	if err := artifact.Write(partPath, zipPath); err != nil {
		_ = os.Remove(partPath)
		return fail(err)
	}

	fr := report.FileResult{Path: path, Status: report.StatusCompleted, ZipPath: zipPath, DurationMS: time.Since(start).Milliseconds()}
	// update state
	if st, ok := states[root]; ok {
		smu.Lock()
		if st.Files == nil {
			st.Files = map[string]state.FileRecord{}
		}
		st.Files[path] = state.FileRecord{Size: size, Hash: hash, Tier: stateTier, ZipPath: zipPath, Status: "completed", DurationMS: fr.DurationMS, UpdatedAt: time.Now()}
		_ = st.Save(root)
		smu.Unlock()
	}
	return fr
}

func cancelTask(client *mineru.Client, taskID string) {
	if taskID == "" {
		return
	}
	cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = client.Cancel(cctx, taskID)
}
