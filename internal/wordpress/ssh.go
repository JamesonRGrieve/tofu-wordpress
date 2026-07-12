// SPDX-License-Identifier: AGPL-3.0-or-later
//
// SSH transport for driving WP-CLI (`wp …`) and a handful of filesystem
// operations (rsync, symlink) on a WordPress host. WordPress installed state,
// wp-config.php, the system cron entry, and the content directory have no HTTP
// API — they are managed by running WP-CLI and shell commands on the box over
// SSH. The provider's resources drive them through this transport.
//
// Like the sibling tofu-opnsense/proxmox/ddwrt/tomato providers, we invoke the
// system `ssh` binary via os/exec rather than an in-process SSH library — this
// keeps go.mod unchanged (no golang.org/x/crypto/ssh) and reuses the lab's
// existing SSH machinery (OpenBao-signed certs / agent / ssh_config). A private
// key is only ever materialized when key_pem is supplied (written to a temp
// 0600 file per call, removed after); key_file and ssh_config paths never touch
// the material. Auth is key/cert by default; a password (SSHConfig.Password, e.g.
// a per-guest root pw from OpenBao) is supported for the password-only CT fleet
// via `sshpass -e` (password in $SSHPASS, never argv) and is used only when no
// key material is supplied.
package wordpress

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// SSHConfig configures the SSH transport. All fields optional except Host.
type SSHConfig struct {
	Host     string // host or host:port, no scheme
	Port     int    // SSH port (0 → use Host's :port or ssh default)
	User     string // SSH user (default "root")
	KeyFile  string // identity file (ssh -i); ssh_config/agent used when empty
	KeyPEM   string // private-key material (e.g. from OpenBao); temp 0600 file per call
	Password string // SSH password (e.g. a per-guest root pw from OpenBao); fed to ssh via sshpass -e (SSHPASS env, never argv). Used only when no key is supplied.
	Timeout  time.Duration
}

// SSHClient runs remote commands on a WordPress host over SSH. Safe for
// concurrent use; each call spawns its own ssh process. It satisfies Executor.
type SSHClient struct {
	addr     string
	port     string
	user     string
	keyFile  string
	keyPEM   string
	password string
	timeout  time.Duration
}

// User returns the SSH login user.
func (c *SSHClient) User() string { return c.user }

// usePassword reports whether this client authenticates by password (via sshpass)
// rather than a key. A key ALWAYS wins: password is used only when a password is
// set and no key material (file or PEM) is supplied.
func (c *SSHClient) usePassword() bool {
	return c.password != "" && c.keyFile == "" && strings.TrimSpace(c.keyPEM) == ""
}

// NewSSHClient builds an SSHClient. It does not contact the host until first use.
func NewSSHClient(c SSHConfig) *SSHClient {
	if c.Timeout == 0 {
		c.Timeout = 120 * time.Second
	}
	user := c.User
	if user == "" {
		user = "root"
	}
	addr, port := splitHostPort(c.Host)
	if c.Port != 0 {
		port = strconv.Itoa(c.Port)
	}
	return &SSHClient{addr: addr, port: port, user: user, keyFile: c.KeyFile, keyPEM: c.KeyPEM, password: c.Password, timeout: c.Timeout}
}

func splitHostPort(h string) (string, string) {
	h = strings.TrimSpace(h)
	h = strings.TrimPrefix(h, "ssh://")
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[i+1:], "]") {
		if _, err := strconv.Atoi(h[i+1:]); err == nil {
			return h[:i], h[i+1:]
		}
	}
	return h, ""
}

// SSHError is returned when an ssh invocation exits non-zero.
type SSHError struct {
	Cmd      string
	ExitCode int
	Stderr   string
}

func (e *SSHError) Error() string {
	return fmt.Sprintf("wordpress ssh %q: exit %d: %s", e.Cmd, e.ExitCode, strings.TrimSpace(e.Stderr))
}

const maxSSHAttempts = 4

func transientSSH(err error) bool {
	var se *SSHError
	if !errors.As(err, &se) {
		return false
	}
	return strings.Contains(se.Stderr, "kex_exchange_identification") ||
		strings.Contains(se.Stderr, "Connection reset by") ||
		strings.Contains(se.Stderr, "Connection closed by")
}

// Run executes a remote command (handed to the box's shell), retrying transient
// connection resets, and returns stdout. stdin (may be nil) is piped to the
// command — used to feed a script or file content to a command without
// shell-quoting it.
func (c *SSHClient) Run(remote string, stdin []byte) ([]byte, error) {
	var (
		out []byte
		err error
	)
	for attempt := 1; attempt <= maxSSHAttempts; attempt++ {
		out, err = c.runOnce(remote, stdin)
		if err == nil || attempt == maxSSHAttempts || !transientSSH(err) {
			return out, err
		}
		time.Sleep(time.Duration(attempt) * 250 * time.Millisecond)
	}
	return out, err
}

func (c *SSHClient) runOnce(remote string, stdin []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
	defer cancel()

	usePassword := c.usePassword()

	args := []string{
		// Relay-forwarded lab boxes present varying host keys and the runner's
		// known_hosts may be unwritable — discard host-key state so a key change
		// never blocks the connection.
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", fmt.Sprintf("ConnectTimeout=%d", connectTimeoutSeconds(c.timeout)),
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
	}
	if usePassword {
		// BatchMode=yes disables password prompts, so it must be OFF for sshpass;
		// force password auth so a stray agent/default key can't preempt it.
		args = append(args,
			"-o", "BatchMode=no",
			"-o", "PreferredAuthentications=password",
			"-o", "PubkeyAuthentication=no",
		)
	} else {
		args = append(args, "-o", "BatchMode=yes")
	}
	if c.port != "" {
		args = append(args, "-p", c.port)
	}
	keyPath, cleanup, kerr := c.identityFile()
	if kerr != nil {
		return nil, fmt.Errorf("wordpress: materialize ssh identity: %w", kerr)
	}
	defer cleanup()
	if keyPath != "" {
		args = append(args, "-i", keyPath, "-o", "IdentitiesOnly=yes")
	}
	target := c.addr
	if c.user != "" {
		target = c.user + "@" + c.addr
	}
	args = append(args, target, remote)

	var cmd *exec.Cmd
	if usePassword {
		// sshpass -e reads the password from $SSHPASS — never argv/process list.
		cmd = exec.CommandContext(ctx, "sshpass", append([]string{"-e", "ssh"}, args...)...)
		cmd.Env = append(os.Environ(), "SSHPASS="+c.password)
	} else {
		cmd = exec.CommandContext(ctx, "ssh", args...)
	}
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		code := -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("wordpress ssh %q: timed out after %s", remote, c.timeout)
		}
		return nil, &SSHError{Cmd: remote, ExitCode: code, Stderr: stderr.String()}
	}
	return stdout.Bytes(), nil
}

// identityFile resolves the SSH identity. An explicit keyFile wins (no cleanup).
// Else keyPEM is written to a temp 0600 file (cleaned up after). Neither → ssh
// falls back to ssh_config/agent.
func (c *SSHClient) identityFile() (path string, cleanup func(), err error) {
	noop := func() {}
	if c.keyFile != "" {
		return c.keyFile, noop, nil
	}
	if strings.TrimSpace(c.keyPEM) == "" {
		return "", noop, nil
	}
	f, err := os.CreateTemp("", "wordpress-key-*")
	if err != nil {
		return "", noop, err
	}
	name := f.Name()
	fail := func(e error) (string, func(), error) { _ = f.Close(); _ = os.Remove(name); return "", noop, e }
	if err := f.Chmod(0o600); err != nil {
		return fail(err)
	}
	if _, err := f.WriteString(strings.TrimRight(c.keyPEM, "\n") + "\n"); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", noop, err
	}
	return name, func() { _ = os.Remove(name) }, nil
}

func connectTimeoutSeconds(d time.Duration) int {
	s := int(d.Seconds()) / 2
	if s < 1 {
		s = 1
	}
	return s
}
