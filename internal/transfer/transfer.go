package transfer

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

type TransferJob struct {
	ID               int64      `json:"id"`
	SourceHostID     int64      `json:"source_host_id"`
	SourceHostName   string     `json:"source_host_name"`
	SourcePath       string     `json:"source_path"`
	DestHostID       int64      `json:"dest_host_id"`
	DestHostName     string     `json:"dest_host_name"`
	DestPath         string     `json:"dest_path"`
	Mode             string     `json:"mode"`   // "fast", "verified"
	Status           string     `json:"status"` // "queued", "transferring", "completed", "failed", "cancelled"
	IsDirectory      bool       `json:"is_directory"`
	TotalBytes       int64      `json:"total_bytes"`
	TransferredBytes int64      `json:"transferred_bytes"`
	SpeedBytesPerSec int64      `json:"speed_bytes_per_sec"`
	Percentage       float64    `json:"percentage"`
	ETASeconds       int64      `json:"eta_seconds"`
	Error            string     `json:"error,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`

	cancelFunc context.CancelFunc
}

type Manager struct {
	db       *store.DB
	factory  sshrunner.RunnerFactory
	mu       sync.RWMutex
	jobs     map[int64]*TransferJob
	jobOrder []int64
	nextID   int64
}

func NewManager(db *store.DB, factory sshrunner.RunnerFactory) *Manager {
	if factory == nil {
		factory = sshrunner.DefaultFactory()
	}
	return &Manager{
		db:      db,
		factory: factory,
		jobs:    make(map[int64]*TransferJob),
	}
}

func (m *Manager) getRunnerForHost(h *store.Host) (sshrunner.Runner, error) {
	key := h.CustomKey
	if strings.TrimSpace(key) == "" {
		globalKey, err := m.db.GetSetting("ssh_private_key")
		if err != nil {
			return nil, fmt.Errorf("get global ssh key: %w", err)
		}
		key = globalKey
	}
	return m.factory(h.Host, h.Port, h.User, key)
}

func (m *Manager) StartTransfer(srcHostID int64, srcPath string, dstHostID int64, dstPath string, mode string) (*TransferJob, error) {
	if srcHostID == dstHostID && srcPath == dstPath {
		return nil, fmt.Errorf("source and destination path cannot be identical on the same host")
	}

	srcHost, err := m.db.GetHost(srcHostID)
	if err != nil || srcHost == nil {
		return nil, fmt.Errorf("source host %d not found", srcHostID)
	}

	dstHost, err := m.db.GetHost(dstHostID)
	if err != nil || dstHost == nil {
		return nil, fmt.Errorf("destination host %d not found", dstHostID)
	}

	if mode != "verified" {
		mode = "fast"
	}

	ctx, cancel := context.WithCancel(context.Background())

	m.mu.Lock()
	m.nextID++
	job := &TransferJob{
		ID:             m.nextID,
		SourceHostID:   srcHostID,
		SourceHostName: srcHost.Name,
		SourcePath:     srcPath,
		DestHostID:     dstHostID,
		DestHostName:   dstHost.Name,
		DestPath:       dstPath,
		Mode:           mode,
		Status:         "queued",
		StartedAt:      time.Now(),
		cancelFunc:     cancel,
	}
	m.jobs[job.ID] = job
	m.jobOrder = append([]int64{job.ID}, m.jobOrder...)
	m.mu.Unlock()

	go m.runTransfer(ctx, job, srcHost, dstHost)

	return job, nil
}

func (m *Manager) runTransfer(ctx context.Context, job *TransferJob, srcHost, dstHost *store.Host) {
	srcRunner, err := m.getRunnerForHost(srcHost)
	if err != nil {
		m.failJob(job, fmt.Sprintf("connect source host failed: %v", err))
		return
	}
	defer srcRunner.Close()

	dstRunner, err := m.getRunnerForHost(dstHost)
	if err != nil {
		m.failJob(job, fmt.Sprintf("connect dest host failed: %v", err))
		return
	}
	defer dstRunner.Close()

	// 1. Inspect source: check if directory and calculate total size
	checkDirCmd := fmt.Sprintf(`test -d %q && echo "DIR" || echo "FILE"`, job.SourcePath)
	typeOut, _, _, _ := srcRunner.Exec(checkDirCmd)
	isDir := strings.TrimSpace(typeOut) == "DIR"

	sizeCmd := fmt.Sprintf(`du -sb %q 2>/dev/null | cut -f1`, job.SourcePath)
	sizeOut, _, _, _ := srcRunner.Exec(sizeCmd)
	totalBytes, _ := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
	if totalBytes <= 0 {
		statCmd := fmt.Sprintf(`stat -c "%%s" %q 2>/dev/null`, job.SourcePath)
		statOut, _, _, _ := srcRunner.Exec(statCmd)
		totalBytes, _ = strconv.ParseInt(strings.TrimSpace(statOut), 10, 64)
	}

	m.mu.Lock()
	job.Status = "transferring"
	job.IsDirectory = isDir
	job.TotalBytes = totalBytes
	m.mu.Unlock()

	// 2. Ensure destination directory exists
	var destDir string
	var destFile string
	if isDir {
		destDir = job.DestPath
	} else {
		destDir = path.Dir(job.DestPath)
		destFile = job.DestPath
	}
	_, _, _, _ = dstRunner.Exec(fmt.Sprintf(`mkdir -p %q`, destDir))

	// 3. Create stream pipe
	pr, pw := io.Pipe()

	var transferred int64
	cw := &countingWriter{
		w: pw,
		onWrite: func(n int) {
			atomic.AddInt64(&transferred, int64(n))
		},
	}

	// Metrics reporter loop
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	go func() {
		lastBytes := int64(0)
		lastTime := time.Now()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				current := atomic.LoadInt64(&transferred)
				now := time.Now()
				elapsedSec := now.Sub(lastTime).Seconds()
				if elapsedSec <= 0 {
					elapsedSec = 0.5
				}

				speed := int64(float64(current-lastBytes) / elapsedSec)
				lastBytes = current
				lastTime = now

				m.mu.Lock()
				job.TransferredBytes = current
				job.SpeedBytesPerSec = speed
				if job.TotalBytes > 0 {
					job.Percentage = (float64(current) / float64(job.TotalBytes)) * 100.0
					if job.Percentage > 100.0 {
						job.Percentage = 100.0
					}
					remaining := job.TotalBytes - current
					if remaining > 0 && speed > 0 {
						job.ETASeconds = remaining / speed
					} else {
						job.ETASeconds = 0
					}
				}
				m.mu.Unlock()
			}
		}
	}()

	// 4. Source command & Destination command
	var srcCmd string
	var dstCmd string

	if isDir {
		srcParent := path.Dir(job.SourcePath)
		srcBase := path.Base(job.SourcePath)
		srcCmd = fmt.Sprintf(`tar -cf - -C %q %q`, srcParent, srcBase)
		dstCmd = fmt.Sprintf(`tar -xf - -C %q`, job.DestPath)
	} else {
		srcCmd = fmt.Sprintf(`cat %q`, job.SourcePath)
		dstCmd = fmt.Sprintf(`cat > %q`, destFile)
	}

	errChan := make(chan error, 2)

	// Goroutine 1: Source reader -> writes to cw
	go func() {
		err := srcRunner.PipeCommand(srcCmd, nil, cw)
		_ = pw.CloseWithError(err)
		errChan <- err
	}()

	// Goroutine 2: Reads from pr -> sends to Destination
	go func() {
		err := dstRunner.PipeCommand(dstCmd, pr, nil)
		_ = pr.CloseWithError(err)
		errChan <- err
	}()

	// Wait for completion or context cancellation
	var execErr error
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			_ = pw.CloseWithError(context.Canceled)
			_ = pr.CloseWithError(context.Canceled)
			m.mu.Lock()
			job.Status = "cancelled"
			now := time.Now()
			job.CompletedAt = &now
			m.mu.Unlock()
			return
		case err := <-errChan:
			if err != nil && execErr == nil {
				execErr = err
			}
		}
	}

	if execErr != nil {
		m.failJob(job, fmt.Sprintf("stream transfer failed: %v", execErr))
		return
	}

	// 5. Verification Phase (if Verified Mode requested)
	if job.Mode == "verified" {
		if !isDir {
			srcHashOut, _, _, _ := srcRunner.Exec(fmt.Sprintf(`sha256sum %q | cut -d' ' -f1`, job.SourcePath))
			dstHashOut, _, _, _ := dstRunner.Exec(fmt.Sprintf(`sha256sum %q | cut -d' ' -f1`, destFile))

			srcHash := strings.TrimSpace(srcHashOut)
			dstHash := strings.TrimSpace(dstHashOut)

			if srcHash == "" || srcHash != dstHash {
				m.failJob(job, fmt.Sprintf("SHA256 checksum mismatch: source=%s, dest=%s", srcHash, dstHash))
				return
			}
		} else {
			dstCountOut, _, _, _ := dstRunner.Exec(fmt.Sprintf(`find %q -type f | wc -l`, job.DestPath))
			if strings.TrimSpace(dstCountOut) == "0" && job.TotalBytes > 0 {
				m.failJob(job, "directory transfer verification failed: destination has 0 files")
				return
			}
		}
	}

	m.mu.Lock()
	job.Status = "completed"
	job.Percentage = 100.0
	job.TransferredBytes = atomic.LoadInt64(&transferred)
	job.SpeedBytesPerSec = 0
	job.ETASeconds = 0
	now := time.Now()
	job.CompletedAt = &now
	m.mu.Unlock()
}

func (m *Manager) failJob(job *TransferJob, errStr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	job.Status = "failed"
	job.Error = errStr
	now := time.Now()
	job.CompletedAt = &now
}

func (m *Manager) CancelTransfer(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	job, ok := m.jobs[id]
	if !ok {
		return fmt.Errorf("job %d not found", id)
	}
	if job.Status == "transferring" || job.Status == "queued" {
		if job.cancelFunc != nil {
			job.cancelFunc()
		}
		job.Status = "cancelled"
		now := time.Now()
		job.CompletedAt = &now
	}
	return nil
}

func (m *Manager) ListJobs() []*TransferJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var list []*TransferJob
	for _, id := range m.jobOrder {
		if j, ok := m.jobs[id]; ok {
			list = append(list, j)
		}
	}
	return list
}

func (m *Manager) GetJob(id int64) *TransferJob {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.jobs[id]
}

type countingWriter struct {
	w       io.Writer
	onWrite func(n int)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}
