package instance

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

var ErrAlreadyRunning = errors.New("pantau instance already running")

type Metadata struct {
	PID       int       `json:"pid"`
	Port      int       `json:"port"`
	StartedAt time.Time `json:"started_at"`
}

type Guard struct {
	file     *os.File
	lockPath string
}

// UpdatePort updates the recorded active port and PID in the lockfile.
func (g *Guard) UpdatePort(port int) error {
	if g == nil || g.file == nil {
		return nil
	}
	meta := Metadata{
		PID:       os.Getpid(),
		Port:      port,
		StartedAt: time.Now(),
	}
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}

	if err := g.file.Truncate(0); err != nil {
		return err
	}
	if _, err := g.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	_, err = g.file.Write(data)
	_ = g.file.Sync()
	return err
}

// Close releases the OS lock and removes the lock file.
func (g *Guard) Close() error {
	if g == nil || g.file == nil {
		return nil
	}
	_ = unlockFile(g.file)
	closeErr := g.file.Close()
	_ = os.Remove(g.lockPath)
	g.file = nil
	return closeErr
}

// Acquire attempts to acquire an exclusive OS-level advisory lock on <dbPath>.lock.
// If another instance already holds the lock, it reads the active metadata and returns ErrAlreadyRunning.
// ponytail: stdlib and native kernel locking (flock/LockFileEx) automatically clean up on process exit/crash.
func Acquire(dbPath string, defaultPort int) (*Guard, *Metadata, error) {
	absDB, err := filepath.Abs(dbPath)
	if err != nil {
		absDB = dbPath
	}
	lockPath := absDB + ".lock"

	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, nil, fmt.Errorf("open lockfile: %w", err)
	}

	if err := lockFile(f); err != nil {
		// Locked by another instance. Read existing metadata.
		var meta Metadata
		buf, readErr := io.ReadAll(f)
		_ = f.Close()

		if readErr == nil && len(buf) > 0 {
			_ = json.Unmarshal(buf, &meta)
		}
		if meta.Port == 0 {
			meta.Port = defaultPort
		}
		return nil, &meta, ErrAlreadyRunning
	}

	g := &Guard{
		file:     f,
		lockPath: lockPath,
	}
	_ = g.UpdatePort(defaultPort)
	return g, nil, nil
}
