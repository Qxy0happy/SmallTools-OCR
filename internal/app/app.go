package app

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/pflag"
)

type Config struct {
	Roots              []string
	Workers            int
	Tier               string
	Project            string
	Port               int
	DryRun             bool
	JSON               bool
	Verbose            bool
	NoServiceLogStream bool
	ServiceLog         string
	Out                io.Writer
	Err                io.Writer
}

func Defaults() Config {
	project := os.Getenv("MINERU_PROJECT")
	if project == "" {
		project = "D:/Projects/Python/MinerU"
	}
	return Config{
		Workers: 2,
		Tier:    "advanced",
		Project: project,
		Port:    0,
		Out:     os.Stdout,
		Err:     os.Stderr,
	}
}

func Parse(args []string) (Config, error) {
	cfg := Defaults()
	fs := pflag.NewFlagSet("mineru-ocr", pflag.ContinueOnError)
	fs.SetOutput(cfg.Err)
	fs.IntVar(&cfg.Workers, "workers", cfg.Workers, "concurrent files; also the server --concurrency")
	fs.StringVar(&cfg.Tier, "tier", cfg.Tier, "PDF parse tier (advanced/basic/standard/flash)")
	fs.StringVar(&cfg.Project, "mineru-project", cfg.Project, "MinerU project passed to uv run --project")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "service port; 0 picks a free port")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "print the plan without starting the service")
	fs.BoolVar(&cfg.JSON, "json", cfg.JSON, "write the machine-readable report to stdout")
	fs.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "log polling and retry details")
	fs.StringVar(&cfg.ServiceLog, "service-log", cfg.ServiceLog, "service log file (default <root>/.mineru-service.log)")
	fs.BoolVar(&cfg.NoServiceLogStream, "no-service-log-stream", cfg.NoServiceLogStream, "stop echoing service logs to stderr")
	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	cfg.Roots = fs.Args()
	if len(cfg.Roots) == 0 {
		return cfg, fmt.Errorf("at least one root required")
	}
	if cfg.Out == nil {
		cfg.Out = os.Stdout
	}
	if cfg.Err == nil {
		cfg.Err = os.Stderr
	}
	return cfg, nil
}
