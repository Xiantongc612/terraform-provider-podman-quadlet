package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestVolumeRenderAndParse(t *testing.T) {
	t.Parallel()

	model := volumeResourceModel{
		Metadata: metadataModel{
			Name:        types.StringValue("service-data"),
			Description: types.StringNull(),
			Labels:      types.MapNull(types.StringType),
		},
		Spec: volumeSpecModel{
			Driver:       types.StringNull(),
			Device:       types.StringValue("/srv/service"),
			Type:         types.StringValue("none"),
			MountOptions: stringListValue([]string{"bind", "nodev"}),
			Copy:         types.BoolValue(false),
		},
	}
	content, diagnostics := renderVolume(context.Background(), &model, "default.target")
	if diagnostics.HasError() {
		t.Fatalf("render diagnostics: %v", diagnostics)
	}
	text := string(content)
	for _, expected := range []string{
		"VolumeName=service-data",
		"Driver=local",
		"Device=/srv/service",
		"Options=bind",
		"Options=nodev",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("rendered volume does not contain %q:\n%s", expected, text)
		}
	}

	parsed, err := parseVolume(content, "service-data")
	if err != nil {
		t.Fatalf("parseVolume returned an error: %v", err)
	}
	options, diagnostics := listStrings(context.Background(), parsed.Spec.MountOptions)
	if diagnostics.HasError() || len(options) != 2 {
		t.Fatalf("unexpected parsed mount options: %#v (%v)", options, diagnostics)
	}
}
