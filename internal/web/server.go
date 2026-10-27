package web

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/bcrypt"

	"pantau/internal/inspector"
	"pantau/internal/logger"
	"pantau/internal/netdiag"
	"pantau/internal/notify"
	"pantau/internal/snapshot"
	"pantau/internal/sshrunner"
	"pantau/internal/store"
	"pantau/internal/transfer"
	"pantau/internal/updater"
)

var validContainerNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		u, err := url.Parse(origin)
		if err != nil {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	},
}

type temp2FAChallenge struct {
	expiry   time.Time
	attempts int
}

type pending2FASetupData struct {
	secret        string
	recoveryCodes []string
	hashedCodes   []string
	createdAt     time.Time
}

type Server struct {
	db                *store.DB
	inspector         *inspector.Inspector
	dispatcher        *notify.Dispatcher
	provisioner       sshrunner.KeyProvisioner
	transferMgr       *transfer.Manager
	snapshotMgr       *snapshot.Manager
	updaterMgr        *updater.Manager
	netdiagService    *netdiag.Service
	mux               *http.ServeMux
	sessions          sync.Map // token -> expiry
	temp2FAChallenges sync.Map // tempToken -> *temp2FAChallenge
	pending2FASetup   sync.Map // sessionToken -> *pending2FASetupData
	htmlContent       []byte
	htmlContentGz     []byte
	htmlETag          string
	docsFS            fs.FS
	tunnelRunners     sync.Map // hostID -> *tunnelRunnerEntry
	inspectAllMu      sync.Mutex
	inspectAllActive  bool
	lastInspectAll    time.Time
}

func (s *Server) prepareHTML(raw []byte) {
	s.htmlContent = raw
	sum := sha256.Sum256(raw)
	s.htmlETag = fmt.Sprintf(`"%x"`, sum[:8])

	// ponytail: pre-compress HTML in-memory with BestCompression so endpoint / requires zero CPU compression work per request and optimal wire size
	var buf bytes.Buffer
	gw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = gw.Write(raw)
	_ = gw.Close()
	s.htmlContentGz = buf.Bytes()
}

func NewServer(db *store.DB, ins *inspector.Inspector, disp *notify.Dispatcher) *Server {
	s := &Server{
		db:             db,
		inspector:      ins,
		dispatcher:     disp,
		provisioner:    sshrunner.DefaultKeyProvisioner,
		transferMgr:    transfer.NewManager(db, nil),
		snapshotMgr:    snapshot.NewManager(db, nil),
		netdiagService: netdiag.NewService(db, ins, disp),
		mux:            http.NewServeMux(),
	}
	s.prepareHTML(embeddedHTML)
	s.routes()
	return s
}

func (s *Server) StartAutoTriageScheduler(ctx context.Context) {
	if s.netdiagService != nil {
		go s.netdiagService.StartAutoTriageScheduler(ctx)
	}
}

func (s *Server) SetNetdiagService(ns *netdiag.Service) {
	s.netdiagService = ns
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

func (s *Server) SetUpdaterManager(um *updater.Manager) {
	s.updaterMgr = um
}

func (s *Server) SetVersion(version string) {
	if version != "" {
		newHTML := bytes.ReplaceAll(embeddedHTML, []byte(`<span class="footer-badge">v1.0.0</span>`), []byte(fmt.Sprintf(`<span class="footer-badge">%s</span>`, html.EscapeString(version))))
		s.prepareHTML(newHTML)
	}
}

func (s *Server) SetDocsFS(dfs fs.FS) {
	s.docsFS = dfs
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Static / UI
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/favicon.ico", s.handleFavicon)

	// In-App Documentation (Public & Private)
	s.mux.HandleFunc("/api/docs", s.handleDocs)

	// Vendor static assets (airgapped / local cache)
	vendorHandler := http.StripPrefix("/vendor/", http.FileServer(http.FS(vendorFS())))
	s.mux.HandleFunc("/vendor/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		vendorHandler.ServeHTTP(w, r)
	})

	// Auth & 2FA
	s.mux.HandleFunc("/api/login", s.handleLogin)
	s.mux.HandleFunc("/api/login/2fa", s.handleLogin2FA)
	s.mux.HandleFunc("/api/logout", s.handleLogout)
	s.mux.HandleFunc("/api/me", s.handleMe)
	s.mux.HandleFunc("/api/password", s.authMiddleware(s.handleChangePassword))
	s.mux.HandleFunc("/api/2fa/status", s.authMiddleware(s.handle2FAStatus))
	s.mux.HandleFunc("/api/2fa/setup", s.authMiddleware(s.handle2FASetup))
	s.mux.HandleFunc("/api/2fa/qr", s.authMiddleware(s.handle2FAQR))
	s.mux.HandleFunc("/api/2fa/enable", s.authMiddleware(s.handle2FAEnable))
	s.mux.HandleFunc("/api/2fa/disable", s.authMiddleware(s.handle2FADisable))

	// Hosts
	s.mux.HandleFunc("/api/hosts", s.authMiddleware(s.handleHosts))
	s.mux.HandleFunc("/api/hosts/", s.authMiddleware(s.handleHostDetailRoute))

	// Rules
	s.mux.HandleFunc("/api/rules/", s.authMiddleware(s.handleRuleRoute))

	// Global Notes & Pins
	s.mux.HandleFunc("/api/notes", s.authMiddleware(s.handleGlobalNotes))
	s.mux.HandleFunc("/api/pins", s.authMiddleware(s.handleGlobalPins))

	// Settings & Alerts
	s.mux.HandleFunc("/api/settings", s.authMiddleware(s.handleSettings))
	s.mux.HandleFunc("/api/settings/test-notify", s.authMiddleware(s.handleTestNotify))
	s.mux.HandleFunc("/api/settings/regenerate-ssh-key", s.authMiddleware(s.handleRegenerateSSHKey))
	s.mux.HandleFunc("/api/alerts", s.authMiddleware(s.handleAlerts))
	s.mux.HandleFunc("/api/alerts/", s.authMiddleware(s.handleAlertDetail))

	// Cross-Host Transfers
	s.mux.HandleFunc("/api/transfers", s.authMiddleware(s.handleTransfers))
	s.mux.HandleFunc("/api/transfers/", s.authMiddleware(s.handleTransferDetail))

	// Terminal Presets
	s.mux.HandleFunc("/api/presets", s.authMiddleware(s.handlePresets))
	s.mux.HandleFunc("/api/presets/", s.authMiddleware(s.handlePresetDetail))

	// WebSockets (Terminal & Docker Logs)
	s.mux.HandleFunc("/ws/terminal", s.authMiddleware(s.handleWSTerminal))
	s.mux.HandleFunc("/ws/docker/logs", s.authMiddleware(s.handleWSDockerLogs))

	// SSH Direct-TCPIP Reverse Proxy Tunnel
	s.mux.HandleFunc("/api/tunnel/", s.authMiddleware(s.handleTunnel))

	// Snapshots & GitHub Backup
	s.mux.HandleFunc("/api/snapshot/status", s.handleSnapshotStatus)
	s.mux.HandleFunc("/api/snapshot/bootstrap/start-fresh", s.handleSnapshotStartFresh)
	s.mux.HandleFunc("/api/snapshot/export", s.authMiddleware(s.handleSnapshotExport))
	s.mux.HandleFunc("/api/snapshot/import", s.handleSnapshotImport)
	s.mux.HandleFunc("/api/snapshot/github/sync", s.authMiddleware(s.handleSnapshotGitHubSync))
	s.mux.HandleFunc("/api/snapshot/github/restore", s.handleSnapshotGitHubRestore)

	// Self-Update & Release Management
	s.mux.HandleFunc("/api/update/check", s.authMiddleware(s.handleUpdateCheck))
	s.mux.HandleFunc("/api/update/apply", s.authMiddleware(s.handleUpdateApply))
	s.mux.HandleFunc("/api/update/restart", s.authMiddleware(s.handleUpdateRestart))

	// Live Event Stream (Activity Console)
	s.mux.HandleFunc("/api/logs/stream", s.authMiddleware(s.handleLogsStream))

	// Network Diagnostics (ADR Network Diagnostic & ADR-0035 Branch Triage Matrix)
	s.mux.HandleFunc("/api/netdiag/stream", s.authMiddleware(s.handleNetDiagStream))
	s.mux.HandleFunc("/api/netdiag/presets", s.authMiddleware(s.handleProbeTargetPresets))
	s.mux.HandleFunc("/api/netdiag/presets/", s.authMiddleware(s.handleProbeTargetPresetDetail))
	s.mux.HandleFunc("/api/netdiag/triage", s.authMiddleware(s.handleNetDiagTriageStream))
	s.mux.HandleFunc("/api/netdiag/triage/history", s.authMiddleware(s.handleTriageHistory))
	s.mux.HandleFunc("/api/netdiag/triage/history/", s.authMiddleware(s.handleTriageHistory))
	s.mux.HandleFunc("/api/netdiag/host-config/", s.authMiddleware(s.handleHostTriageConfig))
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

func (s *Server) issueSession(w http.ResponseWriter) string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		b = []byte(fmt.Sprintf("%d_%d", time.Now().UnixNano(), time.Now().Unix()))
	}
	token := hex.EncodeToString(b)
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
	return token
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
		logger.Warn("auth", "", "Admin login attempt failed (invalid password)")
		http.Error(w, `{"error":"invalid_password"}`, http.StatusUnauthorized)
		return
	}

	// Check if 2FA is active
	totpEnabled, _ := s.db.GetSetting("totp_enabled")
	if totpEnabled == "true" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		tempToken := fmt.Sprintf("2fa_%s", hex.EncodeToString(b))
		s.temp2FAChallenges.Store(tempToken, &temp2FAChallenge{
			expiry:   time.Now().Add(5 * time.Minute),
			attempts: 0,
		})
		logger.Info("auth", "", "Admin login credentials valid, awaiting 2FA challenge")
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":     "require_2fa",
			"temp_token": tempToken,
		})
		return
	}

	s.issueSession(w)
	logger.Info("auth", "", "Admin session established successfully")
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
}

func (s *Server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		TempToken string `json:"temp_token"`
		Code      string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	val, ok := s.temp2FAChallenges.Load(req.TempToken)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_or_expired_token"})
		return
	}
	ch := val.(*temp2FAChallenge)
	if time.Now().After(ch.expiry) {
		s.temp2FAChallenges.Delete(req.TempToken)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_or_expired_token"})
		return
	}

	if ch.attempts >= 5 {
		s.temp2FAChallenges.Delete(req.TempToken)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "too_many_attempts"})
		return
	}
	ch.attempts++

	cleanCode := strings.TrimSpace(req.Code)
	secret, _ := s.db.GetSetting("totp_secret")
	lastStepStr, _ := s.db.GetSetting("last_totp_step")
	var lastStep int64
	if lastStepStr != "" {
		lastStep, _ = strconv.ParseInt(lastStepStr, 10, 64)
	}

	valid, matchedStep := ValidateTOTP(secret, cleanCode, lastStep, time.Now())
	if valid {
		_ = s.db.SetSetting("last_totp_step", strconv.FormatInt(matchedStep, 10))
		s.temp2FAChallenges.Delete(req.TempToken)
		s.issueSession(w)
		logger.Info("auth", "", "Admin 2FA token verified successfully")
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
		return
	}

	// Check recovery codes
	hashesJSON, _ := s.db.GetSetting("totp_recovery_codes")
	var storedHashes []string
	if hashesJSON != "" {
		_ = json.Unmarshal([]byte(hashesJSON), &storedHashes)
	}
	recValid, remaining := ValidateRecoveryCode(cleanCode, storedHashes)
	if recValid {
		remBytes, _ := json.Marshal(remaining)
		_ = s.db.SetSetting("totp_recovery_codes", string(remBytes))
		s.temp2FAChallenges.Delete(req.TempToken)
		s.issueSession(w)
		logger.Warn("auth", "", "Admin 2FA recovery code used")
		writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok", "recovery_used": true})
		return
	}

	remainingAttempts := 5 - ch.attempts
	if remainingAttempts <= 0 {
		s.temp2FAChallenges.Delete(req.TempToken)
	}
	logger.Warn("auth", "", "Admin 2FA verification failed (invalid code)")
	writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
		"error":              "invalid_code",
		"remaining_attempts": remainingAttempts,
	})
}

func (s *Server) handle2FAStatus(w http.ResponseWriter, r *http.Request) {
	enabled, _ := s.db.GetSetting("totp_enabled")
	hashesJSON, _ := s.db.GetSetting("totp_recovery_codes")
	var storedHashes []string
	if hashesJSON != "" {
		_ = json.Unmarshal([]byte(hashesJSON), &storedHashes)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"enabled":              enabled == "true",
		"recovery_codes_count": len(storedHashes),
	})
}

func (s *Server) handle2FASetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, _ := r.Cookie("pantau_session")
	secret, err := GenerateTOTPSecret()
	if err != nil {
		http.Error(w, "failed to generate secret", http.StatusInternalServerError)
		return
	}
	plain, hashed, err := GenerateRecoveryCodes(8)
	if err != nil {
		http.Error(w, "failed to generate recovery codes", http.StatusInternalServerError)
		return
	}

	s.pending2FASetup.Store(cookie.Value, &pending2FASetupData{
		secret:        secret,
		recoveryCodes: plain,
		hashedCodes:   hashed,
		createdAt:     time.Now(),
	})

	otpauthURL := fmt.Sprintf("otpauth://totp/Pantau:admin?secret=%s&issuer=Pantau", secret)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"secret":         secret,
		"otpauth_url":    otpauthURL,
		"recovery_codes": plain,
	})
}

func (s *Server) handle2FAQR(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie("pantau_session")
	if cookie == nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	val, ok := s.pending2FASetup.Load(cookie.Value)
	if !ok {
		http.Error(w, "no pending 2fa setup", http.StatusBadRequest)
		return
	}
	data := val.(*pending2FASetupData)
	otpauthURL := fmt.Sprintf("otpauth://totp/Pantau:admin?secret=%s&issuer=Pantau", data.secret)
	png, err := GenerateQRCodePNG(otpauthURL)
	if err != nil {
		http.Error(w, "failed to generate qr code", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(png)
}

func (s *Server) handle2FAEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, _ := r.Cookie("pantau_session")
	if cookie == nil || cookie.Value == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	val, ok := s.pending2FASetup.Load(cookie.Value)
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no_pending_setup"})
		return
	}
	data := val.(*pending2FASetupData)

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	valid, _ := ValidateTOTP(data.secret, req.Code, 0, time.Now())
	if !valid {

		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_code"})
		return
	}

	hashedJSON, _ := json.Marshal(data.hashedCodes)
	_ = s.db.SetSetting("totp_enabled", "true")
	_ = s.db.SetSetting("totp_secret", data.secret)
	_ = s.db.SetSetting("totp_recovery_codes", string(hashedJSON))
	_ = s.db.SetSetting("last_totp_step", "0")
	s.pending2FASetup.Delete(cookie.Value)


	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "enabled"})
}

func (s *Server) handle2FADisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Verify password
	hash, err := s.db.GetSetting("admin_password_hash")
	if err != nil || hash == "" {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_password"})
		return
	}

	// Verify TOTP or recovery code
	cleanCode := strings.TrimSpace(req.Code)
	secret, _ := s.db.GetSetting("totp_secret")
	lastStepStr, _ := s.db.GetSetting("last_totp_step")
	var lastStep int64
	if lastStepStr != "" {
		lastStep, _ = strconv.ParseInt(lastStepStr, 10, 64)
	}

	valid, _ := ValidateTOTP(secret, cleanCode, lastStep, time.Now())
	if !valid {
		// Try recovery code
		hashesJSON, _ := s.db.GetSetting("totp_recovery_codes")
		var storedHashes []string
		if hashesJSON != "" {
			_ = json.Unmarshal([]byte(hashesJSON), &storedHashes)
		}
		recValid, _ := ValidateRecoveryCode(cleanCode, storedHashes)
		if !recValid {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_code"})
			return
		}
	}

	_ = s.db.SetSetting("totp_enabled", "false")
	_ = s.db.DeleteSetting("totp_secret")
	_ = s.db.DeleteSetting("totp_recovery_codes")
	_ = s.db.DeleteSetting("last_totp_step")

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "disabled"})
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
	_ = s.db.MarkBootstrapCompleted()
	writeJSON(w, http.StatusOK, map[string]string{"status": "password_updated"})
}

func (s *Server) resolveLang(r *http.Request) string {
	if l := r.URL.Query().Get("lang"); l != "" {
		if strings.HasPrefix(strings.ToLower(l), "id") {
			return "id"
		}
		return "en"
	}
	if l := r.Header.Get("X-Pantau-Lang"); l != "" {
		if strings.HasPrefix(strings.ToLower(l), "id") {
			return "id"
		}
		return "en"
	}
	if al := r.Header.Get("Accept-Language"); al != "" {
		if strings.HasPrefix(strings.ToLower(al), "id") {
			return "id"
		}
	}
	return "en"
}

func (s *Server) validateCommissionDate(commDate string, existingHost *store.Host) error {
	commDate = strings.TrimSpace(commDate)
	if commDate == "" {
		return nil
	}
	ct, ok := inspector.ParseCommissionDate(commDate)
	if !ok {
		return errors.New("invalid commission date format (use YYYY-MM-DD)")
	}
	if inspector.IsFutureDate(ct) {
		return errors.New("commission date cannot be in the future")
	}
	if existingHost != nil && !inspector.IsVirtualSystem(existingHost.HardwareModel) && existingHost.BIOSDate != "" {
		if bt, ok := inspector.ParseBIOSDate(existingHost.BIOSDate); ok {
			if ct.Before(bt) {
				return fmt.Errorf("commission date (%s) cannot be earlier than motherboard BIOS date (%s)", ct.Format("2006-01-02"), bt.Format("Jan 2006"))
			}
		}
	}
	return nil
}

func (s *Server) handleHosts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		hosts, err := s.db.ListHosts()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.inspector != nil {
			for i := range hosts {
				hosts[i].IsInspecting = s.inspector.IsInspecting(hosts[i].ID)
			}
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
		h.Host = strings.ToLower(strings.TrimSpace(h.Host))
		h.User = strings.TrimSpace(h.User)
		h.CommissionDate = strings.TrimSpace(h.CommissionDate)
		if h.Name == "" || h.Host == "" {
			http.Error(w, "name and host are required", http.StatusBadRequest)
			return
		}
		if err := s.validateCommissionDate(h.CommissionDate, nil); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if h.Port <= 0 {
			h.Port = 22
		}
		if h.User == "" {
			h.User = "root"
		}

		if dup, err := s.db.FindHostByTarget(h.Host, h.Port, 0); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		} else if dup != nil {
			targetStr := fmt.Sprintf("%s:%d", h.Host, h.Port)
			msgEN := fmt.Sprintf("Host with target '%s' is already registered as '%s'", targetStr, dup.Name)
			msgID := fmt.Sprintf("Host dengan target '%s' sudah terdaftar sebagai '%s'", targetStr, dup.Name)
			errVal := msgEN
			if s.resolveLang(r) == "id" {
				errVal = msgID
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"ok":       false,
				"code":     "err_duplicate_target",
				"target":   targetStr,
				"name":     dup.Name,
				"error":    errVal,
				"error_en": msgEN,
				"error_id": msgID,
			})
			return
		}

		// If one-time password provided, attempt key provisioning
		if strings.TrimSpace(req.Password) != "" {
			pubKey, err := s.db.GetSetting("ssh_public_key")
			if err != nil || pubKey == "" {
				http.Error(w, "global ssh public key not found", http.StatusInternalServerError)
				return
			}
			if err := s.provisioner(h.Host, h.Port, h.User, req.Password, pubKey); err != nil {
				logger.Error("ssh", h.Name, fmt.Sprintf("SSH key provisioning failed: %v", err))
				writeJSON(w, http.StatusBadRequest, map[string]interface{}{
					"error": fmt.Sprintf("Injeksi SSH Key gagal: %v", err),
				})
				return
			}
			logger.Info("ssh", h.Name, "SSH public key provisioned successfully")
		}

		id, err := s.db.CreateHost(&h)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		h.ID = id
		if s.inspector != nil {
			h.IsInspecting = true
			go func(newID int64) {
				_ = s.inspector.InspectHost(newID)
			}(id)
		}
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
	if parts[0] == "test-connection" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			HostID    int64  `json:"host_id"`
			Host      string `json:"host"`
			Port      int    `json:"port"`
			User      string `json:"user"`
			Password  string `json:"password"`
			CustomKey string `json:"custom_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		req.Host = strings.TrimSpace(req.Host)
		req.User = strings.TrimSpace(req.User)
		if req.Host == "" {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "host address is required"})
			return
		}
		if req.Port <= 0 {
			req.Port = 22
		}
		if req.User == "" {
			req.User = "root"
		}

		var runner sshrunner.Runner
		authMethod := "key"
		if strings.TrimSpace(req.Password) != "" {
			authMethod = "password"
			r, err := sshrunner.ConnectWithPassword(req.Host, req.Port, req.User, req.Password, 6*time.Second)
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error(), "auth_method": authMethod})
				return
			}
			runner = r
		} else {
			key := strings.TrimSpace(req.CustomKey)
			if key == "" && req.HostID > 0 {
				existing, _ := s.db.GetHost(req.HostID)
				if existing != nil && strings.TrimSpace(existing.CustomKey) != "" {
					key = strings.TrimSpace(existing.CustomKey)
				}
			}
			var err error
			if s.inspector != nil {
				runner, err = s.inspector.GetRunner(req.Host, req.Port, req.User, key)
			} else {
				if key == "" {
					key, _ = s.db.GetSetting("ssh_private_key")
				}
				runner, err = sshrunner.Connect(req.Host, req.Port, req.User, key, 6*time.Second)
			}
			if err != nil {
				writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error(), "auth_method": authMethod})
				return
			}
		}
		defer runner.Close()

		stdout, stderr, exitCode, err := runner.ExecTimeout("uname -srm; uptime", 5*time.Second)
		if err != nil || exitCode != 0 {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":          false,
				"error":       fmt.Sprintf("exit %d: %s %s", exitCode, stdout, stderr),
				"auth_method": authMethod,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":          true,
			"output":      strings.TrimSpace(stdout),
			"auth_method": authMethod,
		})
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

	if parts[0] == "inspect-all" {
		if r.Method == http.MethodGet {
			s.inspectAllMu.Lock()
			active := s.inspectAllActive || (s.inspector != nil && s.inspector.InFlightCount() > 0)
			elapsed := time.Since(s.lastInspectAll)
			remaining := 0
			if !s.lastInspectAll.IsZero() && elapsed < 15*time.Second {
				remaining = int(math.Ceil((15*time.Second - elapsed).Seconds()))
			}
			s.inspectAllMu.Unlock()
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"in_flight":     active,
				"cooldown":      remaining > 0,
				"remaining_sec": remaining,
			})
			return
		}

		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		s.inspectAllMu.Lock()
		if s.inspectAllActive || (s.inspector != nil && s.inspector.InFlightCount() > 0) {
			s.inspectAllMu.Unlock()
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"ok":        false,
				"in_flight": true,
				"error":     "Inspeksi untuk host sedang berjalan, harap tunggu hingga selesai.",
			})
			return
		}

		elapsed := time.Since(s.lastInspectAll)
		if !s.lastInspectAll.IsZero() && elapsed < 15*time.Second {
			remaining := int(math.Ceil((15*time.Second - elapsed).Seconds()))
			s.inspectAllMu.Unlock()
			writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
				"ok":            false,
				"cooldown":      true,
				"remaining_sec": remaining,
				"error":         fmt.Sprintf("Cooldown aktif. Harap tunggu %d detik sebelum melakukan inspeksi ulang.", remaining),
			})
			return
		}

		hosts, err := s.db.ListHosts()
		if err != nil {
			s.inspectAllMu.Unlock()
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if len(hosts) == 0 {
			s.inspectAllMu.Unlock()
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"ok":      true,
				"count":   0,
				"message": "Tidak ada host terdaftar",
			})
			return
		}

		s.inspectAllActive = true
		s.inspectAllMu.Unlock()

		// ponytail: Bounded worker pool (max 5 concurrent host inspections) prevents host flooding and CPU exhaustion.
		go func() {
			defer func() {
				s.inspectAllMu.Lock()
				s.inspectAllActive = false
				s.lastInspectAll = time.Now()
				s.inspectAllMu.Unlock()
			}()
			sem := make(chan struct{}, 5)
			var wg sync.WaitGroup
			for _, h := range hosts {
				wg.Add(1)
				sem <- struct{}{}
				go func(id int64) {
					defer func() {
						<-sem
						wg.Done()
					}()
					_ = s.inspector.InspectHost(id)
				}(h.ID)
			}
			wg.Wait()
		}()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok":      true,
			"count":   len(hosts),
			"message": fmt.Sprintf("Inspection started for %d hosts", len(hosts)),
		})
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
			if s.inspector != nil {
				host.IsInspecting = s.inspector.IsInspecting(host.ID)
			}
			rules, _ := s.db.ListDesiredRules(hostID)
			items, _ := s.db.ListActualItems(hostID)
			incidents, _ := s.db.ListIncidents(hostID, 20)
			runs, _ := s.db.ListInspectionRuns(hostID, 50)
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"host":         host,
				"rules":        rules,
				"actual_items": items,
				"incidents":    incidents,
				"runs":         runs,
			})
		case http.MethodPut:
			var h store.Host
			if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			h.ID = hostID
			h.Name = strings.TrimSpace(h.Name)
			h.Host = strings.ToLower(strings.TrimSpace(h.Host))
			h.User = strings.TrimSpace(h.User)
			h.CommissionDate = strings.TrimSpace(h.CommissionDate)
			if h.Port <= 0 {
				h.Port = 22
			}
			if h.User == "" {
				h.User = "root"
			}

			existing, _ := s.db.GetHost(hostID)
			if existing == nil {
				http.Error(w, "host not found", http.StatusNotFound)
				return
			}
			if h.Host == "" {
				h.Host = existing.Host
			}
			if h.Name == "" {
				h.Name = existing.Name
			}
			if err := s.validateCommissionDate(h.CommissionDate, existing); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			if dup, err := s.db.FindHostByTarget(h.Host, h.Port, hostID); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			} else if dup != nil {
				targetStr := fmt.Sprintf("%s:%d", h.Host, h.Port)
				msgEN := fmt.Sprintf("Host with target '%s' is already registered as '%s'", targetStr, dup.Name)
				msgID := fmt.Sprintf("Host dengan target '%s' sudah terdaftar sebagai '%s'", targetStr, dup.Name)
				errVal := msgEN
				if s.resolveLang(r) == "id" {
					errVal = msgID
				}
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"ok":       false,
					"code":     "err_duplicate_target",
					"target":   targetStr,
					"name":     dup.Name,
					"error":    errVal,
					"error_en": msgEN,
					"error_id": msgID,
				})
				return
			}

			if err := s.db.UpdateHost(&h); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if s.inspector != nil {
				_ = s.inspector.RecalculateHostLifecycle(hostID)
			}
			writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
		case http.MethodDelete:
			s.evictTunnelRunner(hostID)
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
	case "packages":
		if len(parts) >= 3 && parts[2] == "scan" {
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			// ponytail: Cooldown guard (60s) prevents aggressive SSH scanning stampedes.
			lastScanned, _ := s.db.GetHostPackagesLastScanned(hostID)
			if !lastScanned.IsZero() && time.Since(lastScanned) < 60*time.Second {
				rem := int(60 - time.Since(lastScanned).Seconds())
				if rem > 0 {
					writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
						"error":              fmt.Sprintf("Scan cooldown active. Please wait %d seconds.", rem),
						"cooldown_remaining": rem,
					})
					return
				}
			}

			if s.inspector == nil {
				http.Error(w, "inspector not available", http.StatusInternalServerError)
				return
			}

			if err := s.inspector.ScanPackages(hostID); err != nil {
				if errors.Is(err, inspector.ErrPackageScanInProgress) {
					writeJSON(w, http.StatusConflict, map[string]interface{}{"error": "Package scan already in progress for this host"})
					return
				}
				writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"error": err.Error()})
				return
			}
			pkgs, _ := s.db.GetHostPackages(hostID)
			if pkgs == nil {
				pkgs = []*store.HostPackage{}
			}
			w.Header().Set("X-Last-Scanned", time.Now().UTC().Format(time.RFC3339))
			w.Header().Set("X-Cooldown-Remaining", "60")
			writeJSON(w, http.StatusOK, pkgs)
			return
		}

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pkgs, err := s.db.GetHostPackages(hostID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if pkgs == nil {
			pkgs = []*store.HostPackage{}
		}
		lastScanned, _ := s.db.GetHostPackagesLastScanned(hostID)
		if !lastScanned.IsZero() {
			w.Header().Set("X-Last-Scanned", lastScanned.UTC().Format(time.RFC3339))
			elapsed := time.Since(lastScanned)
			if elapsed < 60*time.Second {
				rem := int(60 - elapsed.Seconds())
				if rem > 0 {
					w.Header().Set("X-Cooldown-Remaining", strconv.Itoa(rem))
				}
			}
		}
		writeJSON(w, http.StatusOK, pkgs)
		return

	case "commission-date":
		if r.Method != http.MethodPut && r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			CommissionDate string `json:"commission_date"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		req.CommissionDate = strings.TrimSpace(req.CommissionDate)
		host, err := s.db.GetHost(hostID)
		if err != nil || host == nil {
			http.Error(w, "host not found", http.StatusNotFound)
			return
		}
		if err := s.validateCommissionDate(req.CommissionDate, host); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		host.CommissionDate = req.CommissionDate
		if err := s.db.UpdateHost(host); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.inspector != nil {
			_ = s.inspector.RecalculateHostLifecycle(hostID)
		}
		updated, _ := s.db.GetHost(hostID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "host": updated})

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
			logger.Error("ssh", host.Name, fmt.Sprintf("SSH key provisioning failed: %v", err))
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": fmt.Sprintf("injeksi gagal: %v", err)})
			return
		}
		logger.Info("ssh", host.Name, "SSH public key provisioned successfully")

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
		host, err := s.db.GetHost(hostID)
		if err != nil || host == nil {
			http.Error(w, "host not found", http.StatusNotFound)
			return
		}

		// 1. In-flight guard: check if host is currently being inspected
		if s.inspector.IsInspecting(hostID) {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"ok":        false,
				"in_flight": true,
				"error":     "Inspeksi untuk host ini sedang berjalan, harap tunggu hingga selesai.",
			})
			return
		}

		// 2. Cooldown guard: 15 seconds per host
		if host.LastInspected != nil {
			elapsed := time.Since(*host.LastInspected)
			if elapsed < 15*time.Second {
				remaining := int(math.Ceil((15*time.Second - elapsed).Seconds()))
				writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"ok":            false,
					"cooldown":      true,
					"remaining_sec": remaining,
					"error":         fmt.Sprintf("Host baru saja diinspeksi %d detik lalu. Harap tunggu %d detik lagi.", int(elapsed.Seconds()), remaining),
				})
				return
			}
		}

		// Execute inspection
		err = s.inspector.InspectHost(hostID)
		if err != nil {
			if errors.Is(err, inspector.ErrAlreadyInspecting) {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"ok":        false,
					"in_flight": true,
					"error":     "Inspeksi untuk host ini sedang berjalan, harap tunggu hingga selesai.",
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		host, _ = s.db.GetHost(hostID)
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "host": host})
		return

	case "runs":
		// GET /api/hosts/{id}/runs?filter=issues&limit=100
		filter := r.URL.Query().Get("filter")
		limit := 100
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}
		runs, err := s.db.ListInspectionRuns(hostID, limit, filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if runs == nil {
			runs = []store.InspectionRun{}
		}
		writeJSON(w, http.StatusOK, runs)

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
			rData.Target = strings.TrimSpace(rData.Target)
			if rData.Target == "" || strings.ContainsAny(rData.Target, ";;&|`$\n\r") {
				http.Error(w, "invalid target: shell metacharacters not allowed", http.StatusBadRequest)
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
	action := subparts[0]
	if action == "pins" {
		switch r.Method {
		case http.MethodGet:
			pins, err := s.db.GetFilePins(hostID)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, pins)
			return
		case http.MethodPost:
			var req struct {
				Path  string `json:"path"`
				IsDir bool   `json:"is_dir"`
				Note  string `json:"note"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "invalid request body", http.StatusBadRequest)
				return
			}
			req.Path = strings.TrimSpace(req.Path)
			if req.Path == "" {
				http.Error(w, "path is required", http.StatusBadRequest)
				return
			}
			if err := s.db.SaveFilePin(hostID, req.Path, req.IsDir, req.Note); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "ok"})
			return
		case http.MethodDelete:
			filePath := r.URL.Query().Get("path")
			if filePath == "" {
				http.Error(w, "path query parameter required", http.StatusBadRequest)
				return
			}
			if err := s.db.DeleteFilePin(hostID, filePath); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusOK, map[string]interface{}{"status": "deleted"})
			return
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	}

	if s.inspector == nil {
		http.Error(w, "inspector not available", http.StatusInternalServerError)
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
			logger.Error("transfer", host.Name, fmt.Sprintf("File upload failed (%s): %v", destPath, err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		logger.Info("transfer", host.Name, fmt.Sprintf("File uploaded: %s", destPath))
		writeJSON(w, http.StatusOK, map[string]string{"status": "uploaded", "path": destPath})

	case "size":
		targetPath := r.URL.Query().Get("path")
		if targetPath == "" {
			http.Error(w, "path required", http.StatusBadRequest)
			return
		}
		out, _, _, _ := runner.Exec(fmt.Sprintf(`du -sb %s 2>/dev/null | cut -f1`, shellQuote(targetPath)))
		size, _ := strconv.ParseInt(strings.TrimSpace(out), 10, 64)
		outDf, _, _, _ := runner.Exec(fmt.Sprintf(`df -PB1 %s 2>/dev/null | tail -n 1 | awk '{print $4}'`, shellQuote(path.Dir(targetPath))))
		avail, _ := strconv.ParseInt(strings.TrimSpace(outDf), 10, 64)
		writeJSON(w, http.StatusOK, map[string]int64{
			"size":      size,
			"available": avail,
		})

	case "duplicate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Path    string `json:"path"`
			NewName string `json:"new_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		req.Path = strings.TrimSpace(req.Path)
		req.NewName = strings.TrimSpace(req.NewName)
		if req.Path == "" || req.NewName == "" {
			http.Error(w, "path and new_name required", http.StatusBadRequest)
			return
		}
		if strings.Contains(req.NewName, "/") || strings.Contains(req.NewName, "\\") || req.NewName == "." || req.NewName == ".." {
			http.Error(w, "invalid new_name", http.StatusBadRequest)
			return
		}
		parentDir := path.Dir(req.Path)
		destPath := path.Join(parentDir, req.NewName)

		if isProtectedPath(req.Path, sftpClient) || isProtectedPath(destPath, sftpClient) {
			http.Error(w, "protected path: cannot duplicate in system location", http.StatusForbidden)
			return
		}

		// Collision check: verify target file/folder does not exist
		if _, err := sftpClient.Stat(destPath); err == nil {
			http.Error(w, "file or folder already exists", http.StatusConflict)
			return
		}

		// Disk Space Safety Check:
		// Measure source size and available space on target partition (+ 100MB safety buffer)
		outSize, _, _, _ := runner.Exec(fmt.Sprintf(`du -sb %s 2>/dev/null | cut -f1`, shellQuote(req.Path)))
		sourceSize, _ := strconv.ParseInt(strings.TrimSpace(outSize), 10, 64)
		outDf, _, _, _ := runner.Exec(fmt.Sprintf(`df -PB1 %s 2>/dev/null | tail -n 1 | awk '{print $4}'`, shellQuote(parentDir)))
		availBytes, _ := strconv.ParseInt(strings.TrimSpace(outDf), 10, 64)

		const safetyBuffer = 100 * 1024 * 1024 // 100 MB buffer
		if availBytes > 0 && sourceSize > 0 && availBytes < (sourceSize+safetyBuffer) {
			http.Error(w, fmt.Sprintf("insufficient disk space: required %d bytes (+ 100MB buffer), available %d bytes", sourceSize, availBytes), http.StatusInsufficientStorage)
			return
		}

		// ponytail: Use native remote cp -a to perform instant atomic copy with preserved permissions
		cmd := fmt.Sprintf("cp -a -- %s %s", shellQuote(req.Path), shellQuote(destPath))
		_, stderr, exitCode, err := runner.Exec(cmd)
		if err != nil || exitCode != 0 {
			http.Error(w, fmt.Sprintf("duplication failed: %s", strings.TrimSpace(stderr)), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status": "duplicated",
			"path":   destPath,
		})

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

			// Ensure remote zip/tar is immediately aborted if client cancels download request
			doneChan := make(chan struct{})
			defer close(doneChan)
			go func() {
				select {
				case <-r.Context().Done():
					_ = runner.Close()
				case <-doneChan:
				}
			}()

			if hasZip {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+".zip"))
				w.Header().Set("Content-Type", "application/zip")
				cmd := fmt.Sprintf(`cd %s && zip -r -q - %s`, shellQuote(parent), shellQuote(base))
				_ = runner.PipeCommand(cmd, nil, w)
			} else {
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+".tar.gz"))
				w.Header().Set("Content-Type", "application/gzip")
				cmd := fmt.Sprintf(`tar -czf - -C %s %s`, shellQuote(parent), shellQuote(base))
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

func (s *Server) handleGlobalPins(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pins, err := s.db.GetAllFilePins()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, pins)
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

		autoTriageEnabled, _ := s.db.GetSetting("auto_triage_enabled")
		autoTriageInterval, _ := s.db.GetSetting("auto_triage_interval_min")
		if autoTriageInterval == "" {
			autoTriageInterval = "10"
		}
		autoTriageHost, _ := s.db.GetSetting("auto_triage_default_host_id")

		writeJSON(w, http.StatusOK, map[string]string{
			"ssh_public_key":              pubKey,
			"telegram_token":              tgToken,
			"telegram_chat_id":            tgChat,
			"webhook_url":                 webhook,
			"poll_interval_sec":           poll,
			"github_backup_enabled":       ghEnabled,
			"github_repo":                 ghRepo,
			"github_token":                ghToken,
			"github_branch":               ghBranch,
			"github_file_path":            ghPath,
			"snapshot_passphrase":         snapPass,
			"backup_interval_hours":       backupInterval,
			"last_backup_time":            lastBackupTime,
			"last_backup_status":          lastBackupStatus,
			"auto_triage_enabled":         autoTriageEnabled,
			"auto_triage_interval_min":    autoTriageInterval,
			"auto_triage_default_host_id": autoTriageHost,
		})
	case http.MethodPost:
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		for k, v := range req {
			if k != "ssh_public_key" && k != "ssh_private_key" && k != "admin_password_hash" {
				if k == "poll_interval_sec" {
					if sec, err := strconv.Atoi(v); err == nil {
						// ponytail: 0 disables background inspection, clamped to min 30s if between 1 and 29.
						if sec <= 0 {
							v = "0"
						} else if sec < 30 {
							v = "30"
						}
					}
				}
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
	if len(parts) >= 1 && (parts[0] == "ack-all" || parts[0] == "ack_all") && r.Method == http.MethodPost {
		_ = s.db.AcknowledgeAllAlerts()
		writeJSON(w, http.StatusOK, map[string]string{"status": "acknowledged_all"})
		return
	}
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

	presetIDStr := r.URL.Query().Get("preset_id")
	cmdParam := r.URL.Query().Get("cmd")
	dirParam := r.URL.Query().Get("dir")
	var initialCmd string
	var useTmux bool

	if presetIDStr != "" {
		if pid, err := strconv.ParseInt(presetIDStr, 10, 64); err == nil {
			if preset, _ := s.db.GetTerminalPreset(pid); preset != nil {
				initialCmd = preset.Command
				useTmux = preset.UseTmux
			}
		}
	} else if cmdParam != "" {
		initialCmd = cmdParam
	}

	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		logger.Error("terminal", host.Name, fmt.Sprintf("Interactive terminal SSH connect failed: %v", err))
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\nSSH connect failed: %v\r\n", err)))
		return
	}
	defer runner.Close()

	logger.Info("terminal", host.Name, fmt.Sprintf("Terminal session opened (Host %s)", host.Name))
	defer logger.Info("terminal", host.Name, fmt.Sprintf("Terminal session closed (Host %s)", host.Name))

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
				if err := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
					_ = inWriter.Close()
					_ = runner.Close()
					return
				}
			}
		}
	}()

	// Read WebSocket input to SSH
	go func() {
		for {
			msgType, data, err := ws.ReadMessage()
			if err != nil {
				_ = inWriter.Close()
				_ = runner.Close()
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

	// ponytail: Auto-provision /tmp/pantau-browser bridge script and export environment variables silently
	s.ensureBrowserBridge(runner, host.ID)

	// Handle initial working directory (Terminal From Path) and preset command execution
	if strings.TrimSpace(dirParam) != "" || strings.TrimSpace(initialCmd) != "" {
		go func() {
			time.Sleep(150 * time.Millisecond)
			if strings.TrimSpace(dirParam) != "" {
				escapedDir := strings.ReplaceAll(dirParam, `'`, `'\''`)
				_, _ = inWriter.Write([]byte(fmt.Sprintf("cd '%s'\n", escapedDir)))
				if strings.TrimSpace(initialCmd) != "" {
					time.Sleep(100 * time.Millisecond)
					execPayload := formatPresetExecution(initialCmd, useTmux)
					_, _ = inWriter.Write([]byte(execPayload + "\n"))
				}
			} else {
				execPayload := formatPresetExecution(initialCmd, useTmux)
				_, _ = inWriter.Write([]byte(execPayload + "\n"))
			}
		}()
	}

	_ = runner.Terminal(inReader, outWriter, 120, 40, resizeChan)
}

var (
	browserBridgeMu          sync.Mutex
	browserBridgeProvisioned = make(map[int64]bool)
)

func (s *Server) ensureBrowserBridge(runner sshrunner.Runner, hostID int64) {
	browserBridgeMu.Lock()
	if browserBridgeProvisioned[hostID] {
		browserBridgeMu.Unlock()
		return
	}
	browserBridgeProvisioned[hostID] = true
	browserBridgeMu.Unlock()

	cmd := `mkdir -p /tmp && cat << 'EOF' > /tmp/pantau-browser
#!/bin/sh
if [ -n "$1" ]; then
  printf '\033]1337;OpenURL=%s\007' "$1"
  printf '\033]777;open;%s\007' "$1"
fi
EOF
chmod +x /tmp/pantau-browser
for f in ~/.bashrc ~/.zshrc ~/.profile; do
  if [ -f "$f" ] && ! grep -q "pantau-browser" "$f" 2>/dev/null; then
    printf '\n# Pantau Remote Browser Bridge\nexport BROWSER=/tmp/pantau-browser GH_BROWSER=/tmp/pantau-browser\n' >> "$f"
  fi
done`
	_, _, _, _ = runner.ExecTimeout(cmd, 2*time.Second)
}

func formatPresetExecution(cmd string, useTmux bool) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	parts := strings.Fields(cmd)
	bin := parts[0]
	if (bin == "sudo" || bin == "doas") && len(parts) > 1 {
		bin = parts[1]
	}
	cleanBin := sanitizeSessionName(bin)

	precheck := fmt.Sprintf(`if command -v %s >/dev/null 2>&1; then %s; else echo -e "\r\n\033[1;33m[Pantau] Binary '%s' not found on this host.\033[0m\r\n"; fi`, cleanBin, cmd, cleanBin)

	if useTmux {
		sessName := fmt.Sprintf("pantau-%s", cleanBin)
		escapedCmd := strings.ReplaceAll(cmd, `"`, `\"`)
		return fmt.Sprintf(`if command -v tmux >/dev/null 2>&1; then tmux new-session -A -s %s "%s"; else %s; fi`, sessName, escapedCmd, precheck)
	}
	return precheck
}

func sanitizeSessionName(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			sb.WriteRune(r)
		}
	}
	if sb.Len() == 0 {
		return "preset"
	}
	return sb.String()
}

func (s *Server) handlePresets(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var presets []store.TerminalPreset
		var err error
		hostIDParam := r.URL.Query().Get("host_id")
		if hostIDParam == "all" {
			presets, err = s.db.ListAllTerminalPresets()
		} else if hostIDParam == "" || hostIDParam == "global" {
			presets, err = s.db.ListTerminalPresets(nil)
		} else {
			hid, parseErr := strconv.ParseInt(hostIDParam, 10, 64)
			if parseErr != nil {
				http.Error(w, "invalid host_id", http.StatusBadRequest)
				return
			}
			presets, err = s.db.ListTerminalPresets(&hid)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, presets)

	case http.MethodPost:
		var req struct {
			HostID    *int64 `json:"host_id"`
			Name      string `json:"name"`
			Command   string `json:"command"`
			SortOrder int    `json:"sort_order"`
			UseTmux   bool   `json:"use_tmux"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Command = strings.TrimSpace(req.Command)
		if req.Name == "" || req.Command == "" {
			http.Error(w, "name and command are required", http.StatusBadRequest)
			return
		}

		preset := &store.TerminalPreset{
			HostID:    req.HostID,
			Name:      req.Name,
			Command:   req.Command,
			SortOrder: req.SortOrder,
			UseTmux:   req.UseTmux,
		}
		id, err := s.db.CreateTerminalPreset(preset)
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

func (s *Server) handlePresetDetail(w http.ResponseWriter, r *http.Request) {
	subpath := strings.TrimPrefix(r.URL.Path, "/api/presets/")
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
		preset, err := s.db.GetTerminalPreset(id)
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
			HostID    *int64 `json:"host_id"`
			Name      string `json:"name"`
			Command   string `json:"command"`
			SortOrder int    `json:"sort_order"`
			UseTmux   bool   `json:"use_tmux"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.Command = strings.TrimSpace(req.Command)
		if req.Name == "" || req.Command == "" {
			http.Error(w, "name and command are required", http.StatusBadRequest)
			return
		}

		preset := &store.TerminalPreset{
			ID:        id,
			HostID:    req.HostID,
			Name:      req.Name,
			Command:   req.Command,
			SortOrder: req.SortOrder,
			UseTmux:   req.UseTmux,
		}
		if err := s.db.UpdateTerminalPreset(preset); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, preset)

	case http.MethodDelete:
		if err := s.db.DeleteTerminalPreset(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// WebSocket Docker Log Stream
func (s *Server) handleWSDockerLogs(w http.ResponseWriter, r *http.Request) {
	hostIDStr := r.URL.Query().Get("host_id")
	container := strings.TrimSpace(r.URL.Query().Get("container"))
	if !validContainerNameRegex.MatchString(container) {
		http.Error(w, "invalid container name", http.StatusBadRequest)
		return
	}
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
				if err := ws.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
					_ = outWriter.Close()
					_ = runner.Close()
					return
				}
			}
		}
	}()

	// Close on websocket close - immediately aborts remote docker logs -f
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				_ = outWriter.Close()
				_ = runner.Close()
				return
			}
		}
	}()

	cmd := fmt.Sprintf("docker logs -f --tail 100 %s", shellQuote(container))
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
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", s.htmlETag)
	// ponytail: strict CSP to guarantee 100% offline airgapped operation with no external CDN/font leaks
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; connect-src 'self' ws: wss:; font-src 'self' data:; img-src 'self' data:;")

	if match := r.Header.Get("If-None-Match"); match != "" && match == s.htmlETag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	// ponytail: serve pre-compressed gzip payload if supported (wire size ~56KB vs ~261KB, zero runtime CPU)
	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") && len(s.htmlContentGz) > 0 {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(s.htmlContentGz)
		return
	}

	_, _ = w.Write(s.htmlContent)
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := embeddedFS.ReadFile("static/favicon.ico")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/x-icon")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
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

// ponytail: read embedded markdown docs on demand with safe path mapping; upgrade to compressed blobs if docs exceed 5MB
func (s *Server) handleDocs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "readme"
	}
	lang := r.URL.Query().Get("lang")
	if lang != "id" {
		lang = "en"
	}

	var filePath string
	switch name {
	case "readme":
		if lang == "id" {
			filePath = "README.id.md"
		} else {
			filePath = "README.md"
		}
	case "guide":
		if lang == "id" {
			filePath = "docs/user-guide.id.md"
		} else {
			filePath = "docs/user-guide.md"
		}
	case "changelog":
		filePath = "CHANGELOG.md"
	default:
		http.Error(w, "Invalid document name", http.StatusBadRequest)
		return
	}

	var data []byte
	var err error
	if s.docsFS != nil {
		data, err = fs.ReadFile(s.docsFS, filePath)
	} else {
		data, err = os.ReadFile(filePath)
		if err != nil {
			data, err = os.ReadFile(filepath.Join("..", "..", filePath))
		}
	}

	if err != nil {
		http.Error(w, "Document not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if s.updaterMgr == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"available": false,
			"error":     "updater not initialized",
		})
		return
	}
	force := r.URL.Query().Get("force") == "true"
	res, err := s.updaterMgr.Check(r.Context(), force)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.updaterMgr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "updater not initialized"})
		return
	}
	if err := s.updaterMgr.ApplyUpdate(r.Context()); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Update downloaded, verified, and installed successfully.",
	})
}

func (s *Server) handleUpdateRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.updaterMgr == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "updater not initialized"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"message": "Restarting Pantau...",
	})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = s.updaterMgr.Restart()
	}()
}

