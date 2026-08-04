package remote

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHConfig contains connection settings for a rootless Podman host.
type SSHConfig struct {
	Host                  string
	User                  string
	Port                  int
	PrivateKeyPath        string
	KnownHostsPath        string
	UseAgent              bool
	InsecureIgnoreHostKey bool
	Timeout               time.Duration
}

// SSHClient performs remote operations using a new SSH connection per operation.
type SSHClient struct {
	config SSHConfig
}

// NewSSHClient validates config and creates an SSH-backed remote client.
func NewSSHClient(config SSHConfig) (*SSHClient, error) {
	if config.Host == "" {
		return nil, errors.New("host must not be empty")
	}
	if config.User == "" {
		return nil, errors.New("user must not be empty")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("port must be between 1 and 65535, got %d", config.Port)
	}
	if config.Timeout <= 0 {
		return nil, errors.New("timeout must be greater than zero")
	}
	if !config.UseAgent && config.PrivateKeyPath == "" {
		return nil, errors.New("private_key_path is required when use_agent is false")
	}
	if !config.InsecureIgnoreHostKey && config.KnownHostsPath == "" {
		return nil, errors.New("known_hosts_path is required unless host key verification is disabled")
	}

	return &SSHClient{config: config}, nil
}

func (c *SSHClient) dial(ctx context.Context) (*ssh.Client, error) {
	authMethods, cleanup, err := c.authMethods()
	if err != nil {
		return nil, err
	}
	defer cleanup()

	hostKeyCallback, err := c.hostKeyCallback()
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User:            c.config.User,
		Auth:            authMethods,
		HostKeyCallback: hostKeyCallback,
		Timeout:         c.config.Timeout,
	}
	address := net.JoinHostPort(c.config.Host, fmt.Sprintf("%d", c.config.Port))
	netConn, err := (&net.Dialer{Timeout: c.config.Timeout}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", address, err)
	}

	conn, channels, requests, err := ssh.NewClientConn(netConn, address, config)
	if err != nil {
		_ = netConn.Close()
		return nil, fmt.Errorf("establish SSH connection to %s: %w", address, err)
	}

	return ssh.NewClient(conn, channels, requests), nil
}

func (c *SSHClient) authMethods() ([]ssh.AuthMethod, func(), error) {
	methods := make([]ssh.AuthMethod, 0, 2)
	cleanup := func() {}

	if c.config.PrivateKeyPath != "" {
		key, err := os.ReadFile(expandHome(c.config.PrivateKeyPath))
		if err != nil {
			return nil, cleanup, fmt.Errorf("read private key: %w", err)
		}
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, cleanup, fmt.Errorf("parse private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if c.config.UseAgent {
		socket := os.Getenv("SSH_AUTH_SOCK")
		if socket != "" {
			conn, err := net.Dial("unix", socket)
			if err != nil {
				return nil, cleanup, fmt.Errorf("connect to SSH agent: %w", err)
			}
			cleanup = func() { _ = conn.Close() }
			methods = append(methods, ssh.PublicKeysCallback(agent.NewClient(conn).Signers))
		}
	}

	if len(methods) == 0 {
		return nil, cleanup, errors.New("no SSH authentication method is available")
	}
	return methods, cleanup, nil
}

func (c *SSHClient) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if c.config.InsecureIgnoreHostKey {
		return ssh.InsecureIgnoreHostKey(), nil //nolint:gosec // Explicitly configured escape hatch.
	}
	callback, err := knownhosts.New(expandHome(c.config.KnownHostsPath))
	if err != nil {
		return nil, fmt.Errorf("load known hosts: %w", err)
	}
	return callback, nil
}

// ReadFile reads a file relative to the remote user's home directory.
func (c *SSHClient) ReadFile(ctx context.Context, filePath string) ([]byte, error) {
	client, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Close() }()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return nil, fmt.Errorf("start SFTP client: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	file, err := sftpClient.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open remote file %q: %w", filePath, err)
	}
	defer func() { _ = file.Close() }()

	contents, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("read remote file %q: %w", filePath, err)
	}
	return contents, nil
}

// WriteFile atomically replaces a file relative to the remote user's home.
func (c *SSHClient) WriteFile(ctx context.Context, filePath string, contents []byte) error {
	client, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("start SFTP client: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	if err := sftpClient.MkdirAll(path.Dir(filePath)); err != nil {
		return fmt.Errorf("create remote directory for %q: %w", filePath, err)
	}
	temporaryPath := filePath + ".podlet-tmp"
	file, err := sftpClient.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		return fmt.Errorf("create temporary remote file: %w", err)
	}
	if err := file.Chmod(0o644); err != nil {
		_ = file.Close()
		_ = sftpClient.Remove(temporaryPath)
		return fmt.Errorf("set remote file permissions: %w", err)
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		_ = sftpClient.Remove(temporaryPath)
		return fmt.Errorf("write temporary remote file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = sftpClient.Remove(temporaryPath)
		return fmt.Errorf("close temporary remote file: %w", err)
	}
	if err := sftpClient.PosixRename(temporaryPath, filePath); err != nil {
		_ = sftpClient.Remove(temporaryPath)
		return fmt.Errorf("replace remote file %q: %w", filePath, err)
	}
	return nil
}

// RemoveFile removes a file relative to the remote user's home directory.
func (c *SSHClient) RemoveFile(ctx context.Context, filePath string) error {
	client, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		return fmt.Errorf("start SFTP client: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	if err := sftpClient.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove remote file %q: %w", filePath, err)
	}
	return nil
}

// Run executes a command and returns its combined output.
func (c *SSHClient) Run(ctx context.Context, command string) (string, error) {
	client, err := c.dial(ctx)
	if err != nil {
		return "", err
	}
	defer func() { _ = client.Close() }()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("create SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	type result struct {
		output []byte
		err    error
	}
	done := make(chan result, 1)
	go func() {
		output, runErr := session.CombinedOutput(command)
		done <- result{output: output, err: runErr}
	}()

	select {
	case <-ctx.Done():
		_ = client.Close()
		return "", fmt.Errorf("run remote command: %w", ctx.Err())
	case completed := <-done:
		output := strings.TrimSpace(string(completed.output))
		if completed.err != nil {
			return output, fmt.Errorf("run remote command %q: %w", command, completed.err)
		}
		return output, nil
	}
}

func expandHome(filePath string) string {
	if filePath != "~" && !strings.HasPrefix(filePath, "~/") {
		return filePath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filePath
	}
	if filePath == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(filePath, "~/"))
}
