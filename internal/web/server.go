package web

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // ponytail: single-origin or local network usage
	},
}

type Server struct {
	db          *store.DB
	inspector   *inspector.Inspector
	dispatcher  *notify.Dispatcher
	provisioner sshrunner.KeyProvisioner
	mux         *http.ServeMux
	sessions    sync.Map // token -> expiry
}

func NewServer(db *store.DB, ins *inspector.Inspector, disp *notify.Dispatcher) *Server {
	s := &Server{
		db:          db,
		inspector:   ins,
		dispatcher:  disp,
		provisioner: sshrunner.DefaultKeyProvisioner,
		mux:         http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) SetKeyProvisioner(p sshrunner.KeyProvisioner) {
	s.provisioner = p
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Static / UI
	s.mux.HandleFunc("/", s.handleIndex)

	// Auth
	s.mux.HandleFunc("/api/login", s.handleLogin)
	s.mux.HandleFunc("/api/logout", s.handleLogout)
	s.mux.HandleFunc("/api/me", s.handleMe)
	s.mux.HandleFunc("/api/password", s.authMiddleware(s.handleChangePassword))

	// Hosts
	s.mux.HandleFunc("/api/hosts", s.authMiddleware(s.handleHosts))
	s.mux.HandleFunc("/api/hosts/", s.authMiddleware(s.handleHostDetailRoute))

	// Rules
	s.mux.HandleFunc("/api/rules/", s.authMiddleware(s.handleRuleRoute))

	// Settings & Alerts
	s.mux.HandleFunc("/api/settings", s.authMiddleware(s.handleSettings))
	s.mux.HandleFunc("/api/settings/test-notify", s.authMiddleware(s.handleTestNotify))
	s.mux.HandleFunc("/api/alerts", s.authMiddleware(s.handleAlerts))
	s.mux.HandleFunc("/api/alerts/", s.authMiddleware(s.handleAlertDetail))

	// WebSockets (Terminal & Docker Logs)
	s.mux.HandleFunc("/ws/terminal", s.handleWSTerminal)
	s.mux.HandleFunc("/ws/docker/logs", s.handleWSDockerLogs)
}

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("pantau_session")
		if err != nil || cookie.Value == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		exp, ok := s.sessions.Load(cookie.Value)
		if !ok || time.Now().After(exp.(time.Time)) {
			s.sessions.Delete(cookie.Value)
			http.Error(w, `{"error":"session_expired"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	hash, err := s.db.GetSetting("admin_password_hash")
	if err != nil || hash == "" {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		http.Error(w, `{"error":"invalid_password"}`, http.StatusUnauthorized)
		return
	}

	token := fmt.Sprintf("%d_%d", time.Now().UnixNano(), time.Now().Unix())
	expiry := time.Now().Add(24 * 7 * time.Hour)
	s.sessions.Store(token, expiry)

	http.SetCookie(w, &http.Cookie{
		Name:     "pantau_session",
		Value:    token,
		Path:     "/",
		Expires:  expiry,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("pantau_session")
	if err == nil {
		s.sessions.Delete(cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "pantau_session",
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
	})
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged_out"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("pantau_session")
	authenticated := false
	if err == nil && cookie.Value != "" {
		if exp, ok := s.sessions.Load(cookie.Value); ok && time.Now().Before(exp.(time.Time)) {
			authenticated = true
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"authenticated": authenticated})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if len(req.NewPassword) < 4 {
		http.Error(w, "new password too short", http.StatusBadRequest)
		return
	}

	hash, _ := s.db.GetSetting("admin_password_hash")
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.OldPassword)); err != nil {
		http.Error(w, `{"error":"invalid_old_password"}`, http.StatusBadRequest)
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	_ = s.db.SetSetting("admin_password_hash", string(newHash))
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_updated"})
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hosts, err := s.db.ListHosts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, hosts)
	case http.MethodPost:
		var req struct {
			store.Host
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		h := req.Host
		if h.Name == "" || h.Host == "" {
			http.Error(w, "name and host are required", http.StatusBadRequest)
			return
		}
		if h.Port <= 0 {
			h.Port = 22
		}
		if h.User == "" {
			h.User = "root"
		}

		// If one-time password provided, attempt key provisioning
		if strings.TrimSpace(req.Password) != "" {
			pubKey, err := s.db.GetSetting("ssh_public_key")
			if err != nil || pubKey == "" {
				http.Error(w, "global ssh public key not found", http.StatusInternalServerError)
				return
			}
			if err := s.provisioner(h.Host, h.Port, h.User, req.Password, pubKey); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]interface{}{
					"error": fmt.Sprintf("Injeksi SSH Key gagal: %v", err),
				})
				return
			}
		}

		id, err := s.db.CreateHost(&h)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.ID = id
		writeJSON(w, http.StatusCreated, h)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHostDetailRoute(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/hosts/")
	parts := strings.Split(subpath, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "host id required", http.StatusBadRequest)
		return
	}
	hostID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid host id", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		// /api/hosts/{id}
		switch r.Method {
		case http.MethodGet:
			host, err := s.db.GetHost(hostID)
			if err != nil || host == nil {
				http.Error(w, "host not found", http.StatusNotFound)
				return
			}
			rules, _ := s.db.ListDesiredRules(hostID)
			items, _ := s.db.ListActualItems(hostID)
			incidents, _ := s.db.ListIncidents(hostID, 20)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"host":         host,
				"rules":        rules,
				"actual_items": items,
				"incidents":    incidents,
			})
		case http.MethodPut:
			var h store.Host
			if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			h.ID = hostID
			if err := s.db.UpdateHost(&h); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		case http.MethodDelete:
			if err := s.db.DeleteHost(hostID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	action := parts[1]
	switch action {
	case "test":
		// POST /api/hosts/{id}/test
		host, err := s.db.GetHost(hostID)
		if err != nil || host == nil {
			http.Error(w, "host not found", http.StatusNotFound)
			return
		}
		runner, err := s.inspector.GetRunnerForHost(host)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		defer runner.Close()

		stdout, stderr, exitCode, err := runner.Exec("uname -srm; uptime")
		if err != nil || exitCode != 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": fmt.Sprintf("exit %d: %s %s", exitCode, stdout, stderr)})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": strings.TrimSpace(stdout)})

	case "inject-key":
		// POST /api/hosts/{id}/inject-key
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Password) == "" {
			http.Error(w, "password required", http.StatusBadRequest)
			return
		}
		host, err := s.db.GetHost(hostID)
		if err != nil || host == nil {
			http.Error(w, "host not found", http.StatusNotFound)
			return
		}

		pubKey, err := s.db.GetSetting("ssh_public_key")
		if err != nil || pubKey == "" {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": "global public key missing"})
			return
		}

		if err := s.provisioner(host.Host, host.Port, host.User, req.Password, pubKey); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": fmt.Sprintf("injeksi gagal: %v", err)})
			return
		}

		// Test connection immediately with key to confirm
		runner, err := s.inspector.GetRunnerForHost(host)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": fmt.Sprintf("key injected but verify connection failed: %v", err)})
			return
		}
		defer runner.Close()

		stdout, _, _, err := runner.Exec("uname -srm")
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": fmt.Sprintf("verify failed: %v", err)})
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"message": fmt.Sprintf("Public key berhasil di-inject ke %s! Verifikasi SSH sukses: %s", host.Host, strings.TrimSpace(stdout)),
		})
		return

	case "inspect":
		// POST /api/hosts/{id}/inspect
		err := s.inspector.InspectHost(hostID)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		host, _ := s.db.GetHost(hostID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "host": host})

	case "baseline":
		// POST /api/hosts/{id}/baseline
		rules, err := s.inspector.GenerateBaseline(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, rules)

	case "rules":
		// /api/hosts/{id}/rules
		switch r.Method {
		case http.MethodGet:
			rules, _ := s.db.ListDesiredRules(hostID)
			writeJSON(w, http.StatusOK, rules)
		case http.MethodPost:
			var rData store.DesiredRule
			if err := json.NewDecoder(r.Body).Decode(&rData); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			rData.HostID = hostID
			if err := s.db.SaveDesiredRule(&rData); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, rData)
		}

	case "docker":
		// /api/hosts/{id}/docker/action
		if len(parts) >= 3 && parts[2] == "action" && r.Method == http.MethodPost {
			var act struct {
				Container string `json:"container"`
				Action    string `json:"action"` // start, stop, restart
			}
			if err := json.NewDecoder(r.Body).Decode(&act); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			out, err := s.inspector.DockerAction(hostID, act.Container, act.Action)
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "output": out})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)

	case "files":
		s.handleFilesRoute(hostID, parts[2:], w, r)

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) handleRuleRoute(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/rules/")
	id, err := strconv.ParseInt(subpath, 10, 64)
	if err != nil {
		http.Error(w, "invalid rule id", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodDelete {
		_ = s.db.DeleteDesiredRule(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
		return
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleFilesRoute(hostID int64, subparts []string, w http.ResponseWriter, r *http.Request) {
	if len(subparts) == 0 {
		http.Error(w, "missing file action", http.StatusBadRequest)
		return
	}
	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		http.Error(w, "host not found", http.StatusNotFound)
		return
	}
	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer runner.Close()

	sftpClient, err := runner.SFTP()
	if err != nil {
		http.Error(w, fmt.Sprintf("sftp error: %v", err), http.StatusInternalServerError)
		return
	}
	defer sftpClient.Close()

	action := subparts[0]
	switch action {
	case "list":
		path := r.URL.Query().Get("path")
		if path == "" {
			path = "/"
		}
		entries, err := sftpClient.ReadDir(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		type FileEntry struct {
			Name    string `json:"name"`
			IsDir   bool   `json:"is_dir"`
			Size    int64  `json:"size"`
			Mode    string `json:"mode"`
			ModTime string `json:"mod_time"`
		}
		var list []FileEntry
		for _, e := range entries {
			list = append(list, FileEntry{
				Name:    e.Name(),
				IsDir:   e.IsDir(),
				Size:    e.Size(),
				Mode:    e.Mode().String(),
				ModTime: e.ModTime().Format("2006-01-02 15:04"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"path":  path,
			"files": list,
		})

	case "read":
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		f, err := sftpClient.Open(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()

		// Read up to 2MB for text editor
		buf := make([]byte, 2*1024*1024)
		n, _ := f.Read(buf)
		writeJSON(w, http.StatusOK, map[string]string{
			"path":    path,
			"content": string(buf[:n]),
		})

	case "write":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f, err := sftpClient.Create(req.Path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()

		_, err = f.Write([]byte(req.Content))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})

	case "upload":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		targetDir := r.URL.Query().Get("dir")
		if targetDir == "" {
			targetDir = "/"
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		destPath := filepath.Join(targetDir, header.Filename)
		dest, err := sftpClient.Create(destPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer dest.Close()

		_, err = io.Copy(dest, file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "path": destPath})

	case "download":
		path := r.URL.Query().Get("path")
		f, err := sftpClient.Open(path)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.Copy(w, f)

	case "delete":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = sftpClient.Remove(req.Path)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	case "chmod":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		modeInt, _ := strconv.ParseUint(req.Mode, 8, 32)
		_ = sftpClient.Chmod(req.Path, os.FileMode(modeInt))
		writeJSON(w, http.StatusOK, map[string]string{"status": "chmod_ok"})

	default:
		http.Error(w, "unsupported file action", http.StatusBadRequest)
	}
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pubKey, _ := s.db.GetSetting("ssh_public_key")
		tgToken, _ := s.db.GetSetting("telegram_token")
		tgChat, _ := s.db.GetSetting("telegram_chat_id")
		webhook, _ := s.db.GetSetting("webhook_url")
		poll, _ := s.db.GetSetting("poll_interval_sec")
		if poll == "" {
			poll = "300"
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"ssh_public_key":    pubKey,
			"telegram_token":    tgToken,
			"telegram_chat_id":  tgChat,
			"webhook_url":       webhook,
			"poll_interval_sec": poll,
		})
	case http.MethodPost:
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for k, v := range req {
			if k != "ssh_public_key" && k != "ssh_private_key" && k != "admin_password_hash" {
				_ = s.db.SetSetting(k, v)
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "settings_saved"})
	}
}

func (s *Server) handleTestNotify(w http.ResponseWriter, r *http.Request) {
	err := s.dispatcher.TestNotification()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	alerts, err := s.db.ListActiveAlerts()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, alerts)
}

func (s *Server) handleAlertDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/alerts/")
	parts := strings.Split(subpath, "/")
	if len(parts) >= 2 && parts[1] == "ack" && r.Method == http.MethodPost {
		id, _ := strconv.ParseInt(parts[0], 10, 64)
		_ = s.db.AcknowledgeAlert(id)
		writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged"})
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

// WebSocket Terminal
func (s *Server) handleWSTerminal(w http.ResponseWriter, r *http.Request) {
	hostIDStr := r.URL.Query().Get("host_id")
	hostID, err := strconv.ParseInt(hostIDStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid host_id", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("\r\nHost not found\r\n"))
		return
	}

	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\nSSH connect failed: %v\r\n", err)))
		return
	}
	defer runner.Close()

	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	resizeChan := make(chan [2]int, 4)

	// Pipe SSH output to WebSocket
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := outReader.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				_ = ws.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
		}
	}()

	// Read WebSocket input to SSH
	go func() {
		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				inWriter.Close()
				return
			}
			if msgType == websocket.TextMessage && len(data) > 0 && data[0] == '{' {
				// Window resize message: {"cols": 120, "rows": 40}
				var sz struct {
					Cols int `json:"cols"`
					Rows int `json:"rows"`
				}
				if json.Unmarshal(data, &sz) == nil && sz.Cols > 0 && sz.Rows > 0 {
					resizeChan <- [2]int{sz.Cols, sz.Rows}
					continue
				}
			}
			_, _ = inWriter.Write(data)
		}
	}()

	_ = runner.Terminal(inReader, outWriter, 120, 40, resizeChan)
}

// WebSocket Docker Log Stream
func (s *Server) handleWSDockerLogs(w http.ResponseWriter, r *http.Request) {
	hostIDStr := r.URL.Query().Get("host_id")
	container := r.URL.Query().Get("container")
	hostID, _ := strconv.ParseInt(hostIDStr, 10, 64)

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	host, err := s.db.GetHost(hostID)
	if err != nil || host == nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte("Host not found\n"))
		return
	}

	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("SSH failed: %v\n", err)))
		return
	}
	defer runner.Close()

	// Stream docker logs -f --tail 100
	outReader, outWriter := io.Pipe()

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := outReader.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				_ = ws.WriteMessage(websocket.TextMessage, buf[:n])
			}
		}
	}()

	// Close on websocket close
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				outWriter.Close()
				return
			}
		}
	}()

	cmd := fmt.Sprintf("docker logs -f --tail 100 %s", container)
	_ = runner.Stream(cmd, outWriter)
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(embeddedHTML))
}
