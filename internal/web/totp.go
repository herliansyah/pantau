package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

// GenerateTOTPSecret creates a 20-byte random base32 encoded secret.
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}

// GenerateOTP produces a 6-digit TOTP code for the given secret and time step (RFC 6238).
func GenerateOTP(secret string, timeStep int64) (string, error) {
	// Normalize secret
	cleanSecret := strings.ToUpper(strings.TrimSpace(secret))
	// Support unpadded or padded base32
	var key []byte
	var err error
	if strings.Contains(cleanSecret, "=") {
		key, err = base32.StdEncoding.DecodeString(cleanSecret)
	} else {
		key, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(cleanSecret)
	}
	if err != nil {
		return "", fmt.Errorf("invalid base32 secret: %w", err)
	}

	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(timeStep))

	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	binaryCode := (int64(h[offset]&0x7f) << 24) |
		(int64(h[offset+1]&0xff) << 16) |
		(int64(h[offset+2]&0xff) << 8) |
		(int64(h[offset+3] & 0xff))

	otp := binaryCode % 1000000
	return fmt.Sprintf("%06d", otp), nil
}

// ValidateTOTP validates a 6-digit TOTP code against the secret within a ±1 step window (RFC 6238).
// It returns true and the matched time step if valid and not a replay of a previously used step.
func ValidateTOTP(secret, code string, lastStep int64, now time.Time) (bool, int64) {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false, 0
	}

	currentStep := now.Unix() / 30
	// ponytail: scan ±1 step window (90 seconds). Sufficient for standard mobile clock drift.
	for step := currentStep - 1; step <= currentStep+1; step++ {
		if step <= lastStep {
			// Anti-replay: skip steps that were already consumed
			continue
		}
		expected, err := GenerateOTP(secret, step)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 {
			return true, step
		}
	}
	return false, 0
}

// GenerateRecoveryCodes produces count plain recovery codes and their SHA-256 hex hashes.
func GenerateRecoveryCodes(count int) (plain []string, hashed []string, err error) {
	plain = make([]string, count)
	hashed = make([]string, count)

	for i := 0; i < count; i++ {
		b := make([]byte, 5)
		if _, err := rand.Read(b); err != nil {
			return nil, nil, err
		}
		code := fmt.Sprintf("%02X%02X-%02X%02X", b[0], b[1], b[2], b[3])
		h := HashRecoveryCode(code)
		plain[i] = code
		hashed[i] = h
	}
	return plain, hashed, nil
}

// HashRecoveryCode computes the SHA-256 hex digest of a normalized recovery code.
func HashRecoveryCode(code string) string {
	clean := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	sum := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(sum[:])
}

// ValidateRecoveryCode checks if code matches any hashed recovery code and returns remaining hashes with the matched one removed.
func ValidateRecoveryCode(code string, storedHashes []string) (bool, []string) {
	h := HashRecoveryCode(code)
	matchedIdx := -1
	for i, sh := range storedHashes {
		if subtle.ConstantTimeCompare([]byte(sh), []byte(h)) == 1 {
			matchedIdx = i
			break
		}
	}
	if matchedIdx == -1 {
		return false, storedHashes
	}

	remaining := make([]string, 0, len(storedHashes)-1)
	for i, sh := range storedHashes {
		if i != matchedIdx {
			remaining = append(remaining, sh)
		}
	}
	return true, remaining
}

// GenerateQRCodePNG generates a self-contained 256x256 PNG QR code for the given URI.
func GenerateQRCodePNG(content string) ([]byte, error) {
	return qrcode.Encode(content, qrcode.Medium, 256)
}
