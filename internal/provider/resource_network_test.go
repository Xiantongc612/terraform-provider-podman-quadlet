package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestNetworkRenderAndParse(t *testing.T) {
	t.Parallel()

	model := networkResourceModel{
		Metadata: metadataModel{
			Name:        types.StringValue("frontend"),
			Description: types.StringValue("Frontend network"),
			Labels: types.MapValueMust(types.StringType, map[string]attr.Value{
				"application": types.StringValue("example"),
			}),
		},
		Spec: networkSpecModel{
			Driver:     types.StringNull(),
			Subnet:     types.StringValue("10.42.0.0/24"),
			Gateway:    types.StringValue("10.42.0.1"),
			IPRange:    types.StringNull(),
			IPv6:       types.BoolNull(),
			Internal:   types.BoolValue(true),
			DNSEnabled: types.BoolValue(false),
			Options:    types.MapNull(types.StringType),
		},
	}
	content, diagnostics := renderNetwork(context.Background(), &model)
	if diagnostics.HasError() {
		t.Fatalf("render diagnostics: %v", diagnostics)
	}
	text := string(content)
	for _, expected := range []string{
		"NetworkName=frontend",
		"Driver=bridge",
		"Internal=true",
		"DisableDNS=true",
		"Label=application=example",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rendered network does not contain %q:\n%s", expected, text)
		}
	}

	parsed, err := parseNetwork(content, "frontend")
	if err != nil {
		t.Fatalf("parseNetwork returned an error: %v", err)
	}
	if parsed.Spec.Driver.ValueString() != "bridge" || !parsed.Spec.Internal.ValueBool() {
		t.Fatalf("unexpected parsed network: %#v", parsed.Spec)
	}
	if parsed.Spec.DNSEnabled.ValueBool() {
		t.Fatal("expected DNS to be disabled")
	}
}
