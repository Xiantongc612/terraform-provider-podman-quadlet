// Package provider implements the podlet Terraform provider.
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

const typeName = "podlet"

var _ provider.Provider = (*PodletProvider)(nil)

// PodletProvider implements provider-level configuration and registrations.
type PodletProvider struct {
	version string
}

// New returns a factory for a provider with the given release version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &PodletProvider{version: version}
	}
}

// Metadata returns the provider type name and release version.
func (p *PodletProvider) Metadata(
	_ context.Context,
	_ provider.MetadataRequest,
	resp *provider.MetadataResponse,
) {
	resp.TypeName = typeName
	resp.Version = p.version
}

// Schema defines provider-level configuration.
func (p *PodletProvider) Schema(
	_ context.Context,
	_ provider.SchemaRequest,
	resp *provider.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manage rootless Podman Quadlets on a remote machine.",
	}
}

// Configure prepares clients shared by resources.
func (p *PodletProvider) Configure(
	_ context.Context,
	_ provider.ConfigureRequest,
	_ *provider.ConfigureResponse,
) {
}

// Resources returns resources implemented by this provider.
func (p *PodletProvider) Resources(context.Context) []func() resource.Resource {
	return nil
}

// DataSources returns data sources implemented by this provider.
func (p *PodletProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
