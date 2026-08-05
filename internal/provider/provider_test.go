package provider

import (
	"context"
	"testing"

	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMetadata(t *testing.T) {
	t.Parallel()

	var resp provider.MetadataResponse
	New("test")().Metadata(context.Background(), provider.MetadataRequest{}, &resp)

	if resp.TypeName != "podman-quadlet" {
		t.Fatalf("expected provider type podman-quadlet, got %q", resp.TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("expected provider version test, got %q", resp.Version)
	}
}

func TestSchemasAreValid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var providerResp provider.SchemaResponse
	New("test")().Schema(ctx, provider.SchemaRequest{}, &providerResp)
	if diagnostics := providerResp.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
		t.Fatalf("invalid provider schema: %v", diagnostics)
	}

	resources := []resource.Resource{newContainerResource(), newNetworkResource(), newVolumeResource()}
	for _, providerResource := range resources {
		var metadataResp resource.MetadataResponse
		providerResource.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "podman-quadlet"}, &metadataResp)
		var schemaResp resource.SchemaResponse
		providerResource.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
		if diagnostics := schemaResp.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
			t.Errorf("invalid %s schema: %v", metadataResp.TypeName, diagnostics)
		}
	}
}

var providerAttributeTypes = map[string]attr.Type{
	"host":                     types.StringType,
	"user":                     types.StringType,
	"port":                     types.Int64Type,
	"private_key_path":         types.StringType,
	"password":                 types.StringType,
	"become_password":          types.StringType,
	"known_hosts_path":         types.StringType,
	"use_agent":                types.BoolType,
	"insecure_ignore_host_key": types.BoolType,
	"connection_timeout":       types.StringType,
	"quadlet_directory":        types.StringType,
	"mode":                     types.StringType,
	"sudo":                     types.BoolType,
}

func configureResponse(ctx context.Context, values map[string]attr.Value) *provider.ConfigureResponse {
	var schemaResp provider.SchemaResponse
	New("test")().Schema(ctx, provider.SchemaRequest{}, &schemaResp)

	attributes := map[string]attr.Value{
		"host":                     types.StringValue("edge.example.com"),
		"user":                     types.StringValue("containers"),
		"port":                     types.Int64Null(),
		"private_key_path":         types.StringNull(),
		"password":                 types.StringNull(),
		"become_password":          types.StringNull(),
		"known_hosts_path":         types.StringNull(),
		"use_agent":                types.BoolNull(),
		"insecure_ignore_host_key": types.BoolNull(),
		"connection_timeout":       types.StringNull(),
		"quadlet_directory":        types.StringNull(),
		"mode":                     types.StringNull(),
		"sudo":                     types.BoolNull(),
	}
	for key, value := range values {
		attributes[key] = value
	}
	config := types.ObjectValueMust(providerAttributeTypes, attributes)
	raw, err := config.ToTerraformValue(ctx)
	if err != nil {
		panic(err)
	}
	request := provider.ConfigureRequest{
		Config: tfsdk.Config{Raw: raw, Schema: schemaResp.Schema},
	}
	var response provider.ConfigureResponse
	New("test")().Configure(ctx, request, &response)
	return &response
}

func configureData(t *testing.T, values map[string]attr.Value) *providerData {
	t.Helper()
	response := configureResponse(context.Background(), values)
	if response.Diagnostics.HasError() {
		t.Fatalf("configure diagnostics: %v", response.Diagnostics)
	}
	data, ok := response.ResourceData.(*providerData)
	if !ok {
		t.Fatalf("expected *providerData, got %T", response.ResourceData)
	}
	return data
}

func TestConfigureDefaultsToUserMode(t *testing.T) {
	t.Parallel()

	data := configureData(t, nil)
	if data.quadletDirectory != ".config/containers/systemd" {
		t.Fatalf("unexpected quadlet directory %q", data.quadletDirectory)
	}
	if data.systemctlPrefix != "systemctl --user" {
		t.Fatalf("unexpected systemctl prefix %q", data.systemctlPrefix)
	}
	if data.installTarget != "default.target" {
		t.Fatalf("unexpected install target %q", data.installTarget)
	}
}

func TestConfigureSystemModeWithSudo(t *testing.T) {
	t.Parallel()

	data := configureData(t, map[string]attr.Value{
		"mode": types.StringValue("system"),
		"sudo": types.BoolValue(true),
	})
	if data.quadletDirectory != "/etc/containers/systemd" {
		t.Fatalf("unexpected quadlet directory %q", data.quadletDirectory)
	}
	if data.systemctlPrefix != "systemctl" {
		t.Fatalf("unexpected systemctl prefix %q", data.systemctlPrefix)
	}
	if data.installTarget != "multi-user.target" {
		t.Fatalf("unexpected install target %q", data.installTarget)
	}
}

func TestConfigureSystemModeAsRoot(t *testing.T) {
	t.Parallel()

	data := configureData(t, map[string]attr.Value{
		"mode": types.StringValue("system"),
		"sudo": types.BoolValue(false),
		"user": types.StringValue("root"),
	})
	if data.systemctlPrefix != "systemctl" {
		t.Fatalf("unexpected systemctl prefix %q", data.systemctlPrefix)
	}
}

func TestConfigureRejectsSudoInUserMode(t *testing.T) {
	t.Parallel()

	response := configureResponse(context.Background(), map[string]attr.Value{
		"mode": types.StringValue("user"),
		"sudo": types.BoolValue(true),
	})
	if !response.Diagnostics.HasError() {
		t.Fatal("expected sudo-in-user-mode error")
	}
}

func TestConfigureRejectsInvalidMode(t *testing.T) {
	t.Parallel()

	response := configureResponse(context.Background(), map[string]attr.Value{
		"mode": types.StringValue("hybrid"),
	})
	if !response.Diagnostics.HasError() {
		t.Fatal("expected invalid-mode error")
	}
}

func TestConfigureRejectsSystemModeWithoutSudoForNonRoot(t *testing.T) {
	t.Parallel()

	response := configureResponse(context.Background(), map[string]attr.Value{
		"mode": types.StringValue("system"),
		"sudo": types.BoolValue(false),
	})
	if !response.Diagnostics.HasError() {
		t.Fatal("expected root-login requirement error")
	}
}

func TestConfigureRejectsRelativeSystemDirectory(t *testing.T) {
	t.Parallel()

	response := configureResponse(context.Background(), map[string]attr.Value{
		"mode":              types.StringValue("system"),
		"sudo":              types.BoolValue(true),
		"quadlet_directory": types.StringValue("etc/containers/systemd"),
	})
	if !response.Diagnostics.HasError() {
		t.Fatal("expected relative-system-directory error")
	}
}

func TestConfigureRejectsTraversalInUserMode(t *testing.T) {
	t.Parallel()

	response := configureResponse(context.Background(), map[string]attr.Value{
		"quadlet_directory": types.StringValue("../etc/containers/systemd"),
	})
	if !response.Diagnostics.HasError() {
		t.Fatal("expected traversal error")
	}
}

func TestConfigureAcceptsPassword(t *testing.T) {
	t.Parallel()

	data := configureData(t, map[string]attr.Value{
		"password": types.StringValue("hunter2"),
	})
	if _, ok := data.client.(*remote.SSHClient); !ok {
		t.Fatalf("expected *remote.SSHClient, got %T", data.client)
	}
}

func TestConfigureAcceptsBecomePasswordWithSudo(t *testing.T) {
	t.Parallel()

	data := configureData(t, map[string]attr.Value{
		"mode":            types.StringValue("system"),
		"sudo":            types.BoolValue(true),
		"become_password": types.StringValue("hunter2"),
	})
	if data.systemctlPrefix != "systemctl" {
		t.Fatalf("unexpected systemctl prefix %q", data.systemctlPrefix)
	}
	if _, ok := data.client.(*remote.SSHClient); !ok {
		t.Fatalf("expected *remote.SSHClient, got %T", data.client)
	}
}

func TestConfigureRejectsBecomePasswordWithoutSudo(t *testing.T) {
	t.Parallel()

	response := configureResponse(context.Background(), map[string]attr.Value{
		"become_password": types.StringValue("hunter2"),
	})
	if !response.Diagnostics.HasError() {
		t.Fatal("expected become-password-without-sudo error")
	}
}
