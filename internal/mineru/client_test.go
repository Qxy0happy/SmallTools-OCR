package mineru

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func init() {
	// Make retries instant in tests.
	Sleep = func(time.Duration) {}
}

func newClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL), srv
}

func TestClientCreatePDFTierAdvanced(t *testing.T) {
	var got map[string]any
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/tasks" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{
			"task_id": "t1",
			"status":  "pending",
			"files":   []map[string]any{{"file_id": "f1", "status": "pending", "upload": map[string]any{"url": "/upload"}}},
		})
	})
	c, _ := newClient(t, h)
	tid, up, err := c.Create(context.Background(), "a.pdf", 123, "advanced")
	if err != nil {
		t.Fatal(err)
	}
	if tid != "t1" || !strings.HasSuffix(up, "/upload") {
		t.Fatalf("tid=%s up=%s", tid, up)
	}
	f, _ := got["files"].([]any)
	if len(f) != 1 {
		t.Fatalf("files: %+v", got)
	}
	b, _ := json.Marshal(got)
	if !strings.Contains(string(b), `"tier":"advanced"`) {
		t.Fatalf("tier missing: %s", b)
	}
	// verify other fields
	if got["ocr_mode"] != "auto" {
		t.Fatalf("ocr_mode: %v", got["ocr_mode"])
	}
	if got["page_range"] != "" {
		t.Fatalf("page_range: %v", got["page_range"])
	}
	of, _ := got["output_formats"].([]any)
	if len(of) != 1 || of[0] != "default" {
		t.Fatalf("output_formats: %v", of)
	}
	fi := f[0].(map[string]any)
	if fi["file_name"] != "a.pdf" {
		t.Fatalf("file_name: %v", fi["file_name"])
	}
	if int64(fi["bytes"].(float64)) != 123 {
		t.Fatalf("bytes: %v", fi["bytes"])
	}
}

func TestClientCreateOfficeOmitsTier(t *testing.T) {
	var raw []byte
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = b
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task_id": "t2",
			"files":   []map[string]any{{"upload": map[string]any{"url": "http://x/upload"}}},
		})
	})
	c, _ := newClient(t, h)
	_, _, err := c.Create(context.Background(), "a.docx", 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"tier"`) {
		t.Fatalf("tier should be omitted: %s", raw)
	}
}

// removed

func TestClientUploadContentLength(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	data := make([]byte, 4096)
	for i := range data {
		data[i] = byte(i)
	}
	os.WriteFile(src, data, 0o644)

	var seen int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/tasks" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"task_id": "t",
				"files":   []map[string]any{{"upload": map[string]any{"url": "/upload"}}},
			})
			return
		}
		seen = r.ContentLength
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newClient(t, h)
	_, up, err := c.Create(context.Background(), "src.bin", int64(len(data)), "advanced")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Upload(context.Background(), up, src); err != nil {
		t.Fatal(err)
	}
	if seen != int64(len(data)) {
		t.Fatalf("content-length %d want %d", seen, len(data))
	}
}

func TestClientUploadSourceChanged(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	os.WriteFile(src, []byte("hello"), 0o644)
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task_id": "t",
			"files":   []map[string]any{{"upload": map[string]any{"url": "/upload"}}},
		})
	})
	c, _ := newClient(t, h)
	// declared size differs from actual
	_, up, err := c.Create(context.Background(), "src.bin", 999, "")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Upload(context.Background(), up, src)
	if err != ErrSourceChanged {
		t.Fatalf("expected ErrSourceChanged got %v", err)
	}
}

func TestClientPollRunningToCompleted(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"status": "running",
				"files":  []map[string]any{{"status": "running"}},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": "completed",
			"files":  []map[string]any{{"status": "completed", "result_url": "/res.zip"}},
		})
	})
	c, _ := newClient(t, h)
	st, file, err := c.Poll(context.Background(), "task1")
	if err != nil || st != "running" {
		t.Fatalf("first poll: %s %v %s", st, err, file.Status)
	}
	st2, file2, err := c.Poll(context.Background(), "task1")
	if err != nil || st2 != "completed" {
		t.Fatalf("second poll: %s %v", st2, err)
	}
	if file2.ResultURL != "/res.zip" {
		t.Fatalf("result url: %s", file2.ResultURL)
	}
}

func TestClientPollFailedExposesErrorCode(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": "failed",
			"files":  []map[string]any{{"status": "failed", "error": map[string]any{"code": "E_PARSE", "message": "bad"}}},
		})
	})
	c, _ := newClient(t, h)
	st, file, err := c.Poll(context.Background(), "task1")
	if err != nil {
		t.Fatal(err)
	}
	if st != "failed" {
		t.Fatalf("status: %s", st)
	}
	if file.Error == nil || file.Error.Code != "E_PARSE" {
		t.Fatalf("error not exposed: %+v", file.Error)
	}
}

func TestClientRetry500ThenSuccess(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":{"code":"boom","message":"x"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"task_id": "t",
			"files":   []map[string]any{{"upload": map[string]any{"url": "/upload"}}},
		})
	})
	c, _ := newClient(t, h)
	_, _, err := c.Create(context.Background(), "a.pdf", 1, "advanced")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("calls=%d want 3", calls)
	}
}

func TestClientNoRetryOn400(t *testing.T) {
	calls := 0
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"code":"bad","message":"nope"}}`))
	})
	c, _ := newClient(t, h)
	_, _, err := c.Create(context.Background(), "a.pdf", 1, "advanced")
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("calls=%d want 1", calls)
	}
	// ensure not retryable
	var e *Error
	if !errors.As(err, &e) || e.Retryable {
		t.Fatalf("expected non-retryable, got %+v", e)
	}
}

// removed

func TestClientDownloadWritesTempFile(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("payload-bytes"))
	})
	c, _ := newClient(t, h)
	dir := t.TempDir()
	part := filepath.Join(dir, "part.zip")
	if err := c.Download(context.Background(), "/download", part); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(part)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "payload-bytes" {
		t.Fatalf("got %s", b)
	}
}

func TestClientDownloadRedirect(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orig" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Write([]byte("redirected"))
	})
	c, _ := newClient(t, h)
	dir := t.TempDir()
	part := filepath.Join(dir, "part.zip")
	if err := c.Download(context.Background(), "/orig", part); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(part)
	if string(b) != "redirected" {
		t.Fatalf("got %s", b)
	}
}

func TestClientHealthNon200Error(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, _ := newClient(t, h)
	if err := c.Health(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestClientHealthOK(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	c, _ := newClient(t, h)
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}
