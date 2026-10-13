package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"pantau/internal/logger"
)

func TestLogsStreamAuthAndSSE(t *testing.T) {
	broadcaster := logger.NewBroadcaster(10)
	logger.SetDefaultBroadcaster(broadcaster)

	broadcaster.Broadcast(logger.Event{
		Level:   "INFO",
		Module:  "inspect",
		Host:    "web-01",
		Message: "preseed_1",
	})
	broadcaster.Broadcast(logger.Event{
		Level:   "WARN",
		Module:  "ssh",
		Host:    "db-01",
		Message: "preseed_2",
	})

	srv := &Server{}
	// Setup route
	mux := http.NewServeMux()
	mux.HandleFunc("/api/logs/stream", srv.authMiddleware(srv.handleLogsStream))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// 1. Unauthenticated request should return 401 Unauthorized
	reqUnauth, _ := http.NewRequest("GET", ts.URL+"/api/logs/stream", nil)
	respUnauth, err := http.DefaultClient.Do(reqUnauth)
	if err != nil {
		t.Fatal(err)
	}
	respUnauth.Body.Close()
	if respUnauth.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", respUnauth.StatusCode)
	}

	// 2. Issue valid session
	sessionToken := "test_valid_session_token"
	srv.sessions.Store(sessionToken, time.Now().Add(1*time.Hour))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	reqAuth, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/logs/stream", nil)
	reqAuth.AddCookie(&http.Cookie{
		Name:  "pantau_session",
		Value: sessionToken,
	})

	respAuth, err := http.DefaultClient.Do(reqAuth)
	if err != nil {
		t.Fatal(err)
	}
	defer respAuth.Body.Close()

	if respAuth.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", respAuth.StatusCode)
	}
	if ct := respAuth.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("expected text/event-stream Content-Type, got %s", ct)
	}

	reader := bufio.NewReader(respAuth.Body)
	readNextData := func() logger.Event {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				t.Fatalf("error reading stream line: %v", err)
			}
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "data:") {
				jsonStr := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				var evt logger.Event
				if err := json.Unmarshal([]byte(jsonStr), &evt); err != nil {
					t.Fatalf("failed to unmarshal SSE event: %v", err)
				}
				return evt
			}
		}
	}

	evt1 := readNextData()
	if evt1.Message != "preseed_1" || evt1.Host != "web-01" {
		t.Errorf("unexpected event 1: %+v", evt1)
	}

	evt2 := readNextData()
	if evt2.Message != "preseed_2" || evt2.Host != "db-01" {
		t.Errorf("unexpected event 2: %+v", evt2)
	}

	// 3. Live broadcast
	go func() {
		time.Sleep(50 * time.Millisecond)
		broadcaster.Broadcast(logger.Event{
			Level:   "ERROR",
			Module:  "transfer",
			Host:    "nas-01",
			Message: "live_transfer_event",
		})
	}()

	evt3 := readNextData()
	if evt3.Message != "live_transfer_event" || evt3.Host != "nas-01" || evt3.Level != "ERROR" {
		t.Errorf("unexpected live event: %+v", evt3)
	}
}
