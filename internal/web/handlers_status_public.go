package web

import (
	"net/http"
	"strings"
)

func (s *Server) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	snap := s.statusSnapshot.Load()
	if snap == nil || snap.disabled {
		http.NotFound(w, r)
		return
	}

	// Strict isolated Content-Security-Policy without external CDNs or WebSockets
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self'; font-src 'self' data:; img-src 'self' data:;")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("ETag", s.statusHTMLETag)
	w.Header().Set("Cache-Control", "public, max-age=60")

	if match := r.Header.Get("If-None-Match"); match != "" && match == s.statusHTMLETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(s.statusHTMLContentGz) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(s.statusHTMLContentGz)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(s.statusHTMLContent)
	}
}

func (s *Server) handlePublicStatusSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	clientIP := resolveClientIP(r)
	if s.statusLimiter != nil && !s.statusLimiter.Allow(clientIP) {
		w.Header().Set("Retry-After", "60")
		http.Error(w, `{"error":"rate_limit_exceeded"}`, http.StatusTooManyRequests)
		return
	}

	snap := s.statusSnapshot.Load()
	if snap == nil || snap.disabled {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"status_page_disabled"}`))
		return
	}

	w.Header().Set("ETag", snap.etag)
	w.Header().Set("Cache-Control", "public, max-age=15, stale-while-revalidate=30")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")

	if match := r.Header.Get("If-None-Match"); match != "" && match == snap.etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(snap.gzipJSON) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(snap.gzipJSON)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(snap.rawJSON)
	}
}
