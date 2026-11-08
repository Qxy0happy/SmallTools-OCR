package app

import (
	"os"
	"testing"
)

func TestDefaults(t *testing.T) {
	os.Unsetenv("MINERU_PROJECT")
	c := Defaults()
	if c.Workers != 2 || c.Tier != "advanced" || c.Project != "/home/qxyhappy/Projects/MinerU" || c.Port != 0 {
		t.Fatalf("bad defaults: %+v", c)
	}
}

func TestEnvProject(t *testing.T) {
	os.Setenv("MINERU_PROJECT", "/opt/MinerU")
	defer os.Unsetenv("MINERU_PROJECT")
	c := Defaults()
	if c.Project != "/opt/MinerU" {
		t.Fatalf("env not applied: %s", c.Project)
	}
}

func TestParsePriorityAndInterleave(t *testing.T) {
	os.Unsetenv("MINERU_PROJECT")
	cfg, err := Parse([]string{"--workers", "4", "root1", "--tier", "basic", "root2"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Workers != 4 || cfg.Tier != "basic" {
		t.Fatalf("flags not applied: %+v", cfg)
	}
	if len(cfg.Roots) != 2 || cfg.Roots[0] != "root1" || cfg.Roots[1] != "root2" {
		t.Fatalf("roots: %v", cfg.Roots)
	}
}

func TestParseNoRoots(t *testing.T) {
	_, err := Parse([]string{"--workers", "4"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseHelp(t *testing.T) {
	_, err := Parse([]string{"--help"})
	if err == nil {
		t.Fatal("expected errhelp")
	}
}
