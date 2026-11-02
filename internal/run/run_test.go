package run

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mineru-ocr/internal/app"
	"mineru-ocr/internal/mineru"
	"mineru-ocr/internal/report"
	"mineru-ocr/internal/service"
)

type fakeTasks struct {
	t           *testing.T
	mu          sync.Mutex
	createCnt   map[string]int
	cancelCnt   map[string]int
	createTotal int64
}

func newFakeTasks(t *testing.T) *fakeTasks {
	return &fakeTasks{t: t, createCnt: map[string]int{}, cancelCnt: map[string]int{}}
}

func (f *fakeTasks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks":
		var body struct {
			Files []struct {
				FileName string `json:"file_name"`
				Bytes    int64  `json:"bytes"`
			} `json:"files"`
		}
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		name := ""
		if len(body.Files) > 0 {
			name = body.Files[0].FileName
		}
		f.mu.Lock()
		f.createCnt[name]++
		atomic.AddInt64(&f.createTotal, 1)
		cnt := f.createCnt[name]
		f.mu.Unlock()

		if name == "bad.pdf" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "bad_request", "message": "bad"}})
			return
		}
		if name == "retry.pdf" && cnt <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": "server_error", "message": "boom"}})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{
			"task_id": name,
			"status":  "pending",
			"files":   []map[string]any{{"file_id": "f1", "status": "pending", "upload": map[string]any{"url": "/upload/" + name}}},
		})
	case strings.HasPrefix(r.URL.Path, "/upload/"):
		name := strings.TrimPrefix(r.URL.Path, "/upload/")
		if name == "slow.pdf" {
			time.Sleep(30 * time.Second)
		}
		w.WriteHeader(http.StatusOK)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/tasks/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/tasks/")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "completed",
			"files":  []map[string]any{{"status": "completed", "result_url": "/download/" + id}},
		})
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cancel"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/cancel")
		f.mu.Lock()
		f.cancelCnt[id]++
		f.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	case strings.HasPrefix(r.URL.Path, "/download/"):
		w.Header().Set("Content-Type", "application/zip")
		zw := zip.NewWriter(w)
		fw, _ := zw.Create("markdown.md")
		fw.Write([]byte("# hi"))
		zw.Close()
	default:
		http.NotFound(w, r)
	}
}

type fakeSup struct {
	port       int
	healthErr  error
	mu         sync.Mutex
	stopCalled bool
}

func (s *fakeSup) Start(ctx context.Context) (report.ServiceInfo, error) {
	return report.ServiceInfo{Port: s.port, ReadyMS: 1}, nil
}
func (s *fakeSup) Health(ctx context.Context) error { return s.healthErr }
func (s *fakeSup) Stop(ctx context.Context) error {
	s.mu.Lock()
	s.stopCalled = true
	s.mu.Unlock()
	return nil
}

func makeFiles(t *testing.T, dir string, names []string) {
	t.Helper()
	for _, n := range names {
		os.WriteFile(filepath.Join(dir, n), []byte("content-"+n), 0o644)
	}
}

func newCfg(dir string, jsonMode bool) (app.Config, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errb := &bytes.Buffer{}
	cfg := app.Defaults()
	cfg.Roots = []string{dir}
	cfg.JSON = jsonMode
	cfg.Out = out
	cfg.Err = errb
	cfg.Workers = 2
	return cfg, out, errb
}

func runCase(t *testing.T, dir string, names []string, cfg app.Config, srv *httptest.Server, ft *fakeTasks, healthErr error) (int, *bytes.Buffer, *bytes.Buffer, *fakeSup) {
	t.Helper()
	makeFiles(t, dir, names)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	sup := &fakeSup{port: port, healthErr: healthErr}
	cfg.Roots = []string{dir}
	deps := Deps{
		NewClient:     func(baseURL string) *mineru.Client { return mineru.New(baseURL) },
		NewSupervisor: func(opts service.Options) service.Supervisor { return sup },
	}
	code := Run(context.Background(), cfg, deps)
	return code, cfg.Out.(*bytes.Buffer), cfg.Err.(*bytes.Buffer), sup
}

func TestRunThreeFilesAllSuccess(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(newFakeTasks(t))
	defer srv.Close()
	ft := newFakeTasks(t)
	_ = ft
	// use srv handler
	srv.Config.Handler = newFakeTasks(t)
	// Re-create server with handler properly
	srv.Close()
	ft = newFakeTasks(t)
	srv = httptest.NewServer(ft)
	defer srv.Close()

	cfg, out, errb := newCfg(dir, false)
	code, _, _, _ := runCase(t, dir, []string{"a.pdf", "b.docx", "c.pptx"}, cfg, srv, ft, nil)
	_ = out
	_ = errb
	if code != 0 {
		t.Fatalf("exit code %d", code)
	}
	for _, n := range []string{"a.pdf", "b.docx", "c.pptx"} {
		if _, err := os.Stat(filepath.Join(dir, n+".zip")); err != nil {
			t.Fatalf("missing zip for %s: %v", n, err)
		}
	}
}

func TestRunSecondRunSkipsZeroHTTP(t *testing.T) {
	dir := t.TempDir()
	makeFiles(t, dir, []string{"a.pdf", "b.docx", "c.pptx"})
	// first run
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()
	cfg, _, _ := newCfg(dir, false)
	runCase(t, dir, []string{}, cfg, srv, ft, nil)
	// second run
	ft2 := newFakeTasks(t)
	srv2 := httptest.NewServer(ft2)
	defer srv2.Close()
	cfg2, _, _ := newCfg(dir, false)
	code, _, _, _ := runCase(t, dir, []string{}, cfg2, srv2, ft2, nil)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if atomic.LoadInt64(&ft2.createTotal) != 0 {
		t.Fatalf("expected 0 create calls, got %d", ft2.createTotal)
	}
}

func TestRunDeleteZipRedoesOne(t *testing.T) {
	dir := t.TempDir()
	makeFiles(t, dir, []string{"a.pdf", "b.docx", "c.pptx"})
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()
	cfg, _, _ := newCfg(dir, false)
	runCase(t, dir, []string{}, cfg, srv, ft, nil)

	os.Remove(filepath.Join(dir, "b.docx.zip"))

	ft2 := newFakeTasks(t)
	srv2 := httptest.NewServer(ft2)
	defer srv2.Close()
	cfg2, _, _ := newCfg(dir, false)
	code, _, _, _ := runCase(t, dir, []string{}, cfg2, srv2, ft2, nil)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if atomic.LoadInt64(&ft2.createTotal) != 1 {
		t.Fatalf("expected 1 create call, got %d", ft2.createTotal)
	}
}

func TestRunSingleFile400FailsExit1(t *testing.T) {
	dir := t.TempDir()
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()
	cfg, _, _ := newCfg(dir, false)
	code, _, _, _ := runCase(t, dir, []string{"a.pdf", "bad.pdf", "c.pptx"}, cfg, srv, ft, nil)
	if code != 1 {
		t.Fatalf("exit %d", code)
	}
}

func TestRunRetry500ThenSuccess(t *testing.T) {
	dir := t.TempDir()
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()
	cfg, _, _ := newCfg(dir, false)
	code, _, _, _ := runCase(t, dir, []string{"retry.pdf"}, cfg, srv, ft, nil)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "retry.pdf.zip")); err != nil {
		t.Fatal(err)
	}
}

func TestRunJSONFieldsExitCode(t *testing.T) {
	dir := t.TempDir()
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()
	cfg, out, _ := newCfg(dir, true)
	code, _, _, _ := runCase(t, dir, []string{"a.pdf", "b.docx", "c.pptx"}, cfg, srv, ft, nil)
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	var r report.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatal(err)
	}
	if r.ExitCode != 0 || r.Completed != 3 {
		t.Fatalf("report: exit=%d completed=%d", r.ExitCode, r.Completed)
	}
	if r.SchemaVersion != 1 {
		t.Fatalf("schema %d", r.SchemaVersion)
	}
}

func TestRunHealthLostExit2(t *testing.T) {
	dir := t.TempDir()
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()
	cfg, _, _ := newCfg(dir, false)
	code, _, _, _ := runCase(t, dir, []string{"slow.pdf"}, cfg, srv, ft, errors.New("health fail"))
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestRunCancelCallsCancelAndStop(t *testing.T) {
	dir := t.TempDir()
	ft := newFakeTasks(t)
	srv := httptest.NewServer(ft)
	defer srv.Close()

	makeFiles(t, dir, []string{"slow.pdf"})
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	sup := &fakeSup{port: port, healthErr: nil}
	cfg, out, errb := newCfg(dir, false)
	cfg.Roots = []string{dir}
	_ = out
	_ = errb
	deps := Deps{
		NewClient:     func(baseURL string) *mineru.Client { return mineru.New(baseURL) },
		NewSupervisor: func(opts service.Options) service.Supervisor { return sup },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		done <- Run(ctx, cfg, deps)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case code := <-done:
		if code != 130 {
			t.Fatalf("exit %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout")
	}
	ft.mu.Lock()
	cancelCalls := 0
	for _, c := range ft.cancelCnt {
		cancelCalls += c
	}
	ft.mu.Unlock()
	if cancelCalls == 0 {
		t.Fatalf("Cancel not called")
	}
	sup.mu.Lock()
	stopped := sup.stopCalled
	sup.mu.Unlock()
	if !stopped {
		t.Fatal("Stop not called")
	}
}

var _ = fmt.Sprintf
