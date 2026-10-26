package web

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"pantau/internal/netdiag"
	"pantau/internal/store"
)

var validHostnameRegex = regexp.MustCompile(`^([a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`)

func isValidProbeTarget(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || len(target) > 253 {
		return false
	}
	if ip := net.ParseIP(target); ip != nil {
		return true
	}
	if strings.Contains(target, "://") || strings.Contains(target, ":") || strings.Contains(target, "/") {
		return false
	}
	return validHostnameRegex.MatchString(target)
}

// handleProbeTargetPresets handles CRUD operations for saved network probe target presets.
func (s *Server) handleProbeTargetPresets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		presets, err := s.db.ListProbeTargetPresets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, presets)

	case http.MethodPost:
		var req struct {
			Name              string  `json:"name"`
			Target            string  `json:"target"`
			Category          string  `json:"category"`
			SortOrder         int     `json:"sort_order"`
			AppEndpoint       string  `json:"app_endpoint"`
			BaselineMTU       int     `json:"baseline_mtu"`
			MaxLatencyMs      float64 `json:"max_latency_ms"`
			MaxLossPct        float64 `json:"max_loss_pct"`
			DBPort            int     `json:"db_port"`
			AutoTriageEnabled bool    `json:"auto_triage_enabled"`
			SourceHostID      *int64  `json:"source_host_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Target = strings.TrimSpace(req.Target)
		req.Category = strings.TrimSpace(req.Category)
		req.AppEndpoint = strings.TrimSpace(req.AppEndpoint)
		if req.Name == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		if !isValidProbeTarget(req.Target) {
			http.Error(w, `{"error":"invalid target IP or hostname"}`, http.StatusBadRequest)
			return
		}

		preset := &store.ProbeTargetPreset{
			Name:              req.Name,
			Target:            req.Target,
			Category:          req.Category,
			SortOrder:         req.SortOrder,
			AppEndpoint:       req.AppEndpoint,
			BaselineMTU:       req.BaselineMTU,
			MaxLatencyMs:      req.MaxLatencyMs,
			MaxLossPct:        req.MaxLossPct,
			DBPort:            req.DBPort,
			AutoTriageEnabled: req.AutoTriageEnabled,
			SourceHostID:      req.SourceHostID,
		}
		id, err := s.db.CreateProbeTargetPreset(preset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		preset.ID = id
		writeJSON(w, http.StatusCreated, preset)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProbeTargetPresetDetail handles single preset inspection, modification, and deletion.
func (s *Server) handleProbeTargetPresetDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/netdiag/presets/")
	parts := strings.Split(subpath, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "preset id required", http.StatusBadRequest)
		return
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid preset id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		preset, err := s.db.GetProbeTargetPreset(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if preset == nil {
			http.Error(w, "preset not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, preset)

	case http.MethodPut:
		var req struct {
			Name              string  `json:"name"`
			Target            string  `json:"target"`
			Category          string  `json:"category"`
			SortOrder         int     `json:"sort_order"`
			AppEndpoint       string  `json:"app_endpoint"`
			BaselineMTU       int     `json:"baseline_mtu"`
			MaxLatencyMs      float64 `json:"max_latency_ms"`
			MaxLossPct        float64 `json:"max_loss_pct"`
			DBPort            int     `json:"db_port"`
			AutoTriageEnabled bool    `json:"auto_triage_enabled"`
			SourceHostID      *int64  `json:"source_host_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Target = strings.TrimSpace(req.Target)
		req.Category = strings.TrimSpace(req.Category)
		req.AppEndpoint = strings.TrimSpace(req.AppEndpoint)
		if req.Name == "" {
			http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
			return
		}
		if !isValidProbeTarget(req.Target) {
			http.Error(w, `{"error":"invalid target IP or hostname"}`, http.StatusBadRequest)
			return
		}

		preset := &store.ProbeTargetPreset{
			ID:                id,
			Name:              req.Name,
			Target:            req.Target,
			Category:          req.Category,
			SortOrder:         req.SortOrder,
			AppEndpoint:       req.AppEndpoint,
			BaselineMTU:       req.BaselineMTU,
			MaxLatencyMs:      req.MaxLatencyMs,
			MaxLossPct:        req.MaxLossPct,
			DBPort:            req.DBPort,
			AutoTriageEnabled: req.AutoTriageEnabled,
			SourceHostID:      req.SourceHostID,
		}
		if err := s.db.UpdateProbeTargetPreset(preset); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, preset)

	case http.MethodDelete:
		if err := s.db.DeleteProbeTargetPreset(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

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

// handleNetDiagTriageStream handles automated multi-layer triage matrix evaluation streamed via SSE.
func (s *Server) handleNetDiagTriageStream(w http.ResponseWriter, r *http.Request) {
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
		sendNetDiagTriageSSE(w, flusher, netdiag.TriageEvent{Type: "error", Message: "Invalid source host ID"})
		return
	}

	presetID, _ := strconv.ParseInt(q.Get("preset_id"), 10, 64)
	targetAddress := strings.TrimSpace(q.Get("target"))
	targetName := strings.TrimSpace(q.Get("target_name"))
	appEndpoint := strings.TrimSpace(q.Get("app_endpoint"))
	baselineMTU, _ := strconv.Atoi(q.Get("baseline_mtu"))
	maxLatencyMs, _ := strconv.ParseFloat(q.Get("max_latency_ms"), 64)
	maxLossPct, _ := strconv.ParseFloat(q.Get("max_loss_pct"), 64)
	dbPort, _ := strconv.Atoi(q.Get("db_port"))

	req := netdiag.TriageRequest{
		SourceHostID:  sourceHostID,
		PresetID:      presetID,
		TargetAddress: targetAddress,
		TargetName:    targetName,
		AppEndpoint:   appEndpoint,
		BaselineMTU:   baselineMTU,
		MaxLatencyMs:  maxLatencyMs,
		MaxLossPct:    maxLossPct,
		DBPort:        dbPort,
	}

	_, err = s.netdiagService.ExecuteTriage(r.Context(), req, func(evt netdiag.TriageEvent) {
		sendNetDiagTriageSSE(w, flusher, evt)
	})

	if err != nil {
		sendNetDiagTriageSSE(w, flusher, netdiag.TriageEvent{Type: "error", Message: err.Error()})
	}
}

func sendNetDiagTriageSSE(w http.ResponseWriter, flusher http.Flusher, evt netdiag.TriageEvent) {
	data, err := json.Marshal(evt)
	if err == nil {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}
}

// handleTriageHistory handles listing and deletion of triage incident logs.
func (s *Server) handleTriageHistory(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/netdiag/triage/history")
	subpath = strings.TrimPrefix(subpath, "/")

	if subpath != "" {
		// Single log operations (DELETE)
		id, err := strconv.ParseInt(subpath, 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "invalid log id", http.StatusBadRequest)
			return
		}

		if r.Method == http.MethodDelete {
			if err := s.db.DeleteTriageIncidentLog(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
			return
		}

		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Method == http.MethodGet {
		var hostID int64
		if hidStr := r.URL.Query().Get("host_id"); hidStr != "" {
			hostID, _ = strconv.ParseInt(hidStr, 10, 64)
		}
		limit := 50
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}

		logs, err := s.db.ListTriageIncidentLogs(hostID, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, logs)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

// handleHostTriageConfig handles reading and updating per-host local app/db probe configuration.
func (s *Server) handleHostTriageConfig(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/netdiag/host-config/")
	subpath = strings.TrimPrefix(subpath, "/")
	id, err := strconv.ParseInt(subpath, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid host id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		app, _ := s.db.GetSetting(fmt.Sprintf("triage_host_%d_app", id))
		dbStr, _ := s.db.GetSetting(fmt.Sprintf("triage_host_%d_db", id))
		dbPort, _ := strconv.Atoi(dbStr)
		writeJSON(w, http.StatusOK, map[string]any{
			"host_id":      id,
			"app_endpoint": app,
			"db_port":      dbPort,
		})
	case http.MethodPut, http.MethodPost:
		var req struct {
			AppEndpoint string `json:"app_endpoint"`
			DBPort      int    `json:"db_port"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}
		_ = s.db.SetSetting(fmt.Sprintf("triage_host_%d_app", id), strings.TrimSpace(req.AppEndpoint))
		_ = s.db.SetSetting(fmt.Sprintf("triage_host_%d_db", id), strconv.Itoa(req.DBPort))
		writeJSON(w, http.StatusOK, map[string]any{
			"host_id":      id,
			"app_endpoint": strings.TrimSpace(req.AppEndpoint),
			"db_port":      req.DBPort,
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
