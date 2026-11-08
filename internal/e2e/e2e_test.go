//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mineru-ocr/internal/app"
	"mineru-ocr/internal/artifact"
	"mineru-ocr/internal/report"
	"mineru-ocr/internal/run"
	"mineru-ocr/internal/testfixture"
)

// countMineruProcesses counts live mineru-kit api-server processes by scanning
// /proc, so it sees uv and its python child alike.
func countMineruProcesses() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	self := os.Getpid()
	n := 0
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		data, err := os.ReadFile("/proc/" + e.Name() + "/cmdline")
		if err != nil {
			continue
		}
		cmdline := strings.ReplaceAll(string(data), "\x00", " ")
		if strings.Contains(cmdline, "mineru-kit") && strings.Contains(cmdline, "api-server") {
			n++
		}
	}
	return n, nil
}

func newCfg(dir string) (app.Config, *bytes.Buffer, *bytes.Buffer) {
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	cfg := app.Defaults()
	cfg.Roots = []string{dir}
	cfg.Workers = 1
	cfg.JSON = true
	cfg.Out = outBuf
	cfg.Err = errBuf
	cfg.ServiceLog = filepath.Join(dir, ".mineru-service.log")
	return cfg, outBuf, errBuf
}

func runE2E(t *testing.T, dir string) (int, *report.Report, string, string) {
	t.Helper()
	cfg, outBuf, errBuf := newCfg(dir)
	code := run.Run(t.Context(), cfg)
	var r report.Report
	_ = json.Unmarshal(outBuf.Bytes(), &r)
	return code, &r, outBuf.String(), errBuf.String()
}

func TestE2EFull(t *testing.T) {
	dir, err := os.MkdirTemp("", "mineru-e2e-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	if err := testfixture.WritePDF(filepath.Join(dir, "a.pdf")); err != nil {
		t.Fatal(err)
	}
	if err := testfixture.WriteDocx(filepath.Join(dir, "b.docx")); err != nil {
		t.Fatal(err)
	}

	// First run: zips land, exit 0.
	code, rep, _, stderr := runE2E(t, dir)
	t.Logf("first run exit=%d stderr=%s", code, stderr)
	if code != 0 {
		t.Fatalf("first run exit %d", code)
	}
	if rep.Skipped != 0 {
		t.Fatalf("first run skipped %d", rep.Skipped)
	}
	for _, fr := range rep.Files {
		if fr.Status != report.StatusCompleted {
			t.Fatalf("file %s status %s", fr.Path, fr.Status)
		}
		if err := artifact.Validate(fr.ZipPath); err != nil {
			t.Fatalf("zip invalid %s: %v", fr.ZipPath, err)
		}
		if _, err := os.Stat(fr.ZipPath); err != nil {
			t.Fatalf("zip missing %s", fr.ZipPath)
		}
	}
	if !strings.Contains(stderr, "[service]") {
		t.Fatalf("stderr missing [service] lines")
	}

	// Second run: all skipped, exit 0.
	code2, rep2, _, stderr2 := runE2E(t, dir)
	t.Logf("second run exit=%d stderr=%s", code2, stderr2)
	if code2 != 0 {
		t.Fatalf("second run exit %d", code2)
	}
	if rep2.Skipped != 2 {
		t.Fatalf("second run skipped %d want 2", rep2.Skipped)
	}
	if rep2.Completed != 0 || rep2.Failed != 0 {
		t.Fatalf("second run completed=%d failed=%d", rep2.Completed, rep2.Failed)
	}

	// Corrupt one zip -> redo that file, other skipped.
	pdfZip := artifact.ZipPathFor(filepath.Join(dir, "a.pdf"))
	if err := os.WriteFile(pdfZip, []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	code3, rep3, _, stderr3 := runE2E(t, dir)
	t.Logf("third run exit=%d stderr=%s", code3, stderr3)
	if code3 != 0 {
		t.Fatalf("third run exit %d", code3)
	}
	if rep3.Skipped != 1 {
		t.Fatalf("third run skipped %d want 1", rep3.Skipped)
	}
	if rep3.Completed != 1 {
		t.Fatalf("third run completed %d want 1", rep3.Completed)
	}
	if err := artifact.Validate(pdfZip); err != nil {
		t.Fatalf("redo zip invalid: %v", err)
	}

	// Port released.
	port := rep.Service.Port
	if port > 0 {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
			if err != nil {
				break
			}
			conn.Close()
			time.Sleep(100 * time.Millisecond)
		}
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			conn.Close()
			t.Fatalf("port %d still open", port)
		}
	}

	// No uv/python residue.
	deadline2 := time.Now().Add(10 * time.Second)
	for {
		n, err := countMineruProcesses()
		if err != nil {
			t.Fatalf("process check: %v", err)
		}
		if n == 0 {
			break
		}
		if time.Now().After(deadline2) {
			t.Fatalf("mineru processes remaining: %d", n)
		}
		time.Sleep(500 * time.Millisecond)
	}
}
