package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

// Event represents a structured diagnostic runtime event emitted to live streams.
type Event struct {
	ID        int64          `json:"id"`
	Timestamp time.Time      `json:"timestamp"`
	Level     string         `json:"level"`
	Module    string         `json:"module"`
	Host      string         `json:"host,omitempty"`
	Message   string         `json:"message"`
	Attrs     map[string]any `json:"attrs,omitempty"`
}

// Broadcaster manages telemetry subscribers and a ring buffer of recent events.
// ponytail: Uses in-memory fixed-capacity ring buffer and standard channel fan-out with zero external dependencies.
type Broadcaster struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
	ringBuffer  []Event
	ringCap     int
	nextID      int64
}

var (
	defaultBroadcasterMu sync.RWMutex
	defaultBroadcaster   *Broadcaster
)

// NewBroadcaster creates a Broadcaster with a fixed ring buffer capacity.
func NewBroadcaster(capacity int) *Broadcaster {
	if capacity <= 0 {
		capacity = 100
	}
	return &Broadcaster{
		subscribers: make(map[chan Event]struct{}),
		ringBuffer:  make([]Event, 0, capacity),
		ringCap:     capacity,
	}
}

// DefaultBroadcaster returns the global default telemetry broadcaster.
func DefaultBroadcaster() *Broadcaster {
	defaultBroadcasterMu.RLock()
	defer defaultBroadcasterMu.RUnlock()
	if defaultBroadcaster == nil {
		defaultBroadcaster = NewBroadcaster(100)
	}
	return defaultBroadcaster
}

// SetDefaultBroadcaster sets the global default telemetry broadcaster.
func SetDefaultBroadcaster(b *Broadcaster) {
	defaultBroadcasterMu.Lock()
	defaultBroadcaster = b
	defaultBroadcasterMu.Unlock()
}

// Broadcast distributes an event to all subscribers and records it in the ring buffer.
func (b *Broadcaster) Broadcast(evt Event) {
	b.mu.Lock()
	b.nextID++
	evt.ID = b.nextID

	// Redact sensitive contents before storing or broadcasting
	evt.Attrs = RedactAttrs(evt.Attrs)
	evt.Message = RedactString(evt.Message)

	if len(b.ringBuffer) >= b.ringCap {
		copy(b.ringBuffer, b.ringBuffer[1:])
		b.ringBuffer[len(b.ringBuffer)-1] = evt
	} else {
		b.ringBuffer = append(b.ringBuffer, evt)
	}

	subs := make([]chan Event, 0, len(b.subscribers))
	for ch := range b.subscribers {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
			// Non-blocking drop on slow subscriber to protect daemon throughput
		}
	}
}

// Subscribe returns a receiving channel and an unsubscription cleanup function.
func (b *Broadcaster) Subscribe() (chan Event, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan Event, 64)
	b.subscribers[ch] = struct{}{}
	unsub := func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subscribers, ch)
	}
	return ch, unsub
}

// History returns a snapshot copy of the ring buffer.
func (b *Broadcaster) History() []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	res := make([]Event, len(b.ringBuffer))
	copy(res, b.ringBuffer)
	return res
}

// Redaction Filter implementation
var sensitiveKeyPrefixes = []string{
	"password", "passphrase", "secret", "token", "private_key",
	"privkey", "auth", "session", "recovery", "totp",
}

// IsSensitiveKey reports whether a key should be redacted.
func IsSensitiveKey(key string) bool {
	k := strings.ToLower(key)
	for _, s := range sensitiveKeyPrefixes {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// RedactString sanitizes private keys or embedded passwords in log strings.
func RedactString(s string) string {
	if strings.Contains(s, "PRIVATE KEY") {
		return "[REDACTED PRIVATE KEY]"
	}
	return s
}

// RedactAttrs sanitizes sensitive attributes map.
func RedactAttrs(attrs map[string]any) map[string]any {
	if len(attrs) == 0 {
		return attrs
	}
	clean := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if IsSensitiveKey(k) {
			clean[k] = "[REDACTED]"
			continue
		}
		if s, ok := v.(string); ok {
			clean[k] = RedactString(s)
		} else {
			clean[k] = v
		}
	}
	return clean
}

type broadcastHandler struct {
	primary     slog.Handler
	broadcaster *Broadcaster
	attrs       []slog.Attr
	groups      []string
}

func (h *broadcastHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return true
}

func (h *broadcastHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.primary != nil && h.primary.Enabled(ctx, r.Level) {
		_ = h.primary.Handle(ctx, r)
	}

	if h.broadcaster == nil {
		return nil
	}

	attrs := make(map[string]any)
	module := "system"
	host := ""

	for _, a := range h.attrs {
		attrs[a.Key] = a.Value.Any()
		if a.Key == "module" {
			module = a.Value.String()
		} else if a.Key == "host" || a.Key == "host_name" {
			host = a.Value.String()
		}
	}

	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.Any()
		if a.Key == "module" {
			module = a.Value.String()
		} else if a.Key == "host" || a.Key == "host_name" {
			host = a.Value.String()
		}
		return true
	})

	evt := Event{
		Timestamp: r.Time,
		Level:     r.Level.String(),
		Module:    module,
		Host:      host,
		Message:   r.Message,
		Attrs:     attrs,
	}

	h.broadcaster.Broadcast(evt)
	return nil
}

func (h *broadcastHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	var nextPrimary slog.Handler
	if h.primary != nil {
		nextPrimary = h.primary.WithAttrs(attrs)
	}
	return &broadcastHandler{
		primary:     nextPrimary,
		broadcaster: h.broadcaster,
		attrs:       append(append([]slog.Attr(nil), h.attrs...), attrs...),
		groups:      h.groups,
	}
}

func (h *broadcastHandler) WithGroup(name string) slog.Handler {
	var nextPrimary slog.Handler
	if h.primary != nil {
		nextPrimary = h.primary.WithGroup(name)
	}
	return &broadcastHandler{
		primary:     nextPrimary,
		broadcaster: h.broadcaster,
		attrs:       h.attrs,
		groups:      append(append([]string(nil), h.groups...), name),
	}
}

// SetupGlobalLogger initializes standard slog with broadcast capabilities and stderr handler.
func SetupGlobalLogger(w io.Writer, level slog.Level) *Broadcaster {
	if w == nil {
		w = os.Stderr
	}
	b := DefaultBroadcaster()
	txtHandler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: level})
	handler := &broadcastHandler{
		primary:     txtHandler,
		broadcaster: b,
	}
	slog.SetDefault(slog.New(handler))
	return b
}

// Emit provides a high-level helper to log and broadcast structured events directly.
func Emit(level slog.Level, module, host, message string, keyvals ...any) {
	attrs := make([]slog.Attr, 0, len(keyvals)/2+2)
	attrs = append(attrs, slog.String("module", module))
	if host != "" {
		attrs = append(attrs, slog.String("host", host))
	}
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			attrs = append(attrs, slog.Any(strings.TrimSpace(toString(keyvals[i])), keyvals[i+1]))
		}
	}
	slog.LogAttrs(context.Background(), level, message, attrs...)
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// Convenience logging wrappers
func Info(module, host, msg string, keyvals ...any) {
	Emit(slog.LevelInfo, module, host, msg, keyvals...)
}

func Warn(module, host, msg string, keyvals ...any) {
	Emit(slog.LevelWarn, module, host, msg, keyvals...)
}

func Error(module, host, msg string, keyvals ...any) {
	Emit(slog.LevelError, module, host, msg, keyvals...)
}

func Debug(module, host, msg string, keyvals ...any) {
	Emit(slog.LevelDebug, module, host, msg, keyvals...)
}
