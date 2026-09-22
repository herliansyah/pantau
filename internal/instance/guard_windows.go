//go:build windows

package instance

import (
	"os"

	"golang.org/x/sys/windows"
)

// ponytail: native Windows LockFileEx provides OS-managed exclusive lock released on process termination.
func lockFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var ol windows.Overlapped
	return windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
}

func unlockFile(f *os.File) error {
	h := windows.Handle(f.Fd())
	var ol windows.Overlapped
	return windows.UnlockFileEx(h, 0, 1, 0, &ol)
}
