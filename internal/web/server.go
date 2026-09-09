package web

import (
	"encoding/json"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/bcrypt"
	"github.com/pkg/sftp"

	"pantau/internal/inspector"
	"pantau/internal/notify"
	"pantau/internal/sshrunner"
	"pantau/internal/snapshot"
	"pantau/internal/store"
	"pantau/internal/transfer"
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
	transferMgr *transfer.Manager
	snapshotMgr *snapshot.Manager
	mux         *http.ServeMux
	sessions    sync.Map // token -> expiry
}

func NewServer(db *store.DB, ins *inspector.Inspector, disp *notify.Dispatcher) *Server {
	s := &Server{
		db:          db,
		inspector:   ins,
		dispatcher:  disp,
		provisioner: sshrunner.DefaultKeyProvisioner,
		transferMgr: transfer.NewManager(db, nil),
		snapshotMgr: snapshot.NewManager(db, nil),
		mux:         http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) SetKeyProvisioner(p sshrunner.KeyProvisioner) {
	s.provisioner = p
}

func (s *Server) SetTransferManager(tm *transfer.Manager) {
	s.transferMgr = tm
}
func (s *Server) SetSnapshotManager(sm *snapshot.Manager) {
	s.snapshotMgr = sm
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

	// Notes
	s.mux.HandleFunc("/api/notes", s.authMiddleware(s.handleGlobalNotes))

	// Settings & Alerts
	s.mux.HandleFunc("/api/settings", s.authMiddleware(s.handleSettings))
	s.mux.HandleFunc("/api/settings/test-notify", s.authMiddleware(s.handleTestNotify))
	s.mux.HandleFunc("/api/settings/regenerate-ssh-key", s.authMiddleware(s.handleRegenerateSSHKey))
	s.mux.HandleFunc("/api/alerts", s.authMiddleware(s.handleAlerts))
	s.mux.HandleFunc("/api/alerts/", s.authMiddleware(s.handleAlertDetail))

	// Cross-Host Transfers
	s.mux.HandleFunc("/api/transfers", s.authMiddleware(s.handleTransfers))
	s.mux.HandleFunc("/api/transfers/", s.authMiddleware(s.handleTransferDetail))

	// WebSockets (Terminal & Docker Logs)
	s.mux.HandleFunc("/ws/terminal", s.handleWSTerminal)
	s.mux.HandleFunc("/ws/docker/logs", s.handleWSDockerLogs)

	// Snapshots & GitHub Backup
	s.mux.HandleFunc("/api/snapshot/status", s.handleSnapshotStatus)
	s.mux.HandleFunc("/api/snapshot/bootstrap/start-fresh", s.handleSnapshotStartFresh)
	s.mux.HandleFunc("/api/snapshot/export", s.authMiddleware(s.handleSnapshotExport))
	s.mux.HandleFunc("/api/snapshot/import", s.handleSnapshotImport)
	s.mux.HandleFunc("/api/snapshot/github/sync", s.authMiddleware(s.handleSnapshotGitHubSync))
	s.mux.HandleFunc("/api/snapshot/github/restore", s.handleSnapshotGitHubRestore)
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
		h.Name = strings.TrimSpace(h.Name)
		h.Host = strings.TrimSpace(h.Host)
		h.User = strings.TrimSpace(h.User)
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
	if parts[0] == "reorder" {
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			IDs []int64 `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.db.ReorderHosts(req.IDs); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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
			h.Name = strings.TrimSpace(h.Name)
			h.Host = strings.TrimSpace(h.Host)
			h.User = strings.TrimSpace(h.User)
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
	case "notes":
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Notes string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.db.UpdateHostNotes(hostID, req.Notes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})

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

var protectedSystemPrefixes = []string{
	"/bin", "/boot", "/dev", "/etc", "/lib", "/lib64", "/lib32", "/proc", "/root", "/sbin", "/sys", "/usr", "/run",
}

func isProtectedPathString(p string) bool {
	clean := path.Clean(p)
	if !path.IsAbs(clean) {
		clean = path.Clean("/" + clean)
	}
	if clean == "/" || clean == "." {
		return true
	}
	for _, prefix := range protectedSystemPrefixes {
		if clean == prefix || strings.HasPrefix(clean, prefix+"/") {
			return true
		}
	}
	return false
}

func isProtectedPath(p string, sftpClient *sftp.Client) bool {
	if isProtectedPathString(p) {
		return true
	}
	if sftpClient == nil {
		return false
	}
	clean := path.Clean(p)
	if !path.IsAbs(clean) {
		clean = path.Clean("/" + clean)
	}

	curr := clean
	for curr != "" && curr != "." {
		if real, err := sftpClient.RealPath(curr); err == nil {
			if isProtectedPathString(real) {
				return true
			}
			break
		}
		parent := path.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}
	return false
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
		dirPath := r.URL.Query().Get("path")
		if dirPath == "" {
			dirPath = "/"
		}
		entries, err := sftpClient.ReadDir(dirPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		type FileEntry struct {
			Name        string `json:"name"`
			IsDir       bool   `json:"is_dir"`
			Size        int64  `json:"size"`
			Mode        string `json:"mode"`
			ModTime     string `json:"mod_time"`
			IsProtected bool   `json:"is_protected"`
		}
		dirProtected := isProtectedPath(dirPath, sftpClient)
		var list []FileEntry
		for _, e := range entries {
			entryPath := path.Join(dirPath, e.Name())
			itemProtected := dirProtected
			if !itemProtected {
				if isProtectedPathString(entryPath) {
					itemProtected = true
				} else if e.Mode()&os.ModeSymlink != 0 {
					itemProtected = isProtectedPath(entryPath, sftpClient)
				}
			}
			list = append(list, FileEntry{
				Name:        e.Name(),
				IsDir:       e.IsDir(),
				Size:        e.Size(),
				Mode:        e.Mode().String(),
				ModTime:     e.ModTime().Format("2006-01-02 15:04"),
				IsProtected: itemProtected,
			})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"path":         dirPath,
			"is_protected": dirProtected,
			"files":        list,
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
		if isProtectedPath(req.Path, sftpClient) {
			http.Error(w, "protected path: cannot modify system location", http.StatusForbidden)
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
		overwrite := r.URL.Query().Get("overwrite") == "true"
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()

		destPath := path.Join(targetDir, header.Filename)
		if isProtectedPath(destPath, sftpClient) {
			http.Error(w, "protected path: cannot upload to system location", http.StatusForbidden)
			return
		}
		if !overwrite {
			if _, err := sftpClient.Stat(destPath); err == nil {
				http.Error(w, "file already exists", http.StatusConflict)
				return
			}
		}
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

	case "size":
		targetPath := r.URL.Query().Get("path")
		if targetPath == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		out, _, _, _ := runner.Exec(fmt.Sprintf(`du -sb %q 2>/dev/null | cut -f1`, targetPath))
		size, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		writeJSON(w, http.StatusOK, map[string]int64{"size": size})

	case "download":
		targetPath := r.URL.Query().Get("path")
		if targetPath == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		st, err := sftpClient.Stat(targetPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if st.IsDir() {
			base := path.Base(targetPath)
			parent := path.Dir(targetPath)

			hasZipOut, _, _, _ := runner.Exec("which zip 2>/dev/null")
			hasZip := strings.TrimSpace(hasZipOut) != ""

			if hasZip {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+".zip"))
				w.Header().Set("Content-Type", "application/zip")
				cmd := fmt.Sprintf(`cd %q && zip -r -q - %q`, parent, base)
				_ = runner.PipeCommand(cmd, nil, w)
			} else {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+".tar.gz"))
				w.Header().Set("Content-Type", "application/gzip")
				cmd := fmt.Sprintf(`tar -czf - -C %q %q`, parent, base)
				_ = runner.PipeCommand(cmd, nil, w)
			}
			return
		}

		f, err := sftpClient.Open(targetPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer f.Close()

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", path.Base(targetPath)))
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
		if isProtectedPath(req.Path, sftpClient) {
			http.Error(w, "protected path: cannot delete system location", http.StatusForbidden)
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
		if isProtectedPath(req.Path, sftpClient) {
			http.Error(w, "protected path: cannot modify system location", http.StatusForbidden)
			return
		}
		modeInt, _ := strconv.ParseUint(req.Mode, 8, 32)
		_ = sftpClient.Chmod(req.Path, os.FileMode(modeInt))
		writeJSON(w, http.StatusOK, map[string]string{"status": "chmod_ok"})

	default:
		http.Error(w, "unsupported file action", http.StatusBadRequest)
	}
}

func (s *Server) handleGlobalNotes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		notes, err := s.db.GetGlobalNotes()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"notes": notes})
	case http.MethodPut, http.MethodPost:
		var req struct {
			Notes string `json:"notes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if err := s.db.SetGlobalNotes(req.Notes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
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
		ghEnabled, _ := s.db.GetSetting("github_backup_enabled")
		ghRepo, _ := s.db.GetSetting("github_repo")
		ghToken, _ := s.db.GetSetting("github_token")
		ghBranch, _ := s.db.GetSetting("github_branch")
		ghPath, _ := s.db.GetSetting("github_file_path")
		snapPass, _ := s.db.GetSetting("snapshot_passphrase")
		backupInterval, _ := s.db.GetSetting("backup_interval_hours")
		if backupInterval == "" {
			backupInterval = "24"
		}
		lastBackupTime, _ := s.db.GetSetting("last_backup_time")
		lastBackupStatus, _ := s.db.GetSetting("last_backup_status")

		writeJSON(w, http.StatusOK, map[string]string{
			"ssh_public_key":        pubKey,
			"telegram_token":        tgToken,
			"telegram_chat_id":      tgChat,
			"webhook_url":           webhook,
			"poll_interval_sec":     poll,
			"github_backup_enabled": ghEnabled,
			"github_repo":           ghRepo,
			"github_token":          ghToken,
			"github_branch":         ghBranch,
			"github_file_path":      ghPath,
			"snapshot_passphrase":   snapPass,
			"backup_interval_hours": backupInterval,
			"last_backup_time":      lastBackupTime,
			"last_backup_status":    lastBackupStatus,
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

func (s *Server) handleRegenerateSSHKey(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	newPub, err := s.db.RegenerateGlobalSSHKey()
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to regenerate ssh key: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":         "ok",
		"ssh_public_key": newPub,
	})
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

func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jobs := s.transferMgr.ListJobs()
		writeJSON(w, http.StatusOK, jobs)
	case http.MethodPost:
		var req struct {
			SourceHostID int64  `json:"source_host_id"`
			SourcePath   string `json:"source_path"`
			DestHostID   int64  `json:"dest_host_id"`
			DestPath     string `json:"dest_path"`
			Mode         string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if req.SourceHostID <= 0 || req.DestHostID <= 0 || req.SourcePath == "" || req.DestPath == "" {
			http.Error(w, "source and destination host and path are required", http.StatusBadRequest)
			return
		}
		job, err := s.transferMgr.StartTransfer(req.SourceHostID, req.SourcePath, req.DestHostID, req.DestPath, req.Mode)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusAccepted, job)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTransferDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/transfers/")
	parts := strings.Split(subpath, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "job id required", http.StatusBadRequest)
		return
	}
	jobID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid job id", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		job := s.transferMgr.GetJob(jobID)
		if job == nil {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, job)
		return
	}

	if len(parts) >= 2 && parts[1] == "cancel" && r.Method == http.MethodPost {
		_ = s.transferMgr.CancelTransfer(jobID)
		writeJSON(w, http.StatusOK, map[string]string{"status": "cancelled"})
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func (s *Server) checkAuthOrFresh(r *http.Request) bool {
	fresh, _ := s.db.IsFresh()
	if fresh {
		return true
	}
	cookie, err := r.Cookie("pantau_session")
	if err != nil || cookie.Value == "" {
		return false
	}
	exp, ok := s.sessions.Load(cookie.Value)
	if !ok || time.Now().After(exp.(time.Time)) {
		return false
	}
	return true
}

func (s *Server) handleSnapshotStatus(w http.ResponseWriter, r *http.Request) {
	fresh, _ := s.db.IsFresh()
	hosts, _ := s.db.ListHosts()
	ghEnabled, _ := s.db.GetSetting("github_backup_enabled")
	ghRepo, _ := s.db.GetSetting("github_repo")
	lastTime, _ := s.db.GetSetting("last_backup_time")
	lastStatus, _ := s.db.GetSetting("last_backup_status")
	interval, _ := s.db.GetSetting("backup_interval_hours")

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"fresh":                 fresh,
		"host_count":            len(hosts),
		"github_backup_enabled": ghEnabled == "1",
		"github_repo":           ghRepo,
		"last_backup_time":      lastTime,
		"last_backup_status":    lastStatus,
		"backup_interval_hours": interval,
	})
}

func (s *Server) handleSnapshotStartFresh(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuthOrFresh(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if err := s.db.MarkBootstrapCompleted(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func (s *Server) handleSnapshotExport(w http.ResponseWriter, r *http.Request) {
	passphrase := r.URL.Query().Get("passphrase")
	if r.Method == http.MethodPost {
		var req struct {
			Passphrase string `json:"passphrase"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Passphrase != "" {
			passphrase = req.Passphrase
		}
	}
	if strings.TrimSpace(passphrase) == "" {
		stored, _ := s.db.GetSetting("snapshot_passphrase")
		passphrase = stored
	}
	if strings.TrimSpace(passphrase) == "" {
		http.Error(w, `{"error":"passphrase required"}`, http.StatusBadRequest)
		return
	}

	payload, err := s.db.ExportSnapshot()
	if err != nil {
		http.Error(w, fmt.Sprintf("export error: %v", err), http.StatusInternalServerError)
		return
	}

	enc, err := snapshot.Encrypt(payload, passphrase)
	if err != nil {
		http.Error(w, fmt.Sprintf("encrypt error: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="pantau-state.enc"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(enc)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(enc)
}

func (s *Server) handleSnapshotImport(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuthOrFresh(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var data []byte
	var passphrase string

	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "failed to parse multipart form", http.StatusBadRequest)
			return
		}
		passphrase = r.FormValue("passphrase")
		file, _, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "file field required", http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, err = io.ReadAll(file)
		if err != nil {
			http.Error(w, "failed to read uploaded file", http.StatusBadRequest)
			return
		}
	} else {
		var req struct {
			Data       string `json:"data"`
			Passphrase string `json:"passphrase"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		passphrase = req.Passphrase
		var err error
		data, err = base64.StdEncoding.DecodeString(req.Data)
		if err != nil {
			http.Error(w, "invalid base64 data", http.StatusBadRequest)
			return
		}
	}

	if len(data) == 0 || strings.TrimSpace(passphrase) == "" {
		http.Error(w, `{"error":"file data and passphrase are required"}`, http.StatusBadRequest)
		return
	}

	payload, err := snapshot.Decrypt(data, passphrase)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	if err := s.db.ImportSnapshot(payload); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": fmt.Sprintf("import failed: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":             true,
		"hosts_imported": len(payload.Hosts),
		"message":        "System snapshot restored successfully",
	})
}

func (s *Server) handleSnapshotGitHubSync(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Repo       string `json:"repo"`
		Token      string `json:"token"`
		Branch     string `json:"branch"`
		Path       string `json:"path"`
		Passphrase string `json:"passphrase"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Repo != "" {
		_ = s.db.SetSetting("github_repo", snapshot.CleanRepo(req.Repo))
	}
	if req.Token != "" {
		_ = s.db.SetSetting("github_token", req.Token)
	}
	if req.Branch != "" {
		_ = s.db.SetSetting("github_branch", req.Branch)
	}
	if req.Path != "" {
		_ = s.db.SetSetting("github_file_path", req.Path)
	}
	if req.Passphrase != "" {
		_ = s.db.SetSetting("snapshot_passphrase", req.Passphrase)
	}

	if err := s.snapshotMgr.SyncNow(r.Context()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Synced to GitHub successfully"})
}

func (s *Server) handleSnapshotGitHubRestore(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuthOrFresh(r) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Repo       string `json:"repo"`
		Token      string `json:"token"`
		Branch     string `json:"branch"`
		Path       string `json:"path"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if req.Repo == "" {
		req.Repo, _ = s.db.GetSetting("github_repo")
	}
	if req.Token == "" {
		req.Token, _ = s.db.GetSetting("github_token")
	}
	if req.Passphrase == "" {
		req.Passphrase, _ = s.db.GetSetting("snapshot_passphrase")
	}

	if req.Repo == "" || req.Token == "" || req.Passphrase == "" {
		http.Error(w, `{"error":"repo, token, and passphrase are required"}`, http.StatusBadRequest)
		return
	}

	if err := s.snapshotMgr.RestoreFromGitHub(r.Context(), req.Repo, req.Token, req.Branch, req.Path, req.Passphrase); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Snapshot successfully restored from GitHub"})
}
