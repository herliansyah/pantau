package sshrunner

import (
	"encoding/base64"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestCleanWindowsRemotePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"/", "/"},
		{"~", "~"},
		{"~/", "~/"},
		{`C:\inetpub\wwwroot`, "C:/inetpub/wwwroot"},
		{`C:/inetpub/wwwroot`, "C:/inetpub/wwwroot"},
		{`/C:/inetpub/wwwroot`, "C:/inetpub/wwwroot"},
		{`//C:/inetpub/wwwroot`, "C:/inetpub/wwwroot"},
		{`///c/inetpub/wwwroot`, "c:/inetpub/wwwroot"},
		{`/c/inetpub/wwwroot`, "c:/inetpub/wwwroot"},
		{`/c`, "c:"},
		{`/c/`, "c:/"},
		{`/d/data/app`, "d:/data/app"},
		{`/D:/data/app`, "D:/data/app"},
		{`/etc/nginx/nginx.conf`, "/etc/nginx/nginx.conf"},
		{`/var/log`, "/var/log"},
	}

	for _, tc := range tests {
		got := CleanWindowsRemotePath(tc.input)
		if got != tc.expected {
			t.Errorf("CleanWindowsRemotePath(%q) = %q, expected %q", tc.input, got, tc.expected)
		}
	}
}

func TestPowershellEncodedCmd(t *testing.T) {
	rawScript := `$o = Get-CimInstance Win32_OperatingSystem; Write-Output $o.Caption`
	encoded := PowershellEncodedCmd(rawScript)

	prefix := "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand "
	if !strings.HasPrefix(encoded, prefix) {
		t.Fatalf("expected prefix %q, got: %s", prefix, encoded)
	}

	b64Part := strings.TrimPrefix(encoded, prefix)
	decodedBytes, err := base64.StdEncoding.DecodeString(b64Part)
	if err != nil {
		t.Fatalf("failed to decode base64 payload: %v", err)
	}

	u16 := make([]uint16, len(decodedBytes)/2)
	for i := 0; i < len(u16); i++ {
		u16[i] = uint16(decodedBytes[i*2]) | (uint16(decodedBytes[i*2+1]) << 8)
	}
	decodedStr := string(utf16.Decode(u16))

	if decodedStr != rawScript {
		t.Fatalf("decoded string does not match original script.\nGot:  %q\nWant: %q", decodedStr, rawScript)
	}
}
