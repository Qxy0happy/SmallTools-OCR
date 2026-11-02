package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildCommandArgs(t *testing.T) {
	opts := Options{Project: "proj", Tier: "advanced", Workers: 4}
	got := BuildCommandArgs(opts, 8080)
	want := []string{"run", "--project", "proj", "mineru-kit", "api-server", "--port", "8080", "--tier", "advanced", "--concurrency", "4", "--preload-models"}
	if len(got) != len(want) {
		t.Fatalf("len %d != %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestFreePort(t *testing.T) {
	p, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	if p <= 0 {
		t.Fatalf("bad port %d", p)
	}
	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
	if err != nil {
		t.Fatalf("port not free: %v", err)
	}
	l.Close()
}

func TestLineWriterMultiLine(t *testing.T) {
	var b strings.Builder
	lw := NewLineWriter("[service] ", &b)
	if _, err := lw.Write([]byte("a\nb\nc\n")); err != nil {
		t.Fatal(err)
	}
	if b.String() != "[service] a\n[service] b\n[service] c\n" {
		t.Fatalf("got %q", b.String())
	}
}

func TestLineWriterHalfLine(t *testing.T) {
	var b strings.Builder
	lw := NewLineWriter("[service] ", &b)
	if _, err := lw.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if b.String() != "" {
		t.Fatalf("half line flushed early: %q", b.String())
	}
	if _, err := lw.Write([]byte(" line\n")); err != nil {
		t.Fatal(err)
	}
	if b.String() != "[service] partial line\n" {
		t.Fatalf("got %q", b.String())
	}
}

func TestLineWriterConcurrent(t *testing.T) {
	var b strings.Builder
	lw := NewLineWriter("[svc] ", &b)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			lw.Write([]byte(fmt.Sprintf("line%d\n", n)))
		}(i)
	}
	wg.Wait()
	out := b.String()
	for i := 0; i < 8; i++ {
		if !strings.Contains(out, fmt.Sprintf("[svc] line%d\n", i)) {
			t.Fatalf("missing line%d in %q", i, out)
		}
	}
}

func TestWaitReadyPolls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			w.WriteHeader(404)
			return
		}
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port := srv.Listener.Addr().(*net.TCPAddr).Port

	start := time.Now()
	ms, err := WaitReadyTimeout(context.Background(), port, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if ms < 150 {
		t.Fatalf("ready too fast: %d ms", ms)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Fatalf("did not poll/wait")
	}
}

func TestWaitReadyTimeout(t *testing.T) {
	// no server listening
	p, err := FreePort()
	if err != nil {
		t.Fatal(err)
	}
	_, err = WaitReadyTimeout(context.Background(), p, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestClassifyExitRetryPortInUse(t *testing.T) {
	retry, err := ClassifyExit(3, "boom")
	if !retry {
		t.Fatalf("expected retry, got %v", err)
	}
	retry, err = ClassifyExit(0, "WinError 10048")
	if !retry {
		t.Fatalf("expected retry on 10048, got %v", err)
	}
	_ = err
}

func TestClassifyExitUnsupportedTierNoRetry(t *testing.T) {
	retry, err := ClassifyExit(1, "Unsupported tier: basic")
	if retry {
		t.Fatalf("expected no retry")
	}
	if err == nil || !strings.Contains(err.Error(), "unsupported tier") {
		t.Fatalf("bad err: %v", err)
	}
}

func TestClassifyExitStartupFailureWording(t *testing.T) {
	_, err := ClassifyExit(1, "boom")
	if err == nil || !strings.Contains(err.Error(), "uv run --project <project> mineru-kit models download") {
		t.Fatalf("bad wording: %v", err)
	}
	_, err = ClassifyExit(1, "boom", "myproj")
	if err == nil || !strings.Contains(err.Error(), "uv run --project myproj mineru-kit models download") {
		t.Fatalf("bad wording with project: %v", err)
	}
}

func TestStaleMatchTruthTable(t *testing.T) {
	for _, alive := range []bool{false, true} {
		for _, hlth := range []bool{false, true} {
			for _, cmdOK := range []bool{false, true} {
				aliveFn := func(int) bool { return alive }
				hlthFn := func(int) bool { return hlth }
				cmdFn := func(int) (string, error) {
					if !cmdOK {
						return "no tokens", nil
					}
					return "x mineru-kit y api-server z --port 1234", nil
				}
				got := StaleMatch(Record{PID: 1, Port: 1234}, aliveFn, hlthFn, cmdFn)
				want := alive && hlth && cmdOK
				if got != want {
					t.Errorf("alive=%v hlth=%v cmdOK=%v: got %v want %v", alive, hlth, cmdOK, got, want)
				}
			}
		}
	}
	// missing individual tokens -> false
	for _, cl := range []string{
		"api-server --port 1234",
		"mineru-kit --port 1234",
		"mineru-kit api-server",
	} {
		cl := cl
		got := StaleMatch(Record{PID: 1, Port: 1234},
			func(int) bool { return true },
			func(int) bool { return true },
			func(int) (string, error) { return cl, nil })
		if got {
			t.Fatalf("expected false for %q", cl)
		}
	}
	// cmd error -> false
	got := StaleMatch(Record{PID: 1, Port: 1234},
		func(int) bool { return true },
		func(int) bool { return true },
		func(int) (string, error) { return "", errors.New("boom") })
	if got {
		t.Fatal("expected false on cmd error")
	}
}

func TestCleanupStaleKillsOnlyStale(t *testing.T) {
	var killed []int
	var mu sync.Mutex
	aliveFn := func(pid int) bool { return pid == 1 }
	hlthFn := func(port int) bool { return port == 10 }
	cmdFn := func(pid int) (string, error) { return "mineru-kit api-server --port 10", nil }
	killFn := func(pid int) error {
		mu.Lock()
		defer mu.Unlock()
		killed = append(killed, pid)
		return nil
	}
	err := CleanupStale([]Record{{PID: 1, Port: 10}, {PID: 2, Port: 20}}, aliveFn, hlthFn, cmdFn, killFn)
	if err != nil {
		t.Fatal(err)
	}
	if len(killed) != 1 || killed[0] != 1 {
		t.Fatalf("killed=%v", killed)
	}
}

func TestStartUVMissing(t *testing.T) {
	orig := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	defer func() { lookPath = orig }()
	sup := New(Options{Project: "p", Tier: "advanced", Workers: 1, Port: 1234})
	_, err := sup.Start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "install uv") && !strings.Contains(strings.ToLower(err.Error()), "uv not found") {
		t.Fatalf("bad err: %v", err)
	}
}

func TestStartStartupFailureWording(t *testing.T) {
	origLook := lookPath
	lookPath = func(string) (string, error) { return "/usr/bin/uv", nil }
	defer func() { lookPath = origLook }()

	origExec := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command("cmd", "/C", "exit 1")
	}
	defer func() { execCommand = origExec }()

	origWait := waitReady
	waitReady = func(ctx context.Context, port int) (int64, error) { return 0, errors.New("not ready") }
	defer func() { waitReady = origWait }()

	sup := New(Options{Project: "myproj", Tier: "advanced", Workers: 1, Port: 1234})
	_, err := sup.Start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "uv run --project myproj mineru-kit models download") {
		t.Fatalf("bad wording: %v", err)
	}
}
