package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/Xiantongc612/podlet-provider/internal/quadlet"
	"github.com/Xiantongc612/podlet-provider/internal/remote"
)

type fakeRemote struct {
	files    map[string][]byte
	commands []string
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

func TestManagedLifecycle(t *testing.T) {
	t.Parallel()

	client := &fakeRemote{files: make(map[string][]byte)}
	managed := managedResource{client: client, quadletDirectory: ".config/containers/systemd"}
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
}
