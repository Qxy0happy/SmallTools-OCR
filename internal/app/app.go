package app

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/pflag"
)

// Config holds the parsed command line for one run.
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

// Defaults returns the built-in configuration, honouring MINERU_PROJECT.
func Defaults() Config {
	project := os.Getenv("MINERU_PROJECT")
	if project == "" {
		project = defaultProject()
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

// defaultProject is $HOME/Projects/MinerU, or the bare relative path when the
// home directory cannot be resolved.
func defaultProject() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join("Projects", "MinerU")
	}
	return filepath.Join(home, "Projects", "MinerU")
}

// Parse fills a Config from args and requires at least one root.
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
