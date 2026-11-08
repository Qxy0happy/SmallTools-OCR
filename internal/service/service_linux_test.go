//go:build linux

package service

import (
	"os"
	"strings"
	"testing"
)

func TestSplitStatHandlesCommWithSpacesAndParens(t *testing.T) {
	// comm may contain spaces and ')' — parse from the last ')'.
	data := []byte("1234 (my weird )( name) S 42 1234 1234 0 -1\n")
	fields, err := splitStat(data)
	if err != nil {
		t.Fatal(err)
	}
	if fields[0] != "S" || fields[1] != "42" {
		t.Fatalf("state=%q ppid=%q", fields[0], fields[1])
	}
	if _, err := splitStat([]byte("1234 no comm field")); err == nil {
		t.Fatal("expected error for missing comm field")
	}
	if _, err := splitStat([]byte("1234 (x) S")); err == nil {
		t.Fatal("expected error for short stat")
	}
}

func TestProcessStateAndParentOfSelf(t *testing.T) {
	state, err := processState(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if state == 'Z' || state == 'X' {
		t.Fatalf("test process reported as dead: %q", state)
	}
	parent, err := processParent(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if parent != os.Getppid() {
		t.Fatalf("ppid=%d want %d", parent, os.Getppid())
	}
}

func TestDefaultPidAlive(t *testing.T) {
	if !defaultPidAlive(os.Getpid()) {
		t.Fatal("self reported dead")
	}
	if defaultPidAlive(1 << 30) {
		t.Fatal("nonexistent pid reported alive")
	}
}

func TestDefaultCommandLineOfSelf(t *testing.T) {
	cmdline, err := defaultCommandLine(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if cmdline == "" {
		t.Fatal("empty cmdline")
	}
	if strings.ContainsRune(cmdline, 0) {
		t.Fatal("cmdline still NUL-separated")
	}
}
