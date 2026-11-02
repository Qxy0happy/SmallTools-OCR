package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"mineru-ocr/internal/app"
	"mineru-ocr/internal/run"

	"github.com/spf13/pflag"
)

func main() {
	cfg, err := app.Parse(os.Args[1:])
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	code := run.Run(context.Background(), cfg)
	os.Exit(code)
}
