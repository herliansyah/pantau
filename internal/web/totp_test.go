package web

import (
	"testing"
	"time"
)

func TestTOTPFlow(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret failed: %v", err)
	}

	now := time.Now()
	currentStep := now.Unix() / 30

	otp, err := GenerateOTP(secret, currentStep)
	if err != nil {
		t.Fatalf("GenerateOTP failed: %v", err)
	}
	if len(otp) != 6 {
		t.Fatalf("expected 6-digit OTP, got %s", otp)
	}

	// Validate valid OTP
	valid, matchedStep := ValidateTOTP(secret, otp, 0, now)
	if !valid || matchedStep != currentStep {
		t.Fatalf("ValidateTOTP failed: valid=%v, matchedStep=%d, currentStep=%d", valid, matchedStep, currentStep)
	}

	// Anti-replay check: same step should now fail
	validAgain, _ := ValidateTOTP(secret, otp, matchedStep, now)
	if validAgain {
		t.Fatalf("expected replay of same step to fail, but it passed")
	}

	// Validate bad OTP
	badValid, _ := ValidateTOTP(secret, "000000", 0, now)
	if badValid && otp != "000000" {
		t.Fatalf("bad OTP should not validate")
	}

	// Recovery code check
	plainCodes, hashedCodes, err := GenerateRecoveryCodes(8)
	if err != nil || len(plainCodes) != 8 {
		t.Fatalf("GenerateRecoveryCodes failed: %v", err)
	}

	recValid, remaining := ValidateRecoveryCode(plainCodes[0], hashedCodes)
	if !recValid || len(remaining) != 7 {
		t.Fatalf("ValidateRecoveryCode failed for valid code: valid=%v, remaining=%d", recValid, len(remaining))
	}

	// Re-using the consumed code should fail
	recValid2, remaining2 := ValidateRecoveryCode(plainCodes[0], remaining)
	if recValid2 || len(remaining2) != 7 {
		t.Fatalf("ValidateRecoveryCode should fail on consumed code: valid=%v", recValid2)
	}

	// QR Code generation
	pngBytes, err := GenerateQRCodePNG("otpauth://totp/Pantau:admin?secret=" + secret + "&issuer=Pantau")
	if err != nil || len(pngBytes) == 0 {
		t.Fatalf("GenerateQRCodePNG failed: %v", err)
	}
}
