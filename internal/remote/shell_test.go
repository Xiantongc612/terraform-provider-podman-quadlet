package remote

import (
	"testing"
	"time"
)

func TestShellQuote(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"":        "''",
		"service": "'service'",
		"a b":     "'a b'",
		"a'b":     "'a'\"'\"'b'",
	}
	for input, expected := range tests {
		if actual := ShellQuote(input); actual != expected {
			t.Errorf("ShellQuote(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestNewSSHClientModeValidation(t *testing.T) {
	t.Parallel()

	base := SSHConfig{
		Host:                  "example.com",
		User:                  "containers",
		Port:                  22,
		UseAgent:              true,
		InsecureIgnoreHostKey: true,
		Timeout:               30 * time.Second,
	}
	if _, err := NewSSHClient(base); err != nil {
		t.Fatalf("default user-mode client failed: %v", err)
	}
	if _, err := NewSSHClient(withMode(base, "invalid")); err == nil {
		t.Fatal("expected invalid-mode error")
	}
	if _, err := NewSSHClient(withMode(withSudo(base, true), "user")); err == nil {
		t.Fatal("expected sudo-in-user-mode error")
	}
	client, err := NewSSHClient(withMode(withSudo(base, true), "system"))
	if err != nil {
		t.Fatalf("system-mode client failed: %v", err)
	}
	if !client.elevate {
		t.Fatal("expected system-mode sudo client to elevate")
	}
	if _, err := NewSSHClient(withMode(withSudo(base, false), "system")); err != nil {
		t.Fatalf("system-mode client without sudo failed: %v", err)
	}
}

func TestNewSSHClientPasswordAuth(t *testing.T) {
	t.Parallel()

	config := SSHConfig{
		Host:                  "example.com",
		User:                  "containers",
		Port:                  22,
		UseAgent:              false,
		Password:              "hunter2",
		InsecureIgnoreHostKey: true,
		Timeout:               30 * time.Second,
	}
	client, err := NewSSHClient(config)
	if err != nil {
		t.Fatalf("create password client: %v", err)
	}
	if client.config.Password != "hunter2" {
		t.Fatalf("password not stored on client: %q", client.config.Password)
	}

	config.Password = ""
	if _, err := NewSSHClient(config); err == nil {
		t.Fatal("expected auth-method error when use_agent is false without a key or password")
	}
}

func withMode(config SSHConfig, mode string) SSHConfig {
	config.Mode = mode
	return config
}

func withSudo(config SSHConfig, sudo bool) SSHConfig {
	config.Sudo = sudo
	return config
}

func TestNewSSHClientBecomePasswordValidation(t *testing.T) {
	t.Parallel()

	base := SSHConfig{
		Host:                  "example.com",
		User:                  "containers",
		Port:                  22,
		UseAgent:              true,
		InsecureIgnoreHostKey: true,
		Timeout:               30 * time.Second,
		Mode:                  ModeSystem,
		BecomePassword:        "hunter2",
	}
	if _, err := NewSSHClient(base); err == nil {
		t.Fatal("expected become-password-without-sudo error")
	}
	if _, err := NewSSHClient(withSudo(base, true)); err != nil {
		t.Fatalf("become password with sudo failed: %v", err)
	}
}

func TestNewSSHClientDefaultsToUserMode(t *testing.T) {
	t.Parallel()

	client, err := NewSSHClient(SSHConfig{
		Host:                  "example.com",
		User:                  "containers",
		Port:                  22,
		UseAgent:              true,
		InsecureIgnoreHostKey: true,
		Timeout:               30 * time.Second,
	})
	if err != nil {
		t.Fatalf("create client: %v", err)
	}
	if client.config.Mode != "user" {
		t.Fatalf("expected default mode user, got %q", client.config.Mode)
	}
	if client.elevate {
		t.Fatal("expected user mode to not elevate")
	}
}
