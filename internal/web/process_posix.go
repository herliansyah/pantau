//go:build !windows

package web

import (
	"os/exec"
	"syscall"
)

// setDetachedProcess sets platform-specific process attributes so the launched terminal
// emulator runs in its own process group and outlives the Pantau server daemon.
func setDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
