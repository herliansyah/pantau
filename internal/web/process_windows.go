//go:build windows

package web

import (
	"os/exec"
	"syscall"
)

// setDetachedProcess sets platform-specific process attributes so the launched terminal
// emulator runs in its own process group and outlives the Pantau server daemon.
func setDetachedProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000208, // CREATE_NEW_PROCESS_GROUP | DETACHED_PROCESS
	}
}
