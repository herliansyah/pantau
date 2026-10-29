package web

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"pantau/internal/logger"
	"pantau/internal/sshrunner"
	"pantau/internal/store"

	"golang.org/x/crypto/ssh"
)

var (
	validKeyLineRegex = regexp.MustCompile(`^(ssh-ed25519|ssh-rsa) [A-Za-z0-9+/=]+ pantau-ephemeral-[0-9a-fA-F]+-[0-9]+$`)
	safeHostRegex     = regexp.MustCompile(`^[a-zA-Z0-9.:\-_\[\]]+$`)
	safeUserRegex     = regexp.MustCompile(`^[a-zA-Z0-9.\-_]+$`)
)

// EphemeralKey holds an in-memory short-lived SSH keypair.
type EphemeralKey struct {
	ID         string    `json:"id"`
	HostID     int64     `json:"host_id"`
	PrivateKey []byte    `json:"-"`
	PublicKey  string    `json:"public_key"`
	Expiry     time.Time `json:"expiry"`
}

// NativeTicket represents an authenticated, single-use ticket for downloading or streaming
// client-side launcher scripts in Remote Web mode.
type NativeTicket struct {
	Token        string        `json:"token"`
	HostID       int64         `json:"host_id"`
	HostName     string        `json:"host_name"`
	HostAddress  string        `json:"host_address"`
	HostPort     int           `json:"host_port"`
	HostUser     string        `json:"host_user"`
	InitialDir   string        `json:"initial_dir,omitempty"`
	EphemeralKey *EphemeralKey `json:"-"`
	ExpiresAt    time.Time     `json:"expires_at"`
	CreatedIP    string        `json:"created_ip"`
}

// cleanInitialDir strips control characters and newlines from an initial directory path.
func cleanInitialDir(dir string) string {
	dir = strings.TrimSpace(dir)
	dir = strings.ReplaceAll(dir, "\n", "")
	dir = strings.ReplaceAll(dir, "\r", "")
	dir = strings.ReplaceAll(dir, "\x00", "")
	return dir
}

// buildRemoteCdClause constructs the SSH remote command clause to navigate to initialDir.
// It supports absolute paths, ~, and ~/subpath with safe single quoting without breaking tilde expansion.
// ponytail: uses semicolon-free shell chaining ('|| true &&') so arguments do not trigger command
// splitting on Windows Terminal (wt.exe), PowerShell, or POSIX shells across all client operating systems.
func buildRemoteCdClause(initialDir string) string {
	cleaned := cleanInitialDir(initialDir)
	if cleaned == "" {
		return ""
	}
	if cleaned == "~" {
		return `cd ~ 2>/dev/null || true && exec bash -l || exec sh -l`
	}
	if strings.HasPrefix(cleaned, "~/") {
		sub := strings.TrimPrefix(cleaned, "~/")
		escapedSub := strings.ReplaceAll(sub, "'", `'\''`)
		return fmt.Sprintf(`cd ~/'%s' 2>/dev/null || true && exec bash -l || exec sh -l`, escapedSub)
	}
	escapedDir := strings.ReplaceAll(cleaned, "'", `'\''`)
	return fmt.Sprintf(`cd '%s' 2>/dev/null || true && exec bash -l || exec sh -l`, escapedDir)
}

// GenerateEphemeralKey generates an RSA 2048-bit keypair in RAM for universal compatibility
// with both modern Linux (OpenSSH 8.x/9.x) and legacy Linux hosts (OpenSSH 5.x/6.x per ADR-0005).
func GenerateEphemeralKey(hostID int64, ttl time.Duration) (*EphemeralKey, error) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generate rsa key: %w", err)
	}

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, fmt.Errorf("generate random id: %w", err)
	}
	id := fmt.Sprintf("%x", idBytes)

	expiry := time.Now().Add(ttl)
	comment := fmt.Sprintf("pantau-ephemeral-%s-%d", id, expiry.Unix())

	sshPub, err := ssh.NewPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("convert ssh public key: %w", err)
	}
	pubBytes := ssh.MarshalAuthorizedKey(sshPub)
	pubLine := fmt.Sprintf("%s %s", strings.TrimSpace(string(pubBytes)), comment)

	if !validKeyLineRegex.MatchString(pubLine) {
		return nil, fmt.Errorf("ephemeral public key failed invariant regex check: %s", pubLine)
	}

	privBlock, err := ssh.MarshalPrivateKey(priv, comment)
	if err != nil {
		return nil, fmt.Errorf("marshal openssh private key: %w", err)
	}
	privPEM := pem.EncodeToMemory(privBlock)

	return &EphemeralKey{
		ID:         id,
		HostID:     hostID,
		PrivateKey: privPEM,
		PublicKey:  pubLine,
		Expiry:     expiry,
	}, nil
}

// getHostKeyMutex returns or creates a per-host mutex to guard concurrent authorized_keys operations.
func (s *Server) getHostKeyMutex(hostID int64) *sync.Mutex {
	val, _ := s.hostKeyMutexes.LoadOrStore(hostID, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// storeNativeTicket stores a single-use ticket in memory with 60s TTL.
func (s *Server) storeNativeTicket(ticket *NativeTicket) {
	s.nativeTickets.Store(ticket.Token, ticket)
	time.AfterFunc(65*time.Second, func() {
		s.nativeTickets.Delete(ticket.Token)
	})
}

// getAndBurnNativeTicket atomically retrieves and deletes a ticket (Single-Use / Burn-After-Read).
func (s *Server) getAndBurnNativeTicket(token string) (*NativeTicket, bool) {
	if token == "" {
		return nil, false
	}
	val, loaded := s.nativeTickets.LoadAndDelete(token)
	if !loaded || val == nil {
		return nil, false
	}
	ticket, ok := val.(*NativeTicket)
	if !ok || ticket == nil {
		return nil, false
	}
	if time.Now().After(ticket.ExpiresAt) {
		return nil, false
	}
	return ticket, true
}

// injectEphemeralKey safely appends the ephemeral public key to target host's authorized_keys.
func (s *Server) injectEphemeralKey(ctx context.Context, host *store.Host, key *EphemeralKey) error {
	mu := s.getHostKeyMutex(host.ID)
	mu.Lock()
	defer mu.Unlock()

	if s.inspector == nil {
		return fmt.Errorf("inspector not available")
	}
	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		return fmt.Errorf("connect to host %s: %w", host.Name, err)
	}
	defer runner.Close()

	// 1. Opportunistic sweep before injection to clean old stale keys
	_ = s.sweepExpiredKeysOnHost(ctx, runner)

	// 2. Safe atomic append with directory & file permission hardening and trailing newline check
	cleanKey := strings.TrimSpace(key.PublicKey)
	injectCmd := fmt.Sprintf(
		`mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && ( [ -s ~/.ssh/authorized_keys ] && [ -n "$(tail -c1 ~/.ssh/authorized_keys 2>/dev/null)" ] && echo "" >> ~/.ssh/authorized_keys ; true ) && echo %q >> ~/.ssh/authorized_keys`,
		cleanKey,
	)

	_, stderr, exitCode, err := runner.ExecTimeout(injectCmd, 15*time.Second)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("inject key failed (exit %d): %s %v", exitCode, stderr, err)
	}

	// 3. Schedule Proactive Go Timer cleanup (Tier 1)
	ttl := time.Until(key.Expiry)
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	time.AfterFunc(ttl, func() {
		_ = s.cleanupEphemeralKeyByID(context.Background(), host, key.ID)
	})

	return nil
}

// cleanupEphemeralKeyByID prunes the specific key ID from authorized_keys using atomic AWK filter.
func (s *Server) cleanupEphemeralKeyByID(ctx context.Context, host *store.Host, keyID string) error {
	if host == nil {
		return nil
	}
	mu := s.getHostKeyMutex(host.ID)
	mu.Lock()
	defer mu.Unlock()

	if s.db != nil {
		if freshHost, err := s.db.GetHost(host.ID); err == nil && freshHost != nil {
			host = freshHost
		}
	}

	if s.inspector == nil {
		return fmt.Errorf("inspector not available")
	}
	runner, err := s.inspector.GetRunnerForHost(host)
	if err != nil {
		return err
	}
	defer runner.Close()

	cleanID := strings.TrimSpace(keyID)
	if cleanID == "" {
		return nil
	}

	cleanupCmd := fmt.Sprintf(
		`ak="$HOME/.ssh/authorized_keys"; tmp="$HOME/.ssh/authorized_keys.tmp.$$"; if [ -f "$ak" ]; then awk -v id="pantau-ephemeral-%s-" '$0 !~ id { print $0 }' "$ak" > "$tmp" && chmod 600 "$tmp" && mv -f "$tmp" "$ak"; rm -f "$tmp" 2>/dev/null || true; fi`,
		cleanID,
	)

	_, _, _, err = runner.ExecTimeout(cleanupCmd, 15*time.Second)
	return err
}

// sweepExpiredKeysOnHost prunes keys whose epoch timestamp is older than (now - 600s).
func (s *Server) sweepExpiredKeysOnHost(ctx context.Context, runner sshrunner.Runner) error {
	sweepCmd := `ak="$HOME/.ssh/authorized_keys"
tmp="$HOME/.ssh/authorized_keys.tmp.$$"
now=$(date +%s 2>/dev/null || echo 0)
cutoff=$((now - 600))
if [ -f "$ak" ]; then
    awk -v cutoff="$cutoff" '{
        is_ephemeral = 0;
        for (i = 1; i <= NF; i++) {
            if ($i ~ /^pantau-ephemeral-[0-9a-fA-F]+-[0-9]+$/) {
                is_ephemeral = 1;
                tag = $i;
                sub(/^.*-/, "", tag);
                ts = tag + 0;
                if (ts > cutoff) {
                    print $0;
                }
                break;
            }
        }
        if (!is_ephemeral) {
            print $0;
        }
    }' "$ak" > "$tmp" && chmod 600 "$tmp" && mv -f "$tmp" "$ak"
    rm -f "$tmp" 2>/dev/null || true
fi`

	_, _, _, err := runner.ExecTimeout(sweepCmd, 15*time.Second)
	return err
}

// validateTerminalTemplate ensures a custom template is safe and valid.
func validateTerminalTemplate(template string) error {
	trimmed := strings.TrimSpace(template)
	if trimmed == "" {
		return fmt.Errorf("template cannot be empty")
	}
	if !strings.Contains(trimmed, "%cmd%") {
		return fmt.Errorf("template must contain %%cmd%% placeholder")
	}
	// Check for dangerous shell metacharacters outside %cmd%
	dangerous := []string{";", "&&", "||", "`", "$(", "|", "\n", "\r"}
	for _, d := range dangerous {
		if strings.Contains(trimmed, d) {
			return fmt.Errorf("template contains disallowed metacharacter: %s", d)
		}
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return fmt.Errorf("invalid template")
	}
	bin := fields[0]
	if _, err := exec.LookPath(bin); err != nil {
		return fmt.Errorf("executable '%s' not found on PATH: %w", bin, err)
	}
	return nil
}

// detectDefaultTerminal detects the best available terminal emulator on the server host.
func detectDefaultTerminal(goos string) (termType, termPath string) {
	switch goos {
	case "windows":
		if path, err := exec.LookPath("wt.exe"); err == nil {
			return "windows-terminal", path
		}
		if path, err := exec.LookPath("pwsh.exe"); err == nil {
			return "powershell-core", path
		}
		if path, err := exec.LookPath("powershell.exe"); err == nil {
			return "powershell", path
		}
		if comspec := os.Getenv("COMSPEC"); comspec != "" {
			return "cmd", comspec
		}
		if path, err := exec.LookPath("cmd.exe"); err == nil {
			return "cmd", path
		}
		return "cmd", "cmd.exe"

	case "darwin":
		itermPaths := []string{
			"/Applications/iTerm.app",
			filepath.Join(os.Getenv("HOME"), "Applications/iTerm.app"),
		}
		for _, p := range itermPaths {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				return "iterm2", p
			}
		}
		appleTerminalPaths := []string{
			"/System/Applications/Utilities/Terminal.app",
			"/Applications/Utilities/Terminal.app",
		}
		for _, p := range appleTerminalPaths {
			if fi, err := os.Stat(p); err == nil && fi.IsDir() {
				return "apple-terminal", p
			}
		}
		return "apple-terminal", "Terminal"

	default: // linux & others
		if term := os.Getenv("TERMINAL"); term != "" {
			if path, err := exec.LookPath(term); err == nil {
				return term, path
			}
		}
		if path, err := exec.LookPath("x-terminal-emulator"); err == nil {
			return "x-terminal-emulator", path
		}
		popular := []string{"gnome-terminal", "konsole", "xfce4-terminal", "alacritty", "kitty", "wezterm", "terminator", "tilix", "foot", "xterm"}
		for _, name := range popular {
			if path, err := exec.LookPath(name); err == nil {
				return name, path
			}
		}
		return "", ""
	}
}

// buildLaunchCommand constructs the exec.Cmd for native terminal execution.
func buildLaunchCommand(goos, termPath, customTemplate, keyPath, user, host string, port int, initialDir string) (*exec.Cmd, error) {
	sshBase := fmt.Sprintf(`ssh -i "%s" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s`, keyPath, port, user, host)
	cdClause := buildRemoteCdClause(initialDir)
	if cdClause != "" {
		sshBase = fmt.Sprintf(`%s -t "%s"`, sshBase, cdClause)
	}

	if customTemplate != "" {
		// Custom template mode: execute via system shell to properly preserve quotes and arguments
		cmdStr := strings.ReplaceAll(customTemplate, "%cmd%", sshBase)
		var cmd *exec.Cmd
		if goos == "windows" {
			cmd = exec.Command("cmd.exe", "/c", cmdStr)
		} else {
			cmd = exec.Command("sh", "-c", "exec "+cmdStr)
		}
		setDetachedProcess(cmd)
		return cmd, nil
	}

	switch goos {
	case "windows":
		if strings.Contains(strings.ToLower(termPath), "wt") {
			// Windows Terminal uses ';' as a subcommand delimiter unless escaped as '\;'
			escapedSSH := strings.ReplaceAll(sshBase, ";", `\;`)
			cmd := exec.Command(termPath, "new-tab", "cmd.exe", "/k", escapedSSH)
			setDetachedProcess(cmd)
			return cmd, nil
		}
		if strings.Contains(strings.ToLower(termPath), "pwsh") || strings.Contains(strings.ToLower(termPath), "powershell") {
			cmd := exec.Command(termPath, "-NoExit", "-Command", sshBase)
			setDetachedProcess(cmd)
			return cmd, nil
		}
		// CMD fallback
		cmd := exec.Command("cmd.exe", "/c", "start", fmt.Sprintf("Pantau - %s", host), "cmd.exe", "/k", sshBase)
		setDetachedProcess(cmd)
		return cmd, nil

	case "darwin":
		// AppleScript execution (escape double quotes inside AppleScript string literal)
		escapedSSH := strings.ReplaceAll(sshBase, `"`, `\"`)
		script := fmt.Sprintf("tell application \"Terminal\" to do script \"%s; exec $SHELL\"\ntell application \"Terminal\" to activate", escapedSSH)
		cmd := exec.Command("osascript", "-e", script)
		setDetachedProcess(cmd)
		return cmd, nil

	default: // Linux
		baseName := filepath.Base(termPath)
		var cmd *exec.Cmd
		switch baseName {
		case "gnome-terminal":
			cmd = exec.Command(termPath, "--", "bash", "-c", fmt.Sprintf("%s; exec bash", sshBase))
		case "konsole", "alacritty", "xfce4-terminal", "x-terminal-emulator":
			cmd = exec.Command(termPath, "-e", "bash", "-c", fmt.Sprintf("%s; exec bash", sshBase))
		case "kitty":
			cmd = exec.Command(termPath, "sh", "-c", fmt.Sprintf("%s; exec $SHELL", sshBase))
		default:
			cmd = exec.Command(termPath, "-e", "bash", "-c", fmt.Sprintf("%s; exec bash", sshBase))
		}
		setDetachedProcess(cmd)
		return cmd, nil
	}
}

// launchLocalTerminal handles launching native terminal when Pantau runs locally.
func (s *Server) launchLocalTerminal(host *store.Host, customTemplate string, initialDir string) error {
	// 1. Generate in-memory key
	key, err := GenerateEphemeralKey(host.ID, 5*time.Minute)
	if err != nil {
		return fmt.Errorf("generate key: %w", err)
	}

	// 2. Inject to host
	if err := s.injectEphemeralKey(context.Background(), host, key); err != nil {
		return fmt.Errorf("inject key: %w", err)
	}

	// 3. Write temporary private key to secure local file
	var keyFile string
	if runtime.GOOS == "windows" {
		tempDir := os.TempDir()
		keyFile = filepath.Join(tempDir, fmt.Sprintf("pantau_%s.key", key.ID))
		if err := os.WriteFile(keyFile, key.PrivateKey, 0600); err != nil {
			return fmt.Errorf("write local key: %w", err)
		}
		// Windows: lock down ACL with icacls so OpenSSH doesn't reject as unprotected
		user := os.Getenv("USERNAME")
		if user != "" {
			_ = exec.Command("icacls", keyFile, "/inheritance:r", "/grant:r", fmt.Sprintf("%s:(R)", user)).Run()
		}
	} else {
		f, err := os.CreateTemp("", fmt.Sprintf("pantau_%s_*.key", key.ID))
		if err != nil {
			return fmt.Errorf("create temp key file: %w", err)
		}
		keyFile = f.Name()
		_ = f.Chmod(0600)
		_, _ = f.Write(key.PrivateKey)
		_ = f.Close()
	}

	// 4. Detect terminal & build launch command
	_, termPath := detectDefaultTerminal(runtime.GOOS)
	if customTemplate == "" && termPath == "" {
		_ = os.Remove(keyFile)
		_ = s.cleanupEphemeralKeyByID(context.Background(), host, key.ID)
		return fmt.Errorf("no supported terminal emulator found in PATH (install gnome-terminal, alacritty, or configure custom template)")
	}
	port := host.Port
	if port <= 0 {
		port = 22
	}
	cmd, err := buildLaunchCommand(runtime.GOOS, termPath, customTemplate, keyFile, host.User, host.Host, port, initialDir)
	if err != nil {
		_ = os.Remove(keyFile)
		_ = s.cleanupEphemeralKeyByID(context.Background(), host, key.ID)
		return fmt.Errorf("build launch command: %w", err)
	}

	// 5. Start detached process
	if err := cmd.Start(); err != nil {
		_ = os.Remove(keyFile)
		_ = s.cleanupEphemeralKeyByID(context.Background(), host, key.ID)
		return fmt.Errorf("start terminal process: %w", err)
	}

	// 6. Schedule local temp key cleanup after 30 seconds
	time.AfterFunc(30*time.Second, func() {
		_ = os.Remove(keyFile)
	})

	return nil
}

// sanitizeDisplayName filters characters to safely display in terminal scripts without shell injection.
func sanitizeDisplayName(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' || r == ' ' {
			sb.WriteRune(r)
		}
	}
	res := strings.TrimSpace(sb.String())
	if res == "" {
		return "host"
	}
	return res
}

// sanitizeFilename strips characters that are disallowed in file systems (e.g. colons on Windows).
func sanitizeFilename(name string) string {
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			sb.WriteRune(r)
		} else if r == ' ' {
			sb.WriteRune('_')
		}
	}
	res := strings.Trim(sb.String(), "._- ")
	if res == "" {
		return "host"
	}
	return res
}

// generateWindowsCmdScript generates a clean .cmd launcher that delegates in-memory key execution to PowerShell.
func generateWindowsCmdScript(ticket *NativeTicket) string {
	displayName := sanitizeDisplayName(ticket.HostName)
	port := ticket.HostPort
	if port <= 0 {
		port = 22
	}
	keyB64 := base64.StdEncoding.EncodeToString(ticket.EphemeralKey.PrivateKey)

	var remoteCmd string
	cdClause := buildRemoteCdClause(ticket.InitialDir)
	if cdClause != "" {
		escaped := strings.ReplaceAll(cdClause, `"`, `\"`)
		remoteCmd = fmt.Sprintf(` -t \"%s\"`, escaped)
	}

	var sb strings.Builder
	sb.WriteString("@echo off\r\n")
	sb.WriteString("REM Pantau Remote Ephemeral Terminal Launcher\r\n")
	psCmd := fmt.Sprintf(
		`powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$b='%s'; $key=[System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String($b)); $k=[System.IO.Path]::GetTempFileName(); Set-Content -Path $k -Value $key; icacls $k /inheritance:r /grant:r \"$($env:USERNAME):(R)\" | Out-Null; Write-Host '[Pantau] Connecting to %s (%s)...'; ssh -i $k -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s; $code=$LASTEXITCODE; Remove-Item -Force $k -ErrorAction SilentlyContinue; if ($code -ne 0) { Write-Host ''; Write-Host \"[Pantau] Sesi SSH terputus atau gagal (exit code: $code).\"; pause }"`,
		keyB64, displayName, ticket.HostAddress, port, ticket.HostUser, ticket.HostAddress, remoteCmd,
	)
	sb.WriteString(psCmd)
	sb.WriteString("\r\n")
	return sb.String()
}

// generateUnixScript generates a self-extracting, self-deleting script for macOS (.command) or Linux (.sh).
func generateUnixScript(ticket *NativeTicket, isMac bool) string {
	displayName := sanitizeDisplayName(ticket.HostName)
	port := ticket.HostPort
	if port <= 0 {
		port = 22
	}
	var remoteCmd string
	cdClause := buildRemoteCdClause(ticket.InitialDir)
	if cdClause != "" {
		remoteCmd = fmt.Sprintf(` -t "%s"`, cdClause)
	}

	var sb strings.Builder
	sb.WriteString("#!/usr/bin/env bash\n")
	sb.WriteString("set -e\n\n")
	sb.WriteString("KEYFILE=\"$(mktemp -t pantau_key.XXXXXX)\"\n")
	sb.WriteString("chmod 600 \"$KEYFILE\"\n")
	sb.WriteString("trap 'rm -f \"$KEYFILE\"; if [ -f \"$0\" ] && [ \"$0\" != \"bash\" ] && [ \"$0\" != \"sh\" ] && [ \"$0\" != \"-bash\" ] && [ \"$0\" != \"-sh\" ]; then rm -f \"$0\"; fi' EXIT INT TERM\n\n")
	sb.WriteString("cat << 'EOF' > \"$KEYFILE\"\n")
	sb.WriteString(strings.TrimSpace(string(ticket.EphemeralKey.PrivateKey)))
	sb.WriteString("\nEOF\n\n")
	sb.WriteString(fmt.Sprintf("echo \"[Pantau] Connecting to %s (%s)...\"\n", displayName, ticket.HostAddress))
	sb.WriteString("set +e\n")
	if isMac {
		// macOS Terminal runs under GUI session without attached tty on launch
		sb.WriteString("if [ -t 0 ]; then\n")
		sb.WriteString(fmt.Sprintf("    ssh -i \"$KEYFILE\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s\n", port, ticket.HostUser, ticket.HostAddress, remoteCmd))
		sb.WriteString("elif [ -c /dev/tty ]; then\n")
		sb.WriteString(fmt.Sprintf("    ssh -i \"$KEYFILE\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s < /dev/tty\n", port, ticket.HostUser, ticket.HostAddress, remoteCmd))
		sb.WriteString("else\n")
		sb.WriteString(fmt.Sprintf("    ssh -i \"$KEYFILE\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s\n", port, ticket.HostUser, ticket.HostAddress, remoteCmd))
		sb.WriteString("fi\n")
	} else {
		sb.WriteString("if [ -t 0 ]; then\n")
		sb.WriteString(fmt.Sprintf("    ssh -i \"$KEYFILE\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s\n", port, ticket.HostUser, ticket.HostAddress, remoteCmd))
		sb.WriteString("elif [ -c /dev/tty ]; then\n")
		sb.WriteString(fmt.Sprintf("    ssh -i \"$KEYFILE\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s < /dev/tty\n", port, ticket.HostUser, ticket.HostAddress, remoteCmd))
		sb.WriteString("else\n")
		sb.WriteString(fmt.Sprintf("    ssh -i \"$KEYFILE\" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s\n", port, ticket.HostUser, ticket.HostAddress, remoteCmd))
		sb.WriteString("fi\n")
	}
	sb.WriteString("SSH_EXIT=$?\n\n")
	sb.WriteString("if [ $SSH_EXIT -ne 0 ]; then\n")
	sb.WriteString("    echo \"\"\n")
	sb.WriteString("    echo \"[Pantau] Sesi SSH terputus atau gagal (exit code: $SSH_EXIT).\"\n")
	sb.WriteString("    echo \"Menjaga terminal tetap terbuka untuk diagnostik...\"\n")
	sb.WriteString("fi\n\n")
	if isMac {
		sb.WriteString("if [ -c /dev/tty ]; then\n")
		sb.WriteString("    exec $SHELL < /dev/tty\n")
		sb.WriteString("else\n")
		sb.WriteString("    exec $SHELL\n")
		sb.WriteString("fi\n")
	} else {
		sb.WriteString("if [ $SSH_EXIT -ne 0 ]; then\n")
		sb.WriteString("    if [ -c /dev/tty ]; then\n")
		sb.WriteString("        exec \"${SHELL:-bash}\" < /dev/tty\n")
		sb.WriteString("    else\n")
		sb.WriteString("        exec \"${SHELL:-bash}\"\n")
		sb.WriteString("    fi\n")
		sb.WriteString("fi\n")
	}
	return sb.String()
}

// flexibleID safely unmarshals host_id from either a JSON integer or string representation.
type flexibleID int64

func (f *flexibleID) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\" \t\r\n")
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid host id: %s", s)
	}
	*f = flexibleID(n)
	return nil
}

// --- HTTP Handlers ---

// handleNativeTerminalOpen: POST /api/terminal/native-open
// Directly spawns native terminal when Pantau runs locally.
func (s *Server) handleNativeTerminalOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return
	}

	var req struct {
		HostID     flexibleID `json:"host_id"`
		Path       string     `json:"path"`
		InitialDir string     `json:"initial_dir"`
		Mode       string     `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request body"})
		return
	}

	targetDir := req.Path
	if targetDir == "" {
		targetDir = req.InitialDir
	}

	host, err := s.db.GetHost(int64(req.HostID))
	if err != nil || host == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "host not found"})
		return
	}

	// Validate host address & user
	if !safeHostRegex.MatchString(host.Host) || !safeUserRegex.MatchString(host.User) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unsafe host target parameters"})
		return
	}

	// Read custom template if configured
	var customTemplate string
	mode, _ := s.db.GetSetting("terminal_launcher_mode")
	if mode == "custom" {
		customTemplate, _ = s.db.GetSetting("terminal_launcher_template")
	}

	if err := s.launchLocalTerminal(host, customTemplate, targetDir); err != nil {
		logger.Error("native_terminal", host.Name, fmt.Sprintf("launch failed: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": fmt.Sprintf("launch failed: %v", err)})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":      true,
		"message": "Native terminal launched successfully",
	})
}

// handleNativeTerminalTicket: POST /api/terminal/native-ticket
// Prepares an ephemeral key and returns a 60-second single-use download/streaming ticket.
func (s *Server) handleNativeTerminalTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]interface{}{"ok": false, "error": "method not allowed"})
		return
	}

	var req struct {
		HostID     flexibleID `json:"host_id"`
		Path       string     `json:"path"`
		InitialDir string     `json:"initial_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "invalid request body"})
		return
	}

	targetDir := req.Path
	if targetDir == "" {
		targetDir = req.InitialDir
	}

	host, err := s.db.GetHost(int64(req.HostID))
	if err != nil || host == nil {
		writeJSON(w, http.StatusNotFound, map[string]interface{}{"ok": false, "error": "host not found"})
		return
	}

	if !safeHostRegex.MatchString(host.Host) || !safeUserRegex.MatchString(host.User) {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unsafe host target parameters"})
		return
	}

	// 1. Generate Ephemeral Key
	key, err := GenerateEphemeralKey(host.ID, 5*time.Minute)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": fmt.Sprintf("generate key: %v", err)})
		return
	}

	// 2. Inject to host
	if err := s.injectEphemeralKey(r.Context(), host, key); err != nil {
		logger.Error("native_terminal", host.Name, fmt.Sprintf("key injection failed: %v", err))
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{"ok": false, "error": fmt.Sprintf("inject key: %v", err)})
		return
	}

	// 3. Generate random 32-byte ticket token
	tokenBytes := make([]byte, 16)
	_, _ = rand.Read(tokenBytes)
	token := fmt.Sprintf("%x", tokenBytes)

	clientIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if clientIP == "" {
		clientIP = r.RemoteAddr
	}

	port := host.Port
	if port <= 0 {
		port = 22
	}

	ticket := &NativeTicket{
		Token:        token,
		HostID:       host.ID,
		HostName:     host.Name,
		HostAddress:  host.Host,
		HostPort:     port,
		HostUser:     host.User,
		InitialDir:   targetDir,
		EphemeralKey: key,
		ExpiresAt:    time.Now().Add(60 * time.Second),
		CreatedIP:    clientIP,
	}

	s.storeNativeTicket(ticket)

	// Format direct oneliner commands
	hostURL := r.Host
	proto := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		proto = "https"
	}
	streamURL := fmt.Sprintf("%s://%s/api/terminal/native-run?ticket=%s", proto, hostURL, token)
	protocolURL := fmt.Sprintf("pantau://%s/api/terminal/native-run?ticket=%s", hostURL, token)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok":           true,
		"ticket":       token,
		"expires_in":   60,
		"host_name":    host.Name,
		"stream_url":   streamURL,
		"protocol_url": protocolURL,
		"command_unix": fmt.Sprintf("curl -sSL %s | bash", streamURL),
		"command_win":  fmt.Sprintf("irm %s | iex", streamURL),
	})
}

// handleNativeTerminalDownload: GET /api/terminal/native-download?ticket=<token>&os=<win|mac|linux>
// Delivers the self-deleting script and burns the ticket immediately.
func (s *Server) handleNativeTerminalDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimRight(r.URL.Query().Get("ticket"), "/")
	ticket, ok := s.getAndBurnNativeTicket(token)
	if !ok || ticket == nil {
		http.Error(w, "ticket expired or already redeemed", http.StatusGone)
		return
	}

	targetOS := strings.ToLower(r.URL.Query().Get("os"))
	if targetOS == "" {
		targetOS = "linux"
	}

	var content, filename string
	cleanName := sanitizeFilename(ticket.HostName)
	if cleanName == "" {
		cleanName = fmt.Sprintf("host_%d", ticket.HostID)
	}

	switch targetOS {
	case "windows", "win":
		content = generateWindowsCmdScript(ticket)
		filename = fmt.Sprintf("pantau-%s.cmd", cleanName)
	case "macos", "mac", "darwin":
		content = generateUnixScript(ticket, true)
		filename = fmt.Sprintf("pantau-%s.command", cleanName)
	default:
		content = generateUnixScript(ticket, false)
		filename = fmt.Sprintf("pantau-%s.sh", cleanName)
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	_, _ = w.Write([]byte(content))
}

// handleNativeTerminalRun: GET /api/terminal/native-run?ticket=<token>
// Streams the single-use script directly into curl/bash or PowerShell.
func (s *Server) handleNativeTerminalRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	token := strings.TrimRight(r.URL.Query().Get("ticket"), "/")
	ticket, ok := s.getAndBurnNativeTicket(token)
	if !ok || ticket == nil {
		http.Error(w, "# ticket expired or already redeemed", http.StatusGone)
		return
	}

	// Detect client OS from User-Agent if possible
	ua := strings.ToLower(r.UserAgent())
	port := ticket.HostPort
	if port <= 0 {
		port = 22
	}
	var content string
	if strings.Contains(ua, "powershell") || strings.Contains(ua, "windows") {
		var remoteCmd string
		cdClause := buildRemoteCdClause(ticket.InitialDir)
		if cdClause != "" {
			remoteCmd = fmt.Sprintf(` -t "%s"`, cdClause)
		}

		// Output PowerShell friendly script
		content = fmt.Sprintf(`$k = [System.IO.Path]::GetTempFileName()
Set-Content -Path "$k" -Value @"
%s
"@
icacls "$k" /inheritance:r /grant:r "$($env:USERNAME):(R)" | Out-Null
ssh -i "$k" -o IdentitiesOnly=yes -o StrictHostKeyChecking=accept-new -o HostKeyAlgorithms=+ssh-rsa,ssh-dss -o PubkeyAcceptedKeyTypes=+ssh-rsa -p %d %s@%s%s
Remove-Item -Force "$k" -ErrorAction SilentlyContinue
`, strings.TrimSpace(string(ticket.EphemeralKey.PrivateKey)), port, ticket.HostUser, ticket.HostAddress, remoteCmd)
	} else {
		content = generateUnixScript(ticket, strings.Contains(ua, "macintosh") || strings.Contains(ua, "darwin"))
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	_, _ = w.Write([]byte(content))
}
