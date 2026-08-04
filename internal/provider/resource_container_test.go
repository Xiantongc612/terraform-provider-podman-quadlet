package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestContainerRenderAndParse(t *testing.T) {
	t.Parallel()

	model := containerResourceModel{
		Metadata: metadataModel{
			Name:        types.StringValue("service"),
			Description: types.StringValue("Example service"),
			Labels:      types.MapNull(types.StringType),
		},
		Spec: emptyContainerSpec(),
	}
	model.Spec.Image = types.StringValue("quay.io/example/service:1")
	model.Spec.Command = stringListValue([]string{"/usr/bin/service", "serve"})
	model.Spec.Arguments = stringListValue([]string{"--message", "hello world"})
	model.Spec.Environment = types.MapValueMust(types.StringType, map[string]attr.Value{
		"LOG_LEVEL": types.StringValue("info"),
	})
	model.Spec.Ports = []containerPortModel{{
		HostIP:        types.StringValue("127.0.0.1"),
		HostPort:      types.Int64Value(8080),
		ContainerPort: types.Int64Value(80),
		Protocol:      types.StringNull(),
	}}
	model.Spec.Mounts = []containerMountModel{{
		Source:   types.StringValue("service-data.volume"),
		Target:   types.StringValue("/var/lib/service"),
		ReadOnly: types.BoolValue(true),
		Options:  types.ListNull(types.StringType),
	}}
	model.Spec.Networks = stringSetValue([]string{"service.network"})
	model.Spec.HealthCheck = &containerHealthModel{
		Command:     stringListValue([]string{"curl", "--fail", "http://localhost/health"}),
		Interval:    types.StringValue("30s"),
		Timeout:     types.StringNull(),
		Retries:     types.Int64Null(),
		StartPeriod: types.StringNull(),
	}
	model.Spec.Service = &containerServiceModel{
		Restart:    types.StringValue("on-failure"),
		RestartSec: types.StringNull(),
	}

	content, diagnostics := renderContainer(context.Background(), &model, "default.target")
	if diagnostics.HasError() {
		t.Fatalf("render diagnostics: %v", diagnostics)
	}
	text := string(content)
	for _, expected := range []string{
		"Image=quay.io/example/service:1",
		"Pull=missing",
		`Exec="--message" "hello world"`,
		`Environment="LOG_LEVEL=info"`,
		"PublishPort=127.0.0.1:8080:80/tcp",
		"Volume=service-data.volume:/var/lib/service:ro",
		"Network=service.network",
		"Restart=on-failure",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rendered container does not contain %q:\n%s", expected, text)
		}
	}

	parsed, err := parseContainer(content, "service")
	if err != nil {
		t.Fatalf("parseContainer returned an error: %v", err)
	}
	arguments, argumentDiagnostics := listStrings(context.Background(), parsed.Spec.Arguments)
	if argumentDiagnostics.HasError() || len(arguments) != 2 || arguments[1] != "hello world" {
		t.Fatalf("unexpected parsed arguments: %#v (%v)", arguments, argumentDiagnostics)
	}
	if len(parsed.Spec.Ports) != 1 || parsed.Spec.Ports[0].HostIP.ValueString() != "127.0.0.1" {
		t.Fatalf("unexpected parsed ports: %#v", parsed.Spec.Ports)
	}
	if len(parsed.Spec.Mounts) != 1 || !parsed.Spec.Mounts[0].ReadOnly.ValueBool() {
		t.Fatalf("unexpected parsed mounts: %#v", parsed.Spec.Mounts)
	}
}

func TestArgumentEncodingRoundTrip(t *testing.T) {
	t.Parallel()

	expected := []string{"", "plain", "space separated", `quote"and\\slash`}
	decoded, err := decodeArguments(encodeArguments(expected))
	if err != nil {
		t.Fatalf("decodeArguments returned an error: %v", err)
	}
	if len(decoded) != len(expected) {
		t.Fatalf("decoded %#v, want %#v", decoded, expected)
	}
	for index := range expected {
		if decoded[index] != expected[index] {
			t.Errorf("argument %d = %q, want %q", index, decoded[index], expected[index])
		}
	}
}
