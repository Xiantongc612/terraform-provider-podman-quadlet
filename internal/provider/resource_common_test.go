package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/quadlet"
	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/remote"
)

type fakeRemote struct {
	files     map[string][]byte
	commands  []string
	lastInput []byte
}

func (f *fakeRemote) ReadFile(_ context.Context, path string) ([]byte, error) {
	content, ok := f.files[path]
	if !ok {
		return nil, remote.ErrNotFound
	}
	return content, nil
}

func (f *fakeRemote) WriteFile(_ context.Context, path string, content []byte) error {
	f.files[path] = content
	return nil
}

func (f *fakeRemote) RemoveFile(_ context.Context, path string) error {
	delete(f.files, path)
	return nil
}

func (f *fakeRemote) Run(_ context.Context, command string) (string, error) {
	f.commands = append(f.commands, command)
	if strings.Contains(command, " show ") {
		return "LoadState=loaded\nActiveState=active\nSubState=running", nil
	}
	return "", nil
}

func (f *fakeRemote) RunWithInput(_ context.Context, command string, input []byte) (string, error) {
	f.commands = append(f.commands, command)
	f.lastInput = input
	if strings.Contains(command, " inspect ") {
		return "", nil
	}
	return "inspect-result", nil
}

func TestManagedLifecycle(t *testing.T) {
	t.Parallel()

	client := &fakeRemote{files: make(map[string][]byte)}
	managed := managedResource{
		client:           client,
		quadletDirectory: ".config/containers/systemd",
		systemctlPrefix:  "systemctl --user",
		installTarget:    "default.target",
	}
	content := quadlet.Render(quadlet.Section{Name: "Network"})
	status, err := managed.apply(
		context.Background(),
		".config/containers/systemd/example.network",
		"example-network.service",
		content,
		true,
	)
	if err != nil {
		t.Fatalf("apply returned an error: %v", err)
	}
	if status.ActiveState.ValueString() != "active" {
		t.Fatalf("unexpected status: %#v", status)
	}
	if len(client.commands) != 3 {
		t.Fatalf("expected reload, start, and status commands, got %#v", client.commands)
	}
	if err := managed.delete(
		context.Background(),
		".config/containers/systemd/example.network",
		"example-network.service",
	); err != nil {
		t.Fatalf("delete returned an error: %v", err)
	}
	if _, ok := client.files[".config/containers/systemd/example.network"]; ok {
		t.Fatal("managed file was not removed")
	}
}

func TestManagedLifecycleSystemMode(t *testing.T) {
	t.Parallel()

	client := &fakeRemote{files: make(map[string][]byte)}
	managed := managedResource{
		client:           client,
		quadletDirectory: "/etc/containers/systemd",
		systemctlPrefix:  "systemctl",
		installTarget:    "multi-user.target",
	}
	content := quadlet.Render(quadlet.Section{Name: "Network"})
	status, err := managed.apply(
		context.Background(),
		"/etc/containers/systemd/example.network",
		"example-network.service",
		content,
		true,
	)
	if err != nil {
		t.Fatalf("apply returned an error: %v", err)
	}
	if status.ActiveState.ValueString() != "active" {
		t.Fatalf("unexpected status: %#v", status)
	}
	for _, command := range client.commands {
		if !strings.HasPrefix(command, "systemctl ") {
			t.Errorf("expected systemctl command, got %q", command)
		}
	}
	if err := managed.delete(
		context.Background(),
		"/etc/containers/systemd/example.network",
		"example-network.service",
	); err != nil {
		t.Fatalf("delete returned an error: %v", err)
	}
}

func TestInstallSectionTarget(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		target   string
		expected string
	}{
		{"default.target", "WantedBy=default.target"},
		{"multi-user.target", "WantedBy=multi-user.target"},
	} {
		rendered := quadlet.Render(installSection(test.target))
		if !strings.Contains(string(rendered), test.expected) {
			t.Errorf("install section for %q does not contain %q:\n%s", test.target, test.expected, rendered)
		}
	}
}

func TestManagedLifecycleProtectsUnmanagedFiles(t *testing.T) {
	t.Parallel()

	client := &fakeRemote{files: map[string][]byte{"example.network": []byte("[Network]\n")}}
	managed := managedResource{client: client}
	_, err := managed.apply(
		context.Background(),
		"example.network",
		"example-network.service",
		quadlet.Render(quadlet.Section{Name: "Network"}),
		true,
	)
	if err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("expected unmanaged-file error, got %v", err)
	}
	_, err = managed.apply(
		context.Background(),
		"example.network",
		"example-network.service",
		quadlet.Render(quadlet.Section{Name: "Network"}),
		false,
	)
	if err == nil || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("expected update to protect unmanaged file, got %v", err)
	}
}
