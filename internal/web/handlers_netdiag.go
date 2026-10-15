package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/netdiag"
)

// handleNetDiagStream handles on-demand network diagnostic probes and streams live SSE events to the browser.
// ponytail: Uses standard library http.Flusher and SSE format with zero external dependencies.
func (s *Server) handleNetDiagStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"Streaming unsupported"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	q := r.URL.Query()
	sourceHostID, err := strconv.ParseInt(q.Get("source_host_id"), 10, 64)
	if err != nil || sourceHostID <= 0 {
		sendNetDiagSSE(w, flusher, netdiag.ProbeEvent{Type: "error", Message: "Invalid source host ID"})
		return
	}

	probeType := netdiag.ProbeType(strings.TrimSpace(q.Get("type")))
	targetType := strings.TrimSpace(q.Get("target_type"))
	targetHostID, _ := strconv.ParseInt(q.Get("target_host_id"), 10, 64)
	targetAddress := strings.TrimSpace(q.Get("target"))
	port, _ := strconv.Atoi(q.Get("port"))

	req := netdiag.ProbeRequest{
		SourceHostID:  sourceHostID,
		Type:          probeType,
		TargetType:    targetType,
		TargetHostID:  targetHostID,
		TargetAddress: targetAddress,
		Port:          port,
	}

	err = s.netdiagService.ExecuteProbe(r.Context(), req, func(evt netdiag.ProbeEvent) {
		sendNetDiagSSE(w, flusher, evt)
	})

	if err != nil {
		sendNetDiagSSE(w, flusher, netdiag.ProbeEvent{Type: "error", Message: err.Error()})
	}
}

func sendNetDiagSSE(w http.ResponseWriter, flusher http.Flusher, evt netdiag.ProbeEvent) {
	data, err := json.Marshal(evt)
	if err == nil {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}
