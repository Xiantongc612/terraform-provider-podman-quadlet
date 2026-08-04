package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
)

func TestMetadata(t *testing.T) {
	t.Parallel()

	var resp provider.MetadataResponse
	New("test")().Metadata(context.Background(), provider.MetadataRequest{}, &resp)

	if resp.TypeName != "podlet" {
		t.Fatalf("expected provider type podlet, got %q", resp.TypeName)
	}
	if resp.Version != "test" {
		t.Fatalf("expected provider version test, got %q", resp.Version)
	}
}
