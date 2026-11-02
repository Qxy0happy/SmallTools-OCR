package report

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type FileStatus string

const (
	StatusCompleted FileStatus = "completed"
	StatusSkipped   FileStatus = "skipped"
	StatusFailed    FileStatus = "failed"
)

type FileResult struct {
	Path       string     `json:"path"`
	Status     FileStatus `json:"status"`
	Error      *Error     `json:"error,omitempty"`
	DurationMS int64      `json:"duration_ms,omitempty"`
	ZipPath    string     `json:"zip_path,omitempty"`
}

type ServiceInfo struct {
	Port    int   `json:"port"`
	ReadyMS int64 `json:"ready_ms"`
	Stopped bool  `json:"stopped"`
}

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

func (r *Report) Add(fr FileResult) {
	if r.Files == nil {
		r.Files = []FileResult{}
	}
	r.Files = append(r.Files, fr)
}

func Summary(w io.Writer, r *Report) {
	fmt.Fprintf(w, "scanned=%d skipped=%d completed=%d failed=%d exit=%d\n", r.Scanned, r.Skipped, r.Completed, r.Failed, r.ExitCode)
}

func WriteJSON(w io.Writer, r *Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

type Progress struct {
	Out   io.Writer
	Total int
}

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
