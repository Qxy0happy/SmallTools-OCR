package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

// Error is a machine-readable failure attached to a file result.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// FileStatus is the terminal status of one file.
type FileStatus string

// Status values reported for each file.
const (
	StatusCompleted FileStatus = "completed"
	StatusSkipped   FileStatus = "skipped"
	StatusFailed    FileStatus = "failed"
)

// FileResult is one file's outcome as reported to the user.
type FileResult struct {
	Path       string     `json:"path"`
	Status     FileStatus `json:"status"`
	Error      *Error     `json:"error,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	ZipPath    string     `json:"zip_path,omitempty"`
}

// ServiceInfo describes the supervised service run.
type ServiceInfo struct {
	PID     int   `json:"-"`
	Port    int   `json:"port"`
	ReadyMS int64 `json:"ready_ms"`
	Stopped bool  `json:"stopped"`
}

// Report is the summary written to stdout, as a table or as JSON.
type Report struct {
	SchemaVersion int          `json:"schema_version"`
	Roots         []string     `json:"roots"`
	Scanned       int          `json:"scanned"`
	Skipped       int          `json:"skipped"`
	Completed     int          `json:"completed"`
	Failed        int          `json:"failed"`
	ExitCode      int          `json:"exit_code"`
	Service       ServiceInfo  `json:"service"`
	Files         []FileResult `json:"files"`
}

// Add appends fr to the report's file list.
func (r *Report) Add(fr FileResult) {
	if r.Files == nil {
		r.Files = []FileResult{}
	}
	r.Files = append(r.Files, fr)
}

// Summary writes the fixed-width summary table.
func Summary(w io.Writer, r *Report) {
	fmt.Fprintf(w, "%-8s %-8s %-10s %-7s %-5s\n", "scanned", "skipped", "completed", "failed", "exit")
	fmt.Fprintf(w, "%-8d %-8d %-10d %-7d %-5d\n", r.Scanned, r.Skipped, r.Completed, r.Failed, r.ExitCode)
}

// WriteJSON writes the report as indented JSON.
func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

// Progress prints one line per processed file to Out.
type Progress struct {
	Out   io.Writer
	Total int
}

// Line prints the progress line for file i of p.Total.
func (p *Progress) Line(i int, fr FileResult) {
	base := filepath.Base(fr.Path)
	switch fr.Status {
	case StatusFailed:
		msg := ""
		if fr.Error != nil {
			msg = fr.Error.Code + ": " + fr.Error.Message
		}
		fmt.Fprintf(p.Out, "[%d/%d] FAIL %s  %s\n", i, p.Total, base, msg)
	default:
		fmt.Fprintf(p.Out, "[%d/%d] ok   %s  %.1fs\n", i, p.Total, base, float64(fr.DurationMS)/1000.0)
	}
}
