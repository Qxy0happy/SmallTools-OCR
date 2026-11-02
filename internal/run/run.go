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

type Deps struct {
	NewClient     func(baseURL string) *mineru.Client
	NewSupervisor func(opts service.Options) service.Supervisor
}

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

	// load state per root
	states := map[string]*state.State{}
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
	type fileInfo struct {
		path string
		hash string
		size int64
	}
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
						lostOnce.Do(func() { close(lostCh) })
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
	jobs := make(chan string, len(toProcess))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				if ctx.Err() != nil {
					// still attempt cancel handling inside processFile
				}
				fr := processFile(ctx, client, path, cfg, hashes[path], sizes[path], rootForFile[path], states, &stateMu)
				resMu.Lock()
				results[path] = fr
				resMu.Unlock()

				// progress line
				// index among all files
				idx := indexOf(files, path) + 1
				p := report.Progress{Out: cfg.Err, Total: len(files)}
				p.Line(idx, fr)
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
	svcInfo.Stopped = true
	_ = sup.Stop(stopCtx)

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
	if canceled {
		exitCode = 130
	} else if lost {
		exitCode = 2
	} else if failed > 0 {
		exitCode = 1
	}
	r.ExitCode = exitCode

	if cfg.JSON {
		if err := report.WriteJSON(cfg.Out, r); err != nil {
			fmt.Fprintln(cfg.Err, err)
		}
	} else {
		report.Summary(cfg.Out, r)
	}

	return exitCode
}

var stateMu sync.Mutex

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

func indexOf(files []string, path string) int {
	for i, f := range files {
		if f == path {
			return i
		}
	}
	return 0
}

func processFile(ctx context.Context, client *mineru.Client, path string, cfg app.Config, hash string, size int64, root string, states map[string]*state.State, smu *sync.Mutex) report.FileResult {
	start := time.Now()
	fail := func(err error) report.FileResult {
		fr := report.FileResult{Path: path, Status: report.StatusFailed, DurationMS: time.Since(start).Milliseconds()}
		if errors.Is(err, context.Canceled) {
			fr.Error = &report.Error{Code: "canceled", Message: "canceled"}
		} else if me, ok := err.(*mineru.Error); ok {
			fr.Error = &report.Error{Code: me.Code, Message: me.Message}
		} else if err != nil {
			fr.Error = &report.Error{Code: "error", Message: err.Error()}
		}
		// update state failed
		if st, ok := states[root]; ok {
			smu.Lock()
			if st.Files == nil {
				st.Files = map[string]state.FileRecord{}
			}
			st.Files[path] = state.FileRecord{Size: size, Hash: hash, Tier: cfg.Tier, Status: "failed", Error: &state.Error{Code: fr.Error.Code, Message: fr.Error.Message}, DurationMS: fr.DurationMS, UpdatedAt: time.Now()}
			smu.Unlock()
			_ = st.Save(root)
		}
		return fr
	}

	ext := strings.ToLower(filepath.Ext(path))
	fileTier := ""
	if ext == ".pdf" {
		fileTier = cfg.Tier
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
	for {
		status, detail, err := client.Poll(ctx, taskID)
		if err != nil {
			if ctx.Err() != nil {
				cancelTask(client, taskID)
			}
			return fail(err)
		}
		if detail.Error != nil {
			err := &mineru.Error{Code: detail.Error.Code, Message: detail.Error.Message}
			return fail(err)
		}
		if detail.ResultURL != "" {
			resultURL = detail.ResultURL
		}
		if status == "completed" || detail.Status == "completed" {
			if resultURL == "" {
				// maybe not ready
			} else {
				break
			}
		}
		if status == "failed" || detail.Status == "failed" {
			code := "task_failed"
			msg := "task failed"
			if detail.Error != nil {
				code = detail.Error.Code
				msg = detail.Error.Message
			}
			return fail(&mineru.Error{Code: code, Message: msg})
		}
		if resultURL != "" && (status == "completed" || detail.Status == "completed") {
			break
		}
		delay := 1 * time.Second
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
	if resultURL == "" {
		// poll one more? just fail
	}
	// Download
	zipPath := artifact.ZipPathFor(path)
	partPath := zipPath + ".part"
	if err := client.Download(ctx, resultURL, partPath); err != nil {
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
		return fail(err)
	}

	fr := report.FileResult{Path: path, Status: report.StatusCompleted, ZipPath: zipPath, DurationMS: time.Since(start).Milliseconds()}
	// update state
	if st, ok := states[root]; ok {
		smu.Lock()
		if st.Files == nil {
			st.Files = map[string]state.FileRecord{}
		}
		st.Files[path] = state.FileRecord{Size: size, Hash: hash, Tier: cfg.Tier, ZipPath: zipPath, Status: "completed", DurationMS: fr.DurationMS, UpdatedAt: time.Now()}
		smu.Unlock()
		_ = st.Save(root)
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
