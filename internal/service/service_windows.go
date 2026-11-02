//go:build windows

package service

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func init() {
	sysProcAttr = func() any {
		return &syscall.SysProcAttr{CreationFlags: 0x00000200}
	}
	ctrlBreak = func(pid int) error {
		kernel32 := syscall.NewLazyDLL("kernel32.dll")
		proc := kernel32.NewProc("GenerateConsoleCtrlEvent")
		r, _, err := proc.Call(uintptr(1), uintptr(pid))
		if r == 0 {
			return err
		}
		return nil
	}
	defaultPidAlive = func(pid int) bool {
		h, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
		if err != nil {
			return false
		}
		defer syscall.CloseHandle(h)
		var code uint32
		if err := syscall.GetExitCodeProcess(h, &code); err != nil {
			return false
		}
		return code == 259
	}
	defaultHealth = func(port int) bool {
		return CheckHealth(context.Background(), port) == nil
	}
	defaultCommandLine = func(pid int) (string, error) {
		cmd := exec.Command("powershell", "-NoProfile", "-Command", fmt.Sprintf("(Get-CimInstance Win32_Process -Filter 'ProcessId=%d').CommandLine", pid))
		out, err := cmd.Output()
		return strings.TrimSpace(string(out)), err
	}
	defaultKill = func(pid int) error {
		return exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid)).Run()
	}
}
