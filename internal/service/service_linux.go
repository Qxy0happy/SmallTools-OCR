//go:build linux

package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

func init() {
	// Setpgid puts uv and its python child in a group led by uv, so signalling
	// the group reaches both without relying on uv's signal forwarding.
	sysProcAttr = func() *syscall.SysProcAttr {
		return &syscall.SysProcAttr{Setpgid: true}
	}
	gracefulStop = func(pid int) error {
		if isGroupLeader(pid) {
			return syscall.Kill(-pid, syscall.SIGTERM)
		}
		return syscall.Kill(pid, syscall.SIGTERM)
	}
	defaultPidAlive = func(pid int) bool {
		state, err := processState(pid)
		if err != nil {
			return !errors.Is(err, os.ErrNotExist)
		}
		return state != 'Z' && state != 'X'
	}
	defaultHealth = func(port int) bool {
		return CheckHealth(context.Background(), port) == nil
	}
	defaultCommandLine = func(pid int) (string, error) {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
		if err != nil {
			return "", err
		}
		parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		return strings.Join(parts, " "), nil
	}
	defaultKill = func(pid int) error {
		if isGroupLeader(pid) {
			err := syscall.Kill(-pid, syscall.SIGKILL)
			if err == nil || !errors.Is(err, syscall.ESRCH) {
				return err
			}
		}
		return killTree(pid)
	}
}

func isGroupLeader(pid int) bool {
	pgid, err := syscall.Getpgid(pid)
	return err == nil && pgid == pid
}

// splitStat splits /proc/<pid>/stat into the whitespace-separated fields that
// follow the comm field. comm is parenthesised and may itself contain spaces or
// ')', so the split starts at the last ')'. fields[0] is the state letter and
// fields[1] the parent pid.
func splitStat(data []byte) ([]string, error) {
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return nil, fmt.Errorf("stat: missing comm field")
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 2 {
		return nil, fmt.Errorf("stat: missing state or ppid field")
	}
	return fields, nil
}

func readStat(pid int) ([]string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return nil, err
	}
	fields, err := splitStat(data)
	if err != nil {
		return nil, fmt.Errorf("/proc/%d/stat: %w", pid, err)
	}
	return fields, nil
}

// processState returns the state letter, e.g. 'R', 'S', 'Z'.
func processState(pid int) (byte, error) {
	fields, err := readStat(pid)
	if err != nil {
		return 0, err
	}
	return fields[0][0], nil
}

func processParent(pid int) (int, error) {
	fields, err := readStat(pid)
	if err != nil {
		return 0, err
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, fmt.Errorf("/proc/%d/stat: bad ppid %q", pid, fields[1])
	}
	return parent, nil
}

// killTree force-kills pid and every descendant. Used when pid is not its own
// group leader, where signalling the group could hit unrelated processes.
func killTree(pid int) error {
	children := map[int][]int{}
	entries, err := os.ReadDir("/proc")
	if err == nil {
		for _, e := range entries {
			child, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			parent, err := processParent(child)
			if err != nil {
				continue
			}
			children[parent] = append(children[parent], child)
		}
	}

	var pids []int
	var collect func(int)
	collect = func(p int) {
		for _, c := range children[p] {
			collect(c)
		}
		pids = append(pids, p)
	}
	collect(pid)

	var firstErr error
	for _, p := range pids {
		if err := syscall.Kill(p, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
