package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

type statusPageSnapshot struct {
	etag        string
	rawJSON     []byte
	gzipJSON    []byte
	generatedAt time.Time
	disabled    bool
}

// InvalidateStatusPageCache schedules an asynchronous debounced rebuild of the status page snapshot.
func (s *Server) InvalidateStatusPageCache() {
	if s.statusInvalidateCh == nil {
		return
	}
	select {
	case s.statusInvalidateCh <- struct{}{}:
	default:
		// Rebuild is already queued
	}
}

func (s *Server) rebuildStatusPageSnapshot() {
	if s.db == nil {
		return
	}

	enabled, _ := s.db.GetSetting("status_page_enabled")
	if enabled != "true" {
		s.statusSnapshot.Store(&statusPageSnapshot{
			disabled:    true,
			generatedAt: time.Now().UTC(),
		})
		return
	}

	summary, err := s.db.GetPublicStatusSummary()
	if err != nil {
		return
	}

	rawJSON, err := json.Marshal(summary)
	if err != nil {
		return
	}

	sum := sha256.Sum256(rawJSON)
	etag := fmt.Sprintf("\"%x\"", sum[:8])

	var gzBuf bytes.Buffer
	gw, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	if err == nil {
		_, _ = gw.Write(rawJSON)
		_ = gw.Close()
	}

	snap := &statusPageSnapshot{
		etag:        etag,
		rawJSON:     rawJSON,
		gzipJSON:    gzBuf.Bytes(),
		generatedAt: time.Now().UTC(),
		disabled:    false,
	}

	s.statusSnapshot.Store(snap)
}

func (s *Server) startStatusPageCacheWorker(ctx context.Context) {
	// Rebuild initial snapshot immediately
	s.rebuildStatusPageSnapshot()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.rebuildStatusPageSnapshot()
		case <-s.statusInvalidateCh:
			// ponytail: Debounce 300ms to coalesce batch inspection events
			select {
			case <-ctx.Done():
				return
			case <-time.After(300 * time.Millisecond):
			}
			// Drain any pending notifications queued during the sleep
			for {
				select {
				case <-s.statusInvalidateCh:
				default:
					goto drained
				}
			}
		drained:
			s.rebuildStatusPageSnapshot()
		}
	}
}
