package run

import (
	"context"
	"fmt"

	"mineru-ocr/internal/app"
	"mineru-ocr/internal/discover"
	"mineru-ocr/internal/report"
)

func Run(ctx context.Context, cfg app.Config) int {
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

	// Non-dry-run pipeline not yet implemented (ticket 17).
	fmt.Fprintln(cfg.Err, "not implemented")
	return 2
}
