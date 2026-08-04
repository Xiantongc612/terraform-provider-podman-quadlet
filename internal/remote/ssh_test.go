package remote

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
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

	go acceptSSHConnections(listener, serverConfig)

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

func acceptSSHConnections(listener net.Listener, config *ssh.ServerConfig) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go serveSSHConnection(conn, config)
	}
}

func serveSSHConnection(conn net.Conn, config *ssh.ServerConfig) {
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
		go serveSSHChannel(channel, requests)
	}
}

func serveSSHChannel(channel ssh.Channel, requests <-chan *ssh.Request) {
	for request := range requests {
		switch request.Type {
		case "exec":
			_ = request.Reply(true, nil)
			payload := struct {
				Command string
			}{}
			if err := ssh.Unmarshal(request.Payload, &payload); err == nil {
				_, _ = channel.Write([]byte(payload.Command + "\n"))
			}
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{}))
			_ = channel.Close()
		default:
			_ = request.Reply(false, nil)
		}
	}
}
