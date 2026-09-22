//go:build !windows

package instance

import (
	"os"
	"syscall"
)

// ponytail: stdlib syscall.Flock provides kernel-managed advisory locking without external daemon or stale lock risk.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
