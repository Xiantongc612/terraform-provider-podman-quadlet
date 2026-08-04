//go:build integration

package remote

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestSSHIntegration(t *testing.T) {
	host := os.Getenv("PODLET_TEST_HOST")
	user := os.Getenv("PODLET_TEST_USER")
	if host == "" || user == "" {
		t.Skip("PODLET_TEST_HOST and PODLET_TEST_USER are required")
	}
	port := 22
	if value := os.Getenv("PODLET_TEST_PORT"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("invalid PODLET_TEST_PORT: %v", err)
		}
		port = parsed
	}
	mode := ModeUser
	if value := os.Getenv("PODLET_TEST_MODE"); value != "" {
		if value != ModeUser && value != ModeSystem {
			t.Fatalf("invalid PODLET_TEST_MODE %q", value)
		}
		mode = value
	}
	sudo := os.Getenv("PODLET_TEST_SUDO") == "true"
	client, err := NewSSHClient(SSHConfig{
		Host:           host,
		User:           user,
		Port:           port,
		PrivateKeyPath: os.Getenv("PODLET_TEST_PRIVATE_KEY_PATH"),
		KnownHostsPath: valueOrDefault(os.Getenv("PODLET_TEST_KNOWN_HOSTS_PATH"), "~/.ssh/known_hosts"),
		UseAgent:       true,
		Timeout:        30 * time.Second,
		Mode:           mode,
		Sudo:           sudo,
	})
	if err != nil {
		t.Fatalf("create SSH client: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	prerequisites := "podman --version"
	if mode == ModeUser {
		prerequisites += " && systemctl --user --version"
	}
	if _, err := client.Run(ctx, prerequisites); err != nil {
		t.Fatalf("verify remote prerequisites: %v", err)
	}
	probe := fmt.Sprintf(".cache/podlet-provider/integration-%d", time.Now().UnixNano())
	expected := []byte("podlet-provider integration probe\n")
	if err := client.WriteFile(ctx, probe, expected); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	t.Cleanup(func() { _ = client.RemoveFile(context.Background(), probe) })
	actual, err := client.ReadFile(ctx, probe)
	if err != nil {
		t.Fatalf("read probe: %v", err)
	}
	if string(actual) != string(expected) {
		t.Fatalf("probe content = %q, want %q", actual, expected)
	}
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
