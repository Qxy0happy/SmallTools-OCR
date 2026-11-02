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
	fs.IntVar(&cfg.Workers, "workers", cfg.Workers, "")
	fs.StringVar(&cfg.Tier, "tier", cfg.Tier, "")
	fs.StringVar(&cfg.Project, "mineru-project", cfg.Project, "")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "")
	fs.BoolVar(&cfg.DryRun, "dry-run", cfg.DryRun, "")
	fs.BoolVar(&cfg.JSON, "json", cfg.JSON, "")
	fs.BoolVar(&cfg.Verbose, "verbose", cfg.Verbose, "")
	fs.StringVar(&cfg.ServiceLog, "service-log", cfg.ServiceLog, "")
	fs.BoolVar(&cfg.NoServiceLogStream, "no-service-log-stream", cfg.NoServiceLogStream, "")
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
