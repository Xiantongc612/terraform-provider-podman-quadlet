package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
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

func TestSchemasAreValid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var providerResp provider.SchemaResponse
	New("test")().Schema(ctx, provider.SchemaRequest{}, &providerResp)
	if diagnostics := providerResp.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
		t.Fatalf("invalid provider schema: %v", diagnostics)
	}

	resources := []resource.Resource{newNetworkResource(), newVolumeResource()}
	for _, providerResource := range resources {
		var metadataResp resource.MetadataResponse
		providerResource.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "podlet"}, &metadataResp)
		var schemaResp resource.SchemaResponse
		providerResource.Schema(ctx, resource.SchemaRequest{}, &schemaResp)
		if diagnostics := schemaResp.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
			t.Errorf("invalid %s schema: %v", metadataResp.TypeName, diagnostics)
		}
	}
}
