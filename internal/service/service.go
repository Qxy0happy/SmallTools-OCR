package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"mineru-ocr/internal/report"
)

// ErrUnsupportedPlatform is returned where process control is not implemented.
var ErrUnsupportedPlatform = errors.New("service: unsupported platform")

var platformSupported = true

// Options configures the supervised service process.
type Options struct {
	Project   string
	Tier      string
	Workers   int
	Port      int
	LogPath   string
	LogStream io.Writer
}

// Record is a service instance recorded in a root's state file.
type Record struct {
	PID  int
	Port int
}

// Supervisor starts, probes and stops the MinerU service.
type Supervisor interface {
	Start(ctx context.Context) (report.ServiceInfo, error)
	Health(ctx context.Context) error
	Stop(ctx context.Context) error
}

// CleanupDeps overrides the probes used by CleanupStale; nil fields use platform defaults.
type CleanupDeps struct {
	PidAlive    func(int) bool
	Health      func(int) bool
	CommandLine func(int) (string, error)
	Kill        func(int) error
}

// LineWriter prefixes every complete line written to W.
type LineWriter struct {
	Prefix string
	W      io.Writer
	mu     sync.Mutex
	buf    []byte
}

// NewLineWriter returns a writer that prefixes each line for w.
func NewLineWriter(prefix string, w io.Writer) *LineWriter {
	return &LineWriter{Prefix: prefix, W: w}
}

func (lw *LineWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	lw.buf = append(lw.buf, p...)
	for {
		idx := -1
		for i, b := range lw.buf {
			if b == '\n' {
				idx = i
				break
			}
		}
		if idx < 0 {
			break
		}
		line := string(lw.buf[:idx])
		lw.buf = lw.buf[idx+1:]
		if lw.W != nil {
			if _, err := fmt.Fprintf(lw.W, "%s%s\n", lw.Prefix, line); err != nil {
				return len(p), err
			}
			if f, ok := lw.W.(interface{ Flush() }); ok {
				f.Flush()
			}
		}
	}
	return len(p), nil
}

// FreePort reserves and releases a 127.0.0.1 port, returning its number.
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected addr type")
	}
	return addr.Port, nil
}

var (
	lookPath    = exec.LookPath
	execCommand = exec.Command
)

// UploadDir is the service upload directory for the given port.
func UploadDir(port int) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("mineru-ocr-%d", port))
}

// BuildCommandArgs returns the uv run arguments that start the service on port.
func BuildCommandArgs(opts Options, port int) []string {
	return []string{
		"run", "--project", opts.Project,
		"mineru-kit", "api-server",
		"--port", strconv.Itoa(port),
		"--upload-dir", UploadDir(port),
		"--tier", opts.Tier,
		"--concurrency", strconv.Itoa(opts.Workers),
		"--preload-models",
	}
}

func healthURL(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d/v1/health", port)
}

// CheckHealth calls GET /v1/health with a one second timeout.
func CheckHealth(ctx context.Context, port int) error {
	cctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, healthURL(port), nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %d", resp.StatusCode)
	}
	return nil
}

// WaitReady polls until ready or 120s elapse, returning the ready time in ms.
func WaitReady(ctx context.Context, port int) (int64, error) {
	return WaitReadyTimeout(ctx, port, 120*time.Second)
}

// WaitReadyTimeout is WaitReady with an explicit readiness timeout.
func WaitReadyTimeout(ctx context.Context, port int, timeout time.Duration) (int64, error) {
	start := time.Now()
	deadline := start.Add(timeout)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := CheckHealth(ctx, port); err == nil {
			return time.Since(start).Milliseconds(), nil
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("service not ready within %s", timeout)
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-ticker.C:
		}
	}
}

var waitReady = WaitReady

// ClassifyExit maps a startup failure to a retry decision or a fatal error.
func ClassifyExit(code int, tail string, project ...string) (bool, error) {
	proj := "<project>"
	if len(project) > 0 && project[0] != "" {
		proj = project[0]
	}
	if code == 3 || strings.Contains(tail, "address already in use") || strings.Contains(tail, "WinError 10048") {
		return true, fmt.Errorf("port in use")
	}
	if code == 1 && strings.Contains(tail, "Unsupported tier") {
		return false, fmt.Errorf("unsupported tier")
	}
	msg := strings.TrimSpace(tail)
	if msg == "" {
		msg = "no log output"
	}
	return false, fmt.Errorf("service failed to start; run `uv run --project %s mineru-kit models download`: %s", proj, msg)
}

// ReadLogTail returns the last n lines of the service log at path.
func ReadLogTail(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func readLogTail(path string, n int) string {
	lines := ReadLogTail(path, n)
	return strings.Join(lines, "\n")
}

// StaleMatch reports whether rec is a leftover service started by this tool.
func StaleMatch(rec Record, alive func(int) bool, hlth func(int) bool, cmd func(int) (string, error)) bool {
	if alive == nil {
		alive = defaultPidAlive
	}
	if hlth == nil {
		hlth = defaultHealth
	}
	if cmd == nil {
		cmd = defaultCommandLine
	}
	if !alive(rec.PID) {
		return false
	}
	if !hlth(rec.Port) {
		return false
	}
	cl, err := cmd(rec.PID)
	if err != nil {
		return false
	}
	if !strings.Contains(cl, "mineru-kit") {
		return false
	}
	if !strings.Contains(cl, "api-server") {
		return false
	}
	if !strings.Contains(cl, fmt.Sprintf("--port %d", rec.Port)) {
		return false
	}
	return true
}

// CleanupStale kills the leftovers among records that match all three stale criteria.
func CleanupStale(records []Record, deps ...any) error {
	if !platformSupported {
		return ErrUnsupportedPlatform
	}
	d := resolveCleanupDeps(deps)
	seen := map[int]bool{}
	var errs []string
	for _, rec := range records {
		if seen[rec.PID] {
			continue
		}
		seen[rec.PID] = true
		aliveFn := d.PidAlive
		if aliveFn == nil {
			aliveFn = defaultPidAlive
		}
		healthFn := d.Health
		if healthFn == nil {
			healthFn = defaultHealth
		}
		cmdFn := d.CommandLine
		if cmdFn == nil {
			cmdFn = defaultCommandLine
		}
		killFn := d.Kill
		if killFn == nil {
			killFn = defaultKill
		}
		hlthBool := func(port int) bool { return healthFn(port) }
		match := StaleMatch(rec, aliveFn, hlthBool, cmdFn)
		if !match {
			continue
		}
		if err := killFn(rec.PID); err != nil {
			errs = append(errs, fmt.Sprintf("pid %d: %v", rec.PID, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("cleanup: %s", strings.Join(errs, "; "))
	}
	return nil
}

func resolveCleanupDeps(deps []any) CleanupDeps {
	out := CleanupDeps{}
	if len(deps) == 0 {
		return out
	}
	if len(deps) == 1 {
		switch v := deps[0].(type) {
		case CleanupDeps:
			return v
		case *CleanupDeps:
			if v != nil {
				return *v
			}
		case map[string]any:
			if f, ok := v["pidAlive"].(func(int) bool); ok {
				out.PidAlive = f
			}
			if f, ok := v["PidAlive"].(func(int) bool); ok {
				out.PidAlive = f
			}
			if f, ok := v["health"].(func(int) bool); ok {
				out.Health = f
			}
			if f, ok := v["Health"].(func(int) bool); ok {
				out.Health = f
			}
			if f, ok := v["commandLine"].(func(int) (string, error)); ok {
				out.CommandLine = f
			}
			if f, ok := v["CommandLine"].(func(int) (string, error)); ok {
				out.CommandLine = f
			}
			if f, ok := v["kill"].(func(int) error); ok {
				out.Kill = f
			}
			if f, ok := v["Kill"].(func(int) error); ok {
				out.Kill = f
			}
			if f, ok := v["health"].(func(int) error); ok {
				out.Health = func(port int) bool { return f(port) == nil }
			}
			if f, ok := v["Health"].(func(int) error); ok {
				out.Health = func(port int) bool { return f(port) == nil }
			}
			return out
		}
	}
	if len(deps) >= 1 {
		if f, ok := deps[0].(func(int) bool); ok {
			out.PidAlive = f
		}
	}
	if len(deps) >= 2 {
		switch f := deps[1].(type) {
		case func(int) bool:
			out.Health = f
		case func(int) error:
			out.Health = func(port int) bool { return f(port) == nil }
		}
	}
	if len(deps) >= 3 {
		if f, ok := deps[2].(func(int) (string, error)); ok {
			out.CommandLine = f
		}
	}
	if len(deps) >= 4 {
		if f, ok := deps[3].(func(int) error); ok {
			out.Kill = f
		}
	}
	return out
}

var (
	defaultPidAlive    = func(_ int) bool { return false }
	defaultHealth      = func(_ int) bool { return false }
	defaultCommandLine = func(_ int) (string, error) { return "", nil }
	defaultKill        = func(_ int) error { return nil }
)

type unsupportedSupervisor struct{ opts Options }

// Start always fails: the platform has no process control support.
func (unsupportedSupervisor) Start(_ context.Context) (report.ServiceInfo, error) {
	return report.ServiceInfo{}, ErrUnsupportedPlatform
}

// Health always fails: the platform has no process control support.
func (unsupportedSupervisor) Health(_ context.Context) error { return ErrUnsupportedPlatform }

// Stop always fails: the platform has no process control support.
func (unsupportedSupervisor) Stop(_ context.Context) error { return ErrUnsupportedPlatform }

type supervisor struct {
	opts           Options
	port           int
	cmd            *exec.Cmd
	uvPID          int
	doneCh         chan error
	logFile        *os.File
	readyMS        int64
	healthFailures int
	lost           bool
}

// New returns the platform supervisor, or a stub reporting ErrUnsupportedPlatform.
func New(opts Options) Supervisor {
	if !platformSupported {
		return unsupportedSupervisor{opts: opts}
	}
	return &supervisor{opts: opts}
}

// Start launches the service, waits for readiness and retries once on a port clash.
func (s *supervisor) Start(ctx context.Context) (report.ServiceInfo, error) {
	if _, err := lookPath("uv"); err != nil {
		return report.ServiceInfo{}, fmt.Errorf("uv not found; install uv (https://docs.astral.sh/uv/): %w", err)
	}
	port := s.opts.Port
	if port == 0 {
		p, err := FreePort()
		if err != nil {
			return report.ServiceInfo{}, err
		}
		port = p
	}
	s.port = port

	uploadDir := UploadDir(port)
	_ = os.RemoveAll(uploadDir)
	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return report.ServiceInfo{}, fmt.Errorf("create upload dir: %w", err)
	}
	started := false
	defer func() {
		if !started {
			_ = os.RemoveAll(uploadDir)
		}
	}()

	for attempt := range 2 {
		if s.opts.LogPath != "" {
			f, err := os.OpenFile(s.opts.LogPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err == nil {
				s.logFile = f
			}
		}
		args := BuildCommandArgs(s.opts, port)
		cmd := execCommand("uv", args...)
		var w io.Writer
		if s.logFile != nil {
			if s.opts.LogStream != nil {
				lw := NewLineWriter("[service] ", s.opts.LogStream)
				w = io.MultiWriter(s.logFile, lw)
			} else {
				w = s.logFile
			}
		} else if s.opts.LogStream != nil {
			lw := NewLineWriter("[service] ", s.opts.LogStream)
			w = lw
		}
		if w != nil {
			cmd.Stdout = w
			cmd.Stderr = w
		}
		cmd.SysProcAttr = sysProcAttr()
		if err := cmd.Start(); err != nil {
			return report.ServiceInfo{}, err
		}
		s.cmd = cmd
		s.uvPID = cmd.Process.Pid
		s.doneCh = make(chan error, 1)
		go func() { s.doneCh <- cmd.Wait() }()

		readyMS, err := waitReady(ctx, port)
		if err == nil {
			s.readyMS = readyMS
			started = true
			return report.ServiceInfo{PID: s.uvPID, Port: port, ReadyMS: readyMS, Stopped: false}, nil
		}
		exited := false
		var exitCode int
		select {
		case werr := <-s.doneCh:
			exited = true
			if ee, ok := werr.(*exec.ExitError); ok {
				exitCode = ee.ExitCode()
			} else if werr != nil {
				exitCode = -1
			}
		case <-time.After(100 * time.Millisecond):
		}
		_ = defaultKill(s.uvPID)
		if s.logFile != nil {
			s.logFile.Close()
			s.logFile = nil
		}

		if exited {
			tail := readLogTail(s.opts.LogPath, 50)
			retry, classErr := ClassifyExit(exitCode, tail, s.opts.Project)
			if retry && attempt == 0 {
				if s.opts.Port == 0 {
					np, err := FreePort()
					if err == nil {
						port = np
						s.port = port
					}
				} else {
					port = port + 1
					s.port = port
				}
				continue
			}
			if classErr != nil {
				return report.ServiceInfo{}, classErr
			}
		}
		return report.ServiceInfo{}, fmt.Errorf("service not ready: %w", err)
	}
	return report.ServiceInfo{}, fmt.Errorf("service failed to start")
}

// Health probes the service and marks it lost after three consecutive failures.
func (s *supervisor) Health(ctx context.Context) error {
	err := CheckHealth(ctx, s.port)
	if err == nil {
		s.healthFailures = 0
		return nil
	}
	s.healthFailures++
	if s.healthFailures >= 3 {
		s.lost = true
	}
	return err
}

// Lost reports whether the liveness monitor gave up on the service.
func (s *supervisor) Lost() bool { return s.lost }

// Stop terminates the service process group, escalating to SIGKILL after 5s.
func (s *supervisor) Stop(_ context.Context) error {
	if s.uvPID != 0 {
		// The child runs in its own process group (sysProcAttr sets Setpgid), so
		// signalling the group reaches uv's python child even if uv stops
		// forwarding signals. Wait on doneCh: cmd.Wait is already reaping the
		// process, and on POSIX only the parent may wait for it.
		_ = gracefulStop(s.uvPID)
		exited := false
		if s.doneCh != nil {
			select {
			case <-s.doneCh:
				exited = true
			case <-time.After(5 * time.Second):
			}
		}
		if !exited {
			_ = defaultKill(s.uvPID)
			if s.doneCh != nil {
				select {
				case <-s.doneCh:
				case <-time.After(2 * time.Second):
				}
			}
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", s.port), 200*time.Millisecond)
		if err != nil {
			break
		}
		conn.Close()
		time.Sleep(100 * time.Millisecond)
	}
	if s.logFile != nil {
		s.logFile.Close()
		s.logFile = nil
	}
	_ = os.RemoveAll(UploadDir(s.port))
	return nil
}

var (
	sysProcAttr  = func() *syscall.SysProcAttr { return nil }
	gracefulStop = func(_ int) error { return nil }
)
