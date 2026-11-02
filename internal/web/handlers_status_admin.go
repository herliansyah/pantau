package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"pantau/internal/store"
)

func (s *Server) handleStatusPageSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		enabled, _ := s.db.GetSetting("status_page_enabled")
		title, _ := s.db.GetSetting("status_page_title")
		if title == "" {
			title = "Pantau System Status"
		}
		org, _ := s.db.GetSetting("status_page_organization")
		if org == "" {
			org = "Pantau Infrastructure"
		}
		desc, _ := s.db.GetSetting("status_page_header_text")
		if desc == "" {
			desc = "Real-time service status and incident updates"
		}
		logo, _ := s.db.GetSetting("status_page_logo_url")
		supportURL, _ := s.db.GetSetting("status_page_support_url")

		resp := map[string]any{
			"enabled":      enabled == "true",
			"title":        title,
			"organization": org,
			"header_text":  desc,
			"logo_url":     logo,
			"support_url":  supportURL,
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Enabled      bool   `json:"enabled"`
			Title        string `json:"title"`
			Organization string `json:"organization"`
			HeaderText   string `json:"header_text"`
			LogoURL      string `json:"logo_url"`
			SupportURL   string `json:"support_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		enStr := "false"
		if req.Enabled {
			enStr = "true"
		}
		_ = s.db.SetSetting("status_page_enabled", enStr)
		_ = s.db.SetSetting("status_page_title", req.Title)
		_ = s.db.SetSetting("status_page_organization", req.Organization)
		_ = s.db.SetSetting("status_page_header_text", req.HeaderText)
		_ = s.db.SetSetting("status_page_logo_url", req.LogoURL)
		_ = s.db.SetSetting("status_page_support_url", req.SupportURL)

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleStatusPageTargets(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		targets, err := s.db.GetStatusPageTargets()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if targets == nil {
			targets = []store.StatusPageTarget{}
		}
		writeJSON(w, http.StatusOK, targets)
		return
	}

	if r.Method == http.MethodPost {
		var t store.StatusPageTarget
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(t.PublicName) == "" {
			http.Error(w, "public_name is required", http.StatusBadRequest)
			return
		}
		if t.GroupName == "" {
			t.GroupName = "Services"
		}

		if err := s.db.CreateStatusPageTarget(&t); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusCreated, t)
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleStatusPageTargetDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/settings/status-page/targets/")
	id, err := strconv.ParseInt(subpath, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid target id", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPut {
		var t store.StatusPageTarget
		if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		t.ID = id
		if strings.TrimSpace(t.PublicName) == "" {
			http.Error(w, "public_name is required", http.StatusBadRequest)
			return
		}

		if err := s.db.UpdateStatusPageTarget(&t); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusOK, t)
		return
	}

	if r.Method == http.MethodDelete {
		if err := s.db.DeleteStatusPageTarget(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleStatusPageIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		activeOnly := r.URL.Query().Get("active_only") == "true"
		incidents, err := s.db.GetStatusPageIncidents(activeOnly)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if incidents == nil {
			incidents = []store.StatusPageIncident{}
		}
		writeJSON(w, http.StatusOK, incidents)
		return
	}

	if r.Method == http.MethodPost {
		var inc store.StatusPageIncident
		if err := json.NewDecoder(r.Body).Decode(&inc); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(inc.Title) == "" {
			http.Error(w, "title is required", http.StatusBadRequest)
			return
		}
		if inc.Status == "" {
			inc.Status = "investigating"
		}
		if inc.Impact == "" {
			inc.Impact = "minor"
		}

		id, err := s.db.CreateStatusPageIncident(&inc)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		inc.ID = id

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusCreated, inc)
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleStatusPageIncidentDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/settings/status-page/incidents/")
	if strings.HasSuffix(subpath, "/updates") {
		s.handleStatusPageIncidentUpdates(w, r)
		return
	}

	id, err := strconv.ParseInt(subpath, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid incident id", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodGet {
		inc, err := s.db.GetStatusPageIncidentByID(id)
		if err != nil {
			http.Error(w, "incident not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, inc)
		return
	}

	if r.Method == http.MethodPut {
		var inc store.StatusPageIncident
		if err := json.NewDecoder(r.Body).Decode(&inc); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		inc.ID = id
		if strings.TrimSpace(inc.Title) == "" {
			http.Error(w, "title is required", http.StatusBadRequest)
			return
		}

		if err := s.db.UpdateStatusPageIncident(&inc); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusOK, inc)
		return
	}

	if r.Method == http.MethodDelete {
		if err := s.db.DeleteStatusPageIncident(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleStatusPageIncidentUpdates(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/settings/status-page/incidents/")
	subpath = strings.TrimSuffix(subpath, "/updates")
	id, err := strconv.ParseInt(subpath, 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "invalid incident id", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost {
		var req struct {
			Status  string `json:"status"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Message) == "" {
			http.Error(w, "message is required", http.StatusBadRequest)
			return
		}
		if req.Status == "" {
			req.Status = "monitoring"
		}

		if err := s.db.AddStatusPageIncidentUpdate(id, req.Status, req.Message); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.InvalidateStatusPageCache()
		writeJSON(w, http.StatusCreated, map[string]string{"status": "ok"})
		return
	}

	http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
}
