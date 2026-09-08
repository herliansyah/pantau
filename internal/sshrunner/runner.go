package sshrunner

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

type Runner interface {
	Exec(cmd string) (stdout, stderr string, exitCode int, err error)
	Stream(cmd string, out io.Writer) error
	SFTP() (*sftp.Client, error)
	Terminal(in io.Reader, out io.Writer, cols, rows int, resizeChan <-chan [2]int) error
	Close() error
}

type LiveSSHRunner struct {
	client *ssh.Client
}

func Connect(host string, port int, user, privateKeyPEM string, timeout time.Duration) (*LiveSSHRunner, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	signer, err := ssh.ParsePrivateKey([]byte(privateKeyPEM))
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		// ponytail: InsecureIgnoreHostKey simplifies first-time connection without manual known_hosts trust dance.
		// Upgrade path: implement known_hosts pinning with TOFU (Trust On First Use) stored in database.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("dial ssh %s: %w", addr, err)
	}

	return &LiveSSHRunner{client: client}, nil
}

type KeyProvisioner func(host string, port int, user, password, pubKey string) error

func DefaultKeyProvisioner(host string, port int, user, password, pubKey string) error {
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         8 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return fmt.Errorf("ssh password authentication failed: %w", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	defer session.Close()

	cleanKey := strings.TrimSpace(pubKey)
	injectCmd := fmt.Sprintf(`sh -c 'mkdir -p ~/.ssh && chmod 700 ~/.ssh && touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && (grep -qxF %q ~/.ssh/authorized_keys || echo %q >> ~/.ssh/authorized_keys)'`, cleanKey, cleanKey)

	out, err := session.CombinedOutput(injectCmd)
	if err != nil {
		return fmt.Errorf("injected command failed: %s (%w)", string(out), err)
	}

	return nil
}

func (r *LiveSSHRunner) Exec(cmd string) (stdout, stderr string, exitCode int, err error) {
	session, err := r.client.NewSession()
	if err != nil {
		return "", "", -1, err
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	execErr := session.Run(cmd)
	stdout = stdoutBuf.String()
	stderr = stderrBuf.String()

	if execErr != nil {
		if exitErr, ok := execErr.(*ssh.ExitError); ok {
			exitCode = exitErr.ExitStatus()
			return stdout, stderr, exitCode, nil
		}
		return stdout, stderr, -1, execErr
	}

	return stdout, stderr, 0, nil
}

func (r *LiveSSHRunner) Stream(cmd string, out io.Writer) error {
	session, err := r.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	session.Stdout = out
	session.Stderr = out
	return session.Run(cmd)
}

func (r *LiveSSHRunner) SFTP() (*sftp.Client, error) {
	return sftp.NewClient(r.client)
}

func (r *LiveSSHRunner) Terminal(in io.Reader, out io.Writer, cols, rows int, resizeChan <-chan [2]int) error {
	session, err := r.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 40
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		return fmt.Errorf("request pty: %w", err)
	}

	session.Stdin = in
	session.Stdout = out
	session.Stderr = out

	if err := session.Shell(); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}

	// Handle window resizing asynchronously
	go func() {
		for sz := range resizeChan {
			_ = session.WindowChange(sz[1], sz[0])
		}
	}()

	return session.Wait()
}

func (r *LiveSSHRunner) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

// MockRunner is the primary test seam for testing all Pantau inspections, terminal, and file logic without real SSH servers.
type MockRunner struct {
	mu           sync.Mutex
	Handlers     map[string]func() (stdout, stderr string, exitCode int, err error)
	DefaultExec  func(cmd string) (stdout, stderr string, exitCode int, err error)
	ExecHistory  []string
	Closed       bool
	SFTPProvider func() (*sftp.Client, error)
}

func NewMockRunner() *MockRunner {
	return &MockRunner{
		Handlers: make(map[string]func() (stdout, stderr string, exitCode int, err error)),
		DefaultExec: func(cmd string) (stdout, stderr string, exitCode int, err error) {
			return "", "", 0, nil
		},
	}
}

func (m *MockRunner) Exec(cmd string) (stdout, stderr string, exitCode int, err error) {
	m.mu.Lock()
	m.ExecHistory = append(m.ExecHistory, cmd)
	handler, exists := m.Handlers[cmd]
	defaultHandler := m.DefaultExec
	m.mu.Unlock()

	if exists && handler != nil {
		return handler()
	}

	// Substring match handler fallback
	m.mu.Lock()
	for prefix, h := range m.Handlers {
		if strings.HasPrefix(cmd, prefix) || strings.Contains(cmd, prefix) {
			m.mu.Unlock()
			return h()
		}
	}
	m.mu.Unlock()

	return defaultHandler(cmd)
}

func (m *MockRunner) Stream(cmd string, out io.Writer) error {
	stdout, _, _, _ := m.Exec(cmd)
	_, _ = io.WriteString(out, stdout)
	return nil
}

func (m *MockRunner) SFTP() (*sftp.Client, error) {
	if m.SFTPProvider != nil {
		return m.SFTPProvider()
	}
	return nil, fmt.Errorf("mock sftp not configured")
}

func (m *MockRunner) Terminal(in io.Reader, out io.Writer, cols, rows int, resizeChan <-chan [2]int) error {
	_, _ = fmt.Fprintln(out, "Mock SSH Terminal Connected")
	buf := make([]byte, 1024)
	for {
		n, err := in.Read(buf)
		if err != nil {
			return nil
		}
		if n > 0 {
			// echo back
			_, _ = out.Write(buf[:n])
		}
	}
}

func (m *MockRunner) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Closed = true
	return nil
}

// RunnerFactory lets components create real or mock runners.
type RunnerFactory func(host string, port int, user, key string) (Runner, error)

func DefaultFactory() RunnerFactory {
	return func(host string, port int, user, key string) (Runner, error) {
		return Connect(host, port, user, key, 8*time.Second)
	}
}

// SafeLocalListener helper for testing
func GetFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
