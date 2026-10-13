package logger

import (
	"bytes"
	"log/slog"
	"testing"
	"time"
)

func TestRingBufferCapacityAndFIFO(t *testing.T) {
	b := NewBroadcaster(3)
	for i := 1; i <= 5; i++ {
		b.Broadcast(Event{
			Message: "event",
			Attrs:   map[string]any{"idx": i},
		})
	}

	hist := b.History()
	if len(hist) != 3 {
		t.Fatalf("expected 3 entries in ring buffer, got %d", len(hist))
	}

	// Should contain events 3, 4, 5
	if hist[0].Attrs["idx"] != 3 || hist[2].Attrs["idx"] != 5 {
		t.Errorf("expected FIFO sequence 3..5, got %v -> %v", hist[0].Attrs["idx"], hist[2].Attrs["idx"])
	}
}

func TestBroadcastAndSubscribe(t *testing.T) {
	b := NewBroadcaster(10)
	ch, unsub := b.Subscribe()
	defer unsub()

	b.Broadcast(Event{
		Level:   "INFO",
		Module:  "ssh",
		Host:    "web-prod",
		Message: "Connection established",
	})

	select {
	case evt := <-ch:
		if evt.Host != "web-prod" || evt.Module != "ssh" {
			t.Errorf("unexpected event content: %+v", evt)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestRedactionFilter(t *testing.T) {
	b := NewBroadcaster(10)
	b.Broadcast(Event{
		Message: "Connecting with key -----BEGIN RSA PRIVATE KEY----- secret",
		Attrs: map[string]any{
			"password":     "supersecret123",
			"ssh_token":    "ghp_12345",
			"host_address": "192.168.1.10",
		},
	})

	hist := b.History()
	if len(hist) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(hist))
	}
	evt := hist[0]

	if evt.Attrs["password"] != "[REDACTED]" {
		t.Errorf("expected password to be redacted, got %v", evt.Attrs["password"])
	}
	if evt.Attrs["ssh_token"] != "[REDACTED]" {
		t.Errorf("expected ssh_token to be redacted, got %v", evt.Attrs["ssh_token"])
	}
	if evt.Attrs["host_address"] != "192.168.1.10" {
		t.Errorf("expected host_address to remain intact, got %v", evt.Attrs["host_address"])
	}
	if evt.Message != "[REDACTED PRIVATE KEY]" {
		t.Errorf("expected private key message to be redacted, got %s", evt.Message)
	}
}

func TestSetupGlobalLoggerAndEmit(t *testing.T) {
	var buf bytes.Buffer
	b := SetupGlobalLogger(&buf, slog.LevelInfo)
	ch, unsub := b.Subscribe()
	defer unsub()

	Info("inspect", "server-1", "Inspection completed", "duration_ms", 120)

	select {
	case evt := <-ch:
		if evt.Module != "inspect" || evt.Host != "server-1" || evt.Message != "Inspection completed" {
			t.Errorf("unexpected emitted event: %+v", evt)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for emitted event")
	}
}
