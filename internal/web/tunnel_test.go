package web

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pantau/internal/inspector"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

func setupTunnelTestServer(t *testing.T, dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)) (*Server, *store.DB, *store.Host) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "pantau-tunnel-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	db, err := store.Open(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	host := &store.Host{
		Name: "test-node",
		Host: "192.168.1.100",
		Port: 22,
		User: "root",
	}
	id, err := db.CreateHost(host)
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	host.ID = id

	mockRunner := sshrunner.NewMockRunner()
	if dialFunc != nil {
		mockRunner.DialContextFunc = dialFunc
	}

	mockFactory := func(h string, p int, u, k string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}

	ins := inspector.New(db, mockFactory, nil)
	s := NewServer(db, ins, nil)
	return s, db, host
}

func TestTunnel_AuthEnforcement(t *testing.T) {
	s, _, host := setupTunnelTestServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/tunnel/%d/8080/", host.ID), nil)
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}
}

func TestTunnel_RouteValidation(t *testing.T) {
	s, _, host := setupTunnelTestServer(t, nil)
	token := "valid-session-token"
	s.sessions.Store(token, time.Now().Add(1*time.Hour))

	tests := []struct {
		name         string
		path         string
		expectedCode int
	}{
		{"empty path", "/api/tunnel/", http.StatusBadRequest},
		{"missing port", fmt.Sprintf("/api/tunnel/%d", host.ID), http.StatusBadRequest},
		{"invalid host ID", "/api/tunnel/invalid/8080/", http.StatusBadRequest},
		{"invalid port non-numeric", fmt.Sprintf("/api/tunnel/%d/abc/", host.ID), http.StatusBadRequest},
		{"port out of bounds", fmt.Sprintf("/api/tunnel/%d/70000/", host.ID), http.StatusBadRequest},
		{"host not found", "/api/tunnel/99999/8080/", http.StatusNotFound},
		{"prohibited ssh port", fmt.Sprintf("/api/tunnel/%d/22/", host.ID), http.StatusForbidden},
		{"trailing slash redirect", fmt.Sprintf("/api/tunnel/%d/8080", host.ID), http.StatusTemporaryRedirect},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
			rec := httptest.NewRecorder()
			s.mux.ServeHTTP(rec, req)

			if rec.Code != tc.expectedCode {
				t.Errorf("for path %s, expected code %d, got %d (body: %s)", tc.path, tc.expectedCode, rec.Code, rec.Body.String())
			}
			if tc.expectedCode == http.StatusTemporaryRedirect {
				expectedLoc := tc.path + "/"
				if loc := rec.Header().Get("Location"); loc != expectedLoc {
					t.Errorf("expected Location %s, got %s", expectedLoc, loc)
				}
			}
		})
	}
}

func TestTunnel_SuccessfulProxyAndRedirectRewrite(t *testing.T) {
	var dialedAddr string

	dialFunc := func(ctx context.Context, network, addr string) (net.Conn, error) {
		dialedAddr = addr
		clientConn, serverConn := net.Pipe()

		go func() {
			defer serverConn.Close()
			reader := bufio.NewReader(serverConn)
			req, err := http.ReadRequest(reader)
			if err != nil {
				return
			}

			// Validate proxy headers
			prefix := req.Header.Get("X-Forwarded-Prefix")
			if !strings.HasPrefix(prefix, "/api/tunnel/") {
				resp := "HTTP/1.1 500 Bad Header\r\nContent-Length: 0\r\n\r\n"
				_, _ = io.WriteString(serverConn, resp)
				return
			}

			// Test path routing: if request is /login, return redirect to /dashboard
			if req.URL.Path == "/login" {
				resp := "HTTP/1.1 302 Found\r\nLocation: /dashboard\r\nContent-Length: 0\r\n\r\n"
				_, _ = io.WriteString(serverConn, resp)
				return
			}

			// Normal response
			body := "Hello from remote tunneled service!"
			resp := fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\nContent-Type: text/plain\r\n\r\n%s", len(body), body)
			_, _ = io.WriteString(serverConn, resp)
		}()

		return clientConn, nil
	}

	s, _, host := setupTunnelTestServer(t, dialFunc)
	token := "valid-session-token"
	s.sessions.Store(token, time.Now().Add(1*time.Hour))

	// 1. Test normal HTTP GET
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/tunnel/%d/3000/hello", host.ID), nil)
	req.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	rec := httptest.NewRecorder()
	s.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "Hello from remote tunneled service!" {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
	if dialedAddr != "127.0.0.1:3000" {
		t.Errorf("expected dial to 127.0.0.1:3000, got %s", dialedAddr)
	}

	// 2. Test Location redirect rewriting
	req2 := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/tunnel/%d/3000/login", host.ID), nil)
	req2.AddCookie(&http.Cookie{Name: "pantau_session", Value: token})
	rec2 := httptest.NewRecorder()
	s.mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusFound {
		t.Fatalf("expected 302 Found, got %d", rec2.Code)
	}
	expectedLocation := fmt.Sprintf("/api/tunnel/%d/3000/dashboard", host.ID)
	if loc := rec2.Header().Get("Location"); loc != expectedLocation {
		t.Errorf("expected rewritten Location %s, got %s", expectedLocation, loc)
	}
}

func TestTunnel_IdleReaper(t *testing.T) {
	mockRunner := sshrunner.NewMockRunner()
	mockFactory := func(h string, p int, u, k string) (sshrunner.Runner, error) {
		return mockRunner, nil
	}

	tmpDir, _ := os.MkdirTemp("", "pantau-reaper-test-*")
	defer os.RemoveAll(tmpDir)
	db, _ := store.Open(filepath.Join(tmpDir, "test.db"))
	defer db.Close()

	host := &store.Host{Name: "reaper-node", Host: "10.0.0.1", Port: 22, User: "root"}
	hID, _ := db.CreateHost(host)
	host.ID = hID

	ins := inspector.New(db, mockFactory, nil)
	s := NewServer(db, ins, nil)

	// Simulate active tunnel connection
	tr, err := s.getTunnelTransport(host)
	if err != nil || tr == nil {
		t.Fatalf("failed to get tunnel transport: %v", err)
	}

	// Verify entry stored in tunnelRunners
	if _, ok := s.tunnelRunners.Load(host.ID); !ok {
		t.Fatalf("expected tunnel runner entry stored for host %d", host.ID)
	}

	// Active entry (< maxIdle) should NOT be reaped
	reaped := s.ReapIdleTunnelRunners(10 * time.Minute)
	if reaped != 0 {
		t.Errorf("expected 0 reaped for active entry, got %d", reaped)
	}
	if mockRunner.IsClosed() {
		t.Errorf("runner should not be closed yet")
	}

	// Simulate entry became idle past maxIdle
	entryVal, _ := s.tunnelRunners.Load(host.ID)
	entry := entryVal.(*tunnelRunnerEntry)
	entry.mu.Lock()
	entry.lastUsed = time.Now().Add(-15 * time.Minute)
	entry.mu.Unlock()

	// Reaper must evict and close idle runner
	reaped = s.ReapIdleTunnelRunners(10 * time.Minute)
	if reaped != 1 {
		t.Errorf("expected 1 reaped, got %d", reaped)
	}
	if !mockRunner.IsClosed() {
		t.Errorf("expected mock runner to be closed by idle reaper")
	}
	if _, ok := s.tunnelRunners.Load(host.ID); ok {
		t.Errorf("expected tunnel runner entry removed from tunnelRunners map")
	}
}
