package remote

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHClientPasswordAuth(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create host key signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == "secret" {
				return nil, nil
			}
			return nil, errors.New("password rejected")
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go acceptSSHConnections(listener, serverConfig, false)

	config := SSHConfig{
		Host:                  "127.0.0.1",
		User:                  "test",
		Port:                  listener.Addr().(*net.TCPAddr).Port,
		UseAgent:              false,
		Password:              "secret",
		InsecureIgnoreHostKey: true,
		Timeout:               5 * time.Second,
	}
	client, err := NewSSHClient(config)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	output, err := client.Run(context.Background(), "echo hello")
	if err != nil {
		t.Fatalf("run with correct password: %v", err)
	}
	if output != "echo hello" {
		t.Fatalf("unexpected output %q", output)
	}

	config.Password = "wrong"
	wrongClient, err := NewSSHClient(config)
	if err != nil {
		t.Fatalf("create client with wrong password: %v", err)
	}
	if _, err := wrongClient.Run(context.Background(), "echo hello"); err == nil {
		t.Fatal("expected authentication failure with the wrong password")
	}
}

func TestSSHClientBecomePassword(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create host key signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == "secret" {
				return nil, nil
			}
			return nil, errors.New("password rejected")
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go acceptSSHConnections(listener, serverConfig, true)

	base := SSHConfig{
		Host:                  "127.0.0.1",
		User:                  "test",
		Port:                  listener.Addr().(*net.TCPAddr).Port,
		UseAgent:              false,
		Password:              "secret",
		InsecureIgnoreHostKey: true,
		Timeout:               5 * time.Second,
		Mode:                  ModeSystem,
		Sudo:                  true,
	}

	client, err := NewSSHClient(base)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	output, err := client.Run(context.Background(), "cat /etc/shadow")
	if err != nil {
		t.Fatalf("run elevated command: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(output), "sudo cat /etc/shadow") {
		t.Fatalf("expected plain sudo command, got %q", output)
	}

	base.BecomePassword = "become-secret"
	client, err = NewSSHClient(base)
	if err != nil {
		t.Fatalf("create client with become password: %v", err)
	}
	output, err = client.Run(context.Background(), "cat /etc/shadow")
	if err != nil {
		t.Fatalf("run elevated command with become password: %v", err)
	}
	if !strings.Contains(output, "sudo -S -p '' cat /etc/shadow") {
		t.Fatalf("expected sudo -S command, got %q", output)
	}
	if !strings.Contains(output, "become-secret") {
		t.Fatalf("expected become password on stdin, got %q", output)
	}
}

func TestSSHClientRunWithInputBecomePassword(t *testing.T) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("create host key signer: %v", err)
	}

	serverConfig := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == "secret" {
				return nil, nil
			}
			return nil, errors.New("password rejected")
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	go acceptSSHConnections(listener, serverConfig, true)

	base := SSHConfig{
		Host:                  "127.0.0.1",
		User:                  "test",
		Port:                  listener.Addr().(*net.TCPAddr).Port,
		UseAgent:              false,
		Password:              "secret",
		InsecureIgnoreHostKey: true,
		Timeout:               5 * time.Second,
		Mode:                  ModeSystem,
		Sudo:                  true,
		BecomePassword:        "become-secret",
	}
	client, err := NewSSHClient(base)
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	output, err := client.RunWithInput(context.Background(), "podman secret create mysecret -", []byte("secret-value"))
	if err != nil {
		t.Fatalf("run with input: %v", err)
	}
	if !strings.Contains(output, "sudo -S -p '' podman secret create mysecret -") {
		t.Fatalf("expected sudo -S command, got %q", output)
	}
	if !strings.Contains(output, "become-secret\nsecret-value") {
		t.Fatalf("expected password then input on stdin, got %q", output)
	}
}

func TestElevatedCommand(t *testing.T) {
	t.Parallel()

	plain := &SSHClient{config: SSHConfig{Mode: ModeUser}}
	if command, input := plain.elevatedCommand("ls /etc"); command != "ls /etc" || input != nil {
		t.Fatalf("user-mode command = %q, %v; want unchanged", command, input)
	}

	nopasswd := SSHConfig{Mode: ModeSystem, Sudo: true}
	client := &SSHClient{config: nopasswd, elevate: true}
	if command, input := client.elevatedCommand("ls /etc"); command != "sudo ls /etc" || input != nil {
		t.Fatalf("NOPASSWD command = %q, %v; want sudo without input", command, input)
	}

	become := SSHConfig{Mode: ModeSystem, Sudo: true, BecomePassword: "hunter2"}
	client = &SSHClient{config: become, elevate: true}
	command, input := client.elevatedCommand("ls /etc")
	if command != "sudo -S -p '' ls /etc" {
		t.Fatalf("become command = %q, want sudo -S", command)
	}
	if string(input) != "hunter2\n" {
		t.Fatalf("become input = %q, want password with newline", input)
	}
}

func acceptSSHConnections(listener net.Listener, config *ssh.ServerConfig, readStdin bool) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go serveSSHConnection(conn, config, readStdin)
	}
}

func serveSSHConnection(conn net.Conn, config *ssh.ServerConfig, readStdin bool) {
	serverConn, channels, requests, err := ssh.NewServerConn(conn, config)
	if err != nil {
		_ = conn.Close()
		return
	}
	go ssh.DiscardRequests(requests)
	defer func() { _ = serverConn.Close() }()

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(ssh.UnknownChannelType, "unsupported channel type")
			continue
		}
		channel, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go serveSSHChannel(channel, requests, readStdin)
	}
}

func serveSSHChannel(channel ssh.Channel, requests <-chan *ssh.Request, readStdin bool) {
	for request := range requests {
		switch request.Type {
		case "exec":
			_ = request.Reply(true, nil)
			payload := struct {
				Command string
			}{}
			var response bytes.Buffer
			if err := ssh.Unmarshal(request.Payload, &payload); err == nil {
				response.WriteString(payload.Command)
				if readStdin {
					stdin := make(chan []byte, 1)
					go func() {
						data, _ := io.ReadAll(channel)
						stdin <- data
					}()
					select {
					case data := <-stdin:
						response.WriteString(" | stdin:")
						response.Write(data)
					case <-time.After(2 * time.Second):
						response.WriteString(" | stdin:<none>")
					}
				}
			}
			_, _ = channel.Write(response.Bytes())
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{}))
			_ = channel.Close()
		default:
			_ = request.Reply(false, nil)
		}
	}
}
