package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"pantau/internal/store"
)

func setupTestServer(t *testing.T) (*Server, *store.DB, func()) {
	tmpDir, err := os.MkdirTemp("", "pantau-2fa-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Set initial admin password: "secretpassword"
	hash, err := bcrypt.GenerateFromPassword([]byte("secretpassword"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}
	_ = db.SetSetting("admin_password_hash", string(hash))

	srv := NewServer(db, nil, nil)

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}
	return srv, db, cleanup
}

func Test2FAFullLifecycle(t *testing.T) {
	srv, _, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Initial Login (2FA disabled)
	loginBody, _ := json.Marshal(map[string]string{"password": "secretpassword"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("initial login failed: %d, body: %s", w.Code, w.Body.String())
	}
	var loginResp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &loginResp)
	if loginResp["status"] != "ok" {
		t.Fatalf("expected status ok, got %v", loginResp)
	}

	cookies := w.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "pantau_session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatalf("expected pantau_session cookie")
	}

	// 2. Setup 2FA
	reqSetup := httptest.NewRequest(http.MethodPost, "/api/2fa/setup", nil)
	reqSetup.AddCookie(sessionCookie)
	wSetup := httptest.NewRecorder()
	srv.ServeHTTP(wSetup, reqSetup)

	if wSetup.Code != http.StatusOK {
		t.Fatalf("2fa setup failed: %d, body: %s", wSetup.Code, wSetup.Body.String())
	}
	var setupResp struct {
		Secret        string   `json:"secret"`
		OtpauthURL    string   `json:"otpauth_url"`
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.Unmarshal(wSetup.Body.Bytes(), &setupResp)
	if setupResp.Secret == "" || len(setupResp.RecoveryCodes) != 8 {
		t.Fatalf("invalid setup response: %+v", setupResp)
	}

	// 3. Test QR code endpoint
	reqQR := httptest.NewRequest(http.MethodGet, "/api/2fa/qr", nil)
	reqQR.AddCookie(sessionCookie)
	wQR := httptest.NewRecorder()
	srv.ServeHTTP(wQR, reqQR)
	if wQR.Code != http.StatusOK || !strings.Contains(wQR.Header().Get("Content-Type"), "image/png") {
		t.Fatalf("qr code endpoint failed: code=%d, type=%s", wQR.Code, wQR.Header().Get("Content-Type"))
	}

	// 4. Enable 2FA with current valid OTP
	otp, _ := GenerateOTP(setupResp.Secret, time.Now().Unix()/30)
	enableBody, _ := json.Marshal(map[string]string{"code": otp})
	reqEnable := httptest.NewRequest(http.MethodPost, "/api/2fa/enable", bytes.NewReader(enableBody))
	reqEnable.AddCookie(sessionCookie)
	wEnable := httptest.NewRecorder()
	srv.ServeHTTP(wEnable, reqEnable)

	if wEnable.Code != http.StatusOK {
		t.Fatalf("2fa enable failed: %d, body: %s", wEnable.Code, wEnable.Body.String())
	}

	// 5. Verify 2FA status
	reqStatus := httptest.NewRequest(http.MethodGet, "/api/2fa/status", nil)
	reqStatus.AddCookie(sessionCookie)
	wStatus := httptest.NewRecorder()
	srv.ServeHTTP(wStatus, reqStatus)
	var statusResp struct {
		Enabled            bool `json:"enabled"`
		RecoveryCodesCount int  `json:"recovery_codes_count"`
	}
	_ = json.Unmarshal(wStatus.Body.Bytes(), &statusResp)
	if !statusResp.Enabled || statusResp.RecoveryCodesCount != 8 {
		t.Fatalf("expected enabled=true, recovery=8, got %+v", statusResp)
	}

	// 6. Login now requires 2FA
	req2 := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, req2)
	var challengeResp struct {
		Status    string `json:"status"`
		TempToken string `json:"temp_token"`
	}
	_ = json.Unmarshal(w2.Body.Bytes(), &challengeResp)
	if challengeResp.Status != "require_2fa" || challengeResp.TempToken == "" {
		t.Fatalf("expected require_2fa, got %+v, body: %s", challengeResp, w2.Body.String())
	}


	// 7. Verify 2FA login with wrong code
	wrongBody, _ := json.Marshal(map[string]string{
		"temp_token": challengeResp.TempToken,
		"code":       "000000",
	})
	reqWrong := httptest.NewRequest(http.MethodPost, "/api/login/2fa", bytes.NewReader(wrongBody))
	wWrong := httptest.NewRecorder()
	srv.ServeHTTP(wWrong, reqWrong)
	if wWrong.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong code, got %d", wWrong.Code)
	}

	// 8. Verify 2FA login with valid OTP
	curStep := time.Now().Unix() / 30
	validOTP, _ := GenerateOTP(setupResp.Secret, curStep)
	validBody, _ := json.Marshal(map[string]string{
		"temp_token": challengeResp.TempToken,
		"code":       validOTP,
	})
	reqValid := httptest.NewRequest(http.MethodPost, "/api/login/2fa", bytes.NewReader(validBody))
	wValid := httptest.NewRecorder()
	srv.ServeHTTP(wValid, reqValid)
	if wValid.Code != http.StatusOK {
		t.Fatalf("valid 2FA login failed: %d, body: %s", wValid.Code, wValid.Body.String())
	}

	// 9. Anti-replay test: Attempt login again with same OTP step should fail
	reqReplayCh := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	wReplayChallenge := httptest.NewRecorder()
	srv.ServeHTTP(wReplayChallenge, reqReplayCh)
	var replayChallenge struct {
		TempToken string `json:"temp_token"`
	}
	_ = json.Unmarshal(wReplayChallenge.Body.Bytes(), &replayChallenge)

	replayBody, _ := json.Marshal(map[string]string{
		"temp_token": replayChallenge.TempToken,
		"code":       validOTP,
	})
	reqReplay := httptest.NewRequest(http.MethodPost, "/api/login/2fa", bytes.NewReader(replayBody))
	wReplay := httptest.NewRecorder()
	srv.ServeHTTP(wReplay, reqReplay)
	if wReplay.Code != http.StatusUnauthorized {
		t.Fatalf("expected replay of same OTP to fail with 401, got %d", wReplay.Code)
	}

	// 10. Login using Recovery Code (need fresh challenge)
	reqRecCh := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	wRecChallenge := httptest.NewRecorder()
	srv.ServeHTTP(wRecChallenge, reqRecCh)
	var recChallenge struct {
		TempToken string `json:"temp_token"`
	}
	_ = json.Unmarshal(wRecChallenge.Body.Bytes(), &recChallenge)

	recBody, _ := json.Marshal(map[string]string{
		"temp_token": recChallenge.TempToken,
		"code":       setupResp.RecoveryCodes[0],
	})
	reqRec := httptest.NewRequest(http.MethodPost, "/api/login/2fa", bytes.NewReader(recBody))
	wRec := httptest.NewRecorder()
	srv.ServeHTTP(wRec, reqRec)
	if wRec.Code != http.StatusOK {
		t.Fatalf("login with recovery code failed: %d, body: %s", wRec.Code, wRec.Body.String())
	}

	// 11. Disable 2FA with password and valid OTP
	// Generate OTP for next step or current if time advanced
	curStep2 := (time.Now().Unix() / 30) + 1
	validOTP2, _ := GenerateOTP(setupResp.Secret, curStep2)
	disBody, _ := json.Marshal(map[string]string{
		"password": "secretpassword",
		"code":     validOTP2,
	})
	reqDis := httptest.NewRequest(http.MethodPost, "/api/2fa/disable", bytes.NewReader(disBody))
	reqDis.AddCookie(sessionCookie)
	wDis := httptest.NewRecorder()
	srv.ServeHTTP(wDis, reqDis)
	if wDis.Code != http.StatusOK {
		t.Fatalf("disable 2fa failed: %d, body: %s", wDis.Code, wDis.Body.String())
	}

	// 12. Login directly with password (2FA now disabled)
	reqFinal := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	wFinal := httptest.NewRecorder()
	srv.ServeHTTP(wFinal, reqFinal)
	var finalResp map[string]interface{}
	_ = json.Unmarshal(wFinal.Body.Bytes(), &finalResp)
	if finalResp["status"] != "ok" {
		t.Fatalf("expected direct login after 2FA disabled, got %+v", finalResp)
	}
}

