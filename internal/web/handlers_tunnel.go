package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

type tunnelRunnerEntry struct {
	mu        sync.Mutex
	runner    sshrunner.Runner
	transport *http.Transport
	lastUsed  time.Time
}

// getTunnelTransport returns or creates a pooled http.Transport that dials over SSH direct-tcpip for the given host.
// ponytail: Caches connection per host to avoid 200ms SSH handshakes on every web asset request.
func (s *Server) getTunnelTransport(host *store.Host) (*http.Transport, error) {
	if val, ok := s.tunnelRunners.Load(host.ID); ok {
		entry := val.(*tunnelRunnerEntry)
		entry.mu.Lock()
		entry.lastUsed = time.Now()
		tr := entry.transport
		entry.mu.Unlock()
		if tr != nil {
			return tr, nil
		}
	}

	if s.inspector == nil {
		return nil, fmt.Errorf("inspector not available")
	}

	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		return nil, fmt.Errorf("connect to host %s: %w", host.Name, err)
	}

	var transport *http.Transport
	transport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// ponytail: Anti-SSRF enforcement - strictly dial target remote localhost port only
			conn, err := runner.DialContext(ctx, "tcp", addr)
			if err != nil {
				// On dial failure, evict runner and attempt single reconnection
				s.evictTunnelRunner(host.ID)
				freshRunner, freshErr := s.inspector.GetRunnerForHost(host)
				if freshErr == nil {
					freshConn, dialErr := freshRunner.DialContext(ctx, "tcp", addr)
					if dialErr == nil {
						newTransport := &http.Transport{
							DialContext: func(c context.Context, n, a string) (net.Conn, error) {
								return freshRunner.DialContext(c, "tcp", a)
							},
							MaxIdleConns:          100,
							IdleConnTimeout:       90 * time.Second,
							ResponseHeaderTimeout: 30 * time.Second,
							ExpectContinueTimeout: 1 * time.Second,
						}
						s.tunnelRunners.Store(host.ID, &tunnelRunnerEntry{
							runner:    freshRunner,
							transport: newTransport,
							lastUsed:  time.Now(),
						})
						return freshConn, nil
					}
					_ = freshRunner.Close()
				}
				return nil, err
			}
			return conn, nil
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	entry := &tunnelRunnerEntry{
		runner:    runner,
		transport: transport,
		lastUsed:  time.Now(),
	}
	s.tunnelRunners.Store(host.ID, entry)
	return transport, nil
}

// evictTunnelRunner closes idle connections and releases SSH resources for a host.
func (s *Server) evictTunnelRunner(hostID int64) {
	if val, loaded := s.tunnelRunners.LoadAndDelete(hostID); loaded {
		entry := val.(*tunnelRunnerEntry)
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if entry.transport != nil {
			entry.transport.CloseIdleConnections()
		}
		if entry.runner != nil {
			_ = entry.runner.Close()
		}
	}
}

// handleTunnel proxies HTTP and WebSocket traffic to remote host localhost ports via SSH direct-tcpip.
// Format: /api/tunnel/:hostId/:port/*
func (s *Server) handleTunnel(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/api/tunnel/")
	if tail == "" || tail == "/" {
		http.Error(w, "missing host ID and port in tunnel path", http.StatusBadRequest)
		return
	}

	parts := strings.SplitN(tail, "/", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		http.Error(w, "invalid tunnel route (expected /api/tunnel/:hostId/:port/)", http.StatusBadRequest)
		return
	}

	hostID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || hostID <= 0 {
		http.Error(w, "invalid host ID", http.StatusBadRequest)
		return
	}

	port, err := strconv.Atoi(parts[1])
	if err != nil || port < 1 || port > 65535 {
		http.Error(w, "invalid port number (must be 1-65535)", http.StatusBadRequest)
		return
	}

	// Enforce trailing slash for base root so relative HTML assets resolve correctly
	pathPrefix := fmt.Sprintf("/api/tunnel/%d/%d", hostID, port)
	if r.URL.Path == pathPrefix {
		target := pathPrefix + "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}

	// Verify host exists
	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}

	// Blacklist: prevent connecting to the target host's own SSH daemon via HTTP tunnel
	if port == host.Port {
		http.Error(w, fmt.Sprintf("access to host SSH port (%d) via web tunnel is prohibited", host.Port), http.StatusForbidden)
		return
	}

	transport, err := s.getTunnelTransport(host)
	if err != nil {
		logger.Error("tunnel", host.Name, fmt.Sprintf("Failed to initialize tunnel transport: %v", err))
		http.Error(w, fmt.Sprintf("SSH connection failed: %v", err), http.StatusBadGateway)
		return
	}

	targetHostPort := fmt.Sprintf("127.0.0.1:%d", port)

	proxy := &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(&url.URL{
				Scheme: "http",
				Host:   targetHostPort,
			})

			cleanPath := strings.TrimPrefix(pr.In.URL.Path, pathPrefix)
			if cleanPath == "" || !strings.HasPrefix(cleanPath, "/") {
				cleanPath = "/" + cleanPath
			}
			pr.Out.URL.Path = cleanPath
			pr.Out.URL.RawPath = ""

			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Prefix", pathPrefix)
			pr.Out.Host = targetHostPort
		},
		ModifyResponse: func(res *http.Response) error {
			// Rewrite Location header on redirects (301/302/307/308) to preserve tunnel subpath
			if loc := res.Header.Get("Location"); loc != "" {
				if strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, pathPrefix) {
					res.Header.Set("Location", pathPrefix+loc)
				}
			}
			return nil
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			logger.Error("tunnel", host.Name, fmt.Sprintf("Tunnel proxy error (port %d): %v", port, err))
			http.Error(rw, fmt.Sprintf("SSH Tunnel Error (Host: %s, Port: %d): %v", host.Name, port, err), http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}
