package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSecretCreateCommand(t *testing.T) {
	t.Parallel()

	model := secretResourceModel{
		Metadata: metadataModel{
			Name:   types.StringValue("db"),
			Labels: types.MapValueMust(types.StringType, map[string]attr.Value{"app": types.StringValue("example")}),
		},
		Spec: secretSpecModel{
			Value:      types.StringValue("s3cr3t"),
			Driver:     types.StringValue("file"),
			DriverOpts: types.MapValueMust(types.StringType, map[string]attr.Value{"key": types.StringValue("value")}),
		},
	}
	command, diagnostics := renderSecretCreate(context.Background(), &model)
	if diagnostics.HasError() {
		t.Fatalf("render diagnostics: %v", diagnostics)
	}
	if strings.Contains(command, "s3cr3t") {
		t.Fatalf("command must not contain the secret value: %s", command)
	}
	for _, part := range []string{"--driver", "file", "--driver-opts", "key=value", "--label", "app=example", "db", "-"} {
		if !strings.Contains(command, "'"+part+"'") {
			t.Errorf("command does not contain %q: %s", part, command)
		}
	}
}

func TestSecretInspect(t *testing.T) {
	t.Parallel()

	client := &fakeRemote{files: make(map[string][]byte)}
	resource := secretResource{managedResource: managedResource{client: client}}

	status, report, err := resource.inspectSecret(context.Background(), "db")
	if err != nil {
		t.Fatalf("inspectSecret returned an error: %v", err)
	}
	if status.ID.ValueString() != "inspect-id" {
		t.Fatalf("unexpected status id %q", status.ID.ValueString())
	}
	if status.Driver.ValueString() != "file" {
		t.Fatalf("unexpected status driver %q", status.Driver.ValueString())
	}
	if report.Spec.Name != "test-secret" {
		t.Fatalf("unexpected spec name %q", report.Spec.Name)
	}
}

func TestSecretNotFound(t *testing.T) {
	t.Parallel()

	notFound := errors.New("run remote command: exit status 1: Error: no secret with name or ID \"db\" found")
	if !isSecretNotFound(notFound) {
		t.Fatal("expected not-found error to be detected")
	}
	if isSecretNotFound(errors.New("run remote command: exit status 1: Error: cannot remove secret \"db\": secret is in use")) {
		t.Fatal("unexpected in-use error detected as not found")
	}
	if isSecretNotFound(nil) {
		t.Fatal("nil error detected as not found")
	}
}

func TestSecretValueDelivery(t *testing.T) {
	t.Parallel()

	client := &fakeRemote{files: make(map[string][]byte)}
	resource := secretResource{managedResource: managedResource{client: client}}

	model := secretResourceModel{
		Metadata: metadataModel{Name: types.StringValue("db")},
		Spec: secretSpecModel{
			Value:      types.StringValue("s3cr3t\nline2"),
			Driver:     types.StringValue("file"),
			DriverOpts: types.MapNull(types.StringType),
		},
	}
	command, diagnostics := renderSecretCreate(context.Background(), &model)
	if diagnostics.HasError() {
		t.Fatalf("render diagnostics: %v", diagnostics)
	}
	if _, err := resource.client.RunWithInput(context.Background(), command, []byte(model.Spec.Value.ValueString())); err != nil {
		t.Fatalf("create returned an error: %v", err)
	}
	if string(client.lastInput) != "s3cr3t\nline2" {
		t.Fatalf("expected value on stdin, got %q", client.lastInput)
	}
	for _, command := range client.commands {
		if strings.Contains(command, "s3cr3t") {
			t.Errorf("secret value leaked into command %q", command)
		}
	}
}

func TestSecretValidation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	base := secretResourceModel{
		Metadata: metadataModel{Name: types.StringValue("db")},
		Spec: secretSpecModel{
			Value:      types.StringValue("secret"),
			Driver:     types.StringValue("file"),
			DriverOpts: types.MapNull(types.StringType),
		},
	}
	for name, modify := range map[string]func(*secretResourceModel){
		"missing value": func(model *secretResourceModel) { model.Spec.Value = types.StringNull() },
		"oversized value": func(model *secretResourceModel) {
			model.Spec.Value = types.StringValue(strings.Repeat("x", maxSecretBytes+1))
		},
		"multi-line driver": func(model *secretResourceModel) { model.Spec.Driver = types.StringValue("file\nother") },
		"multi-line option": func(model *secretResourceModel) {
			model.Spec.DriverOpts = types.MapValueMust(types.StringType, map[string]attr.Value{"k": types.StringValue("a\nb")})
		},
	} {
		model := base
		modify(&model)
		var diagnostics diag.Diagnostics
		validateSecretSpec(ctx, model.Spec, &diagnostics)
		if !diagnostics.HasError() {
			t.Errorf("%s: expected validation error", name)
		}
	}
}
