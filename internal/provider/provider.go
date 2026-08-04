// Package provider implements the podlet Terraform provider.
package provider

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/Xiantongc612/podlet-provider/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	frameworkpath "github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const typeName = "podlet"

var _ provider.Provider = (*PodletProvider)(nil)

// PodletProvider implements provider-level configuration and registrations.
type PodletProvider struct {
	version string
}

type providerModel struct {
	Host                  types.String `tfsdk:"host"`
	User                  types.String `tfsdk:"user"`
	Port                  types.Int64  `tfsdk:"port"`
	PrivateKeyPath        types.String `tfsdk:"private_key_path"`
	KnownHostsPath        types.String `tfsdk:"known_hosts_path"`
	UseAgent              types.Bool   `tfsdk:"use_agent"`
	InsecureIgnoreHostKey types.Bool   `tfsdk:"insecure_ignore_host_key"`
	ConnectionTimeout     types.String `tfsdk:"connection_timeout"`
	QuadletDirectory      types.String `tfsdk:"quadlet_directory"`
}

type providerData struct {
	client           remote.Client
	quadletDirectory string
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
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:    true,
				Description: "Hostname or IP address of the remote machine.",
			},
			"user": schema.StringAttribute{
				Required:    true,
				Description: "Remote user that owns the rootless Podman services.",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "SSH port. Defaults to 22.",
			},
			"private_key_path": schema.StringAttribute{
				Optional:    true,
				Description: "Path to an unencrypted SSH private key.",
			},
			"known_hosts_path": schema.StringAttribute{
				Optional:    true,
				Description: "Path to an OpenSSH known_hosts file. Defaults to ~/.ssh/known_hosts.",
			},
			"use_agent": schema.BoolAttribute{
				Optional:    true,
				Description: "Use SSH_AUTH_SOCK for authentication. Defaults to true.",
			},
			"insecure_ignore_host_key": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable SSH host-key verification. Defaults to false.",
			},
			"connection_timeout": schema.StringAttribute{
				Optional:    true,
				Description: "SSH connection timeout as a Go duration. Defaults to 30s.",
			},
			"quadlet_directory": schema.StringAttribute{
				Optional:    true,
				Description: "Remote Quadlet directory relative to the user's home. Defaults to .config/containers/systemd.",
			},
		},
	}
}

// Configure prepares clients shared by resources.
func (p *PodletProvider) Configure(
	ctx context.Context,
	req provider.ConfigureRequest,
	resp *provider.ConfigureResponse,
) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.Host.IsUnknown() || config.User.IsUnknown() {
		resp.Diagnostics.AddError(
			"Unknown SSH configuration",
			"The host and user values must be known while configuring the provider.",
		)
		return
	}

	port := int64(22)
	if !config.Port.IsNull() && !config.Port.IsUnknown() {
		port = config.Port.ValueInt64()
	}
	useAgent := true
	if !config.UseAgent.IsNull() && !config.UseAgent.IsUnknown() {
		useAgent = config.UseAgent.ValueBool()
	}
	insecureIgnoreHostKey := false
	if !config.InsecureIgnoreHostKey.IsNull() && !config.InsecureIgnoreHostKey.IsUnknown() {
		insecureIgnoreHostKey = config.InsecureIgnoreHostKey.ValueBool()
	}
	timeoutText := "30s"
	if !config.ConnectionTimeout.IsNull() && !config.ConnectionTimeout.IsUnknown() {
		timeoutText = config.ConnectionTimeout.ValueString()
	}
	timeout, err := time.ParseDuration(timeoutText)
	if err != nil {
		resp.Diagnostics.AddAttributeError(
			frameworkpath.Root("connection_timeout"),
			"Invalid connection timeout",
			fmt.Sprintf("connection_timeout must be a Go duration: %s", err),
		)
		return
	}
	quadletDirectory := ".config/containers/systemd"
	if !config.QuadletDirectory.IsNull() && !config.QuadletDirectory.IsUnknown() {
		quadletDirectory = config.QuadletDirectory.ValueString()
	}
	if quadletDirectory == "" || quadletDirectory == "." || path.Clean(quadletDirectory) != quadletDirectory ||
		quadletDirectory == ".." || len(quadletDirectory) >= 3 && quadletDirectory[:3] == "../" {
		resp.Diagnostics.AddAttributeError(
			frameworkpath.Root("quadlet_directory"),
			"Invalid Quadlet directory",
			"quadlet_directory must be a clean remote path and must not traverse to a parent directory.",
		)
		return
	}
	privateKeyPath := ""
	if !config.PrivateKeyPath.IsNull() && !config.PrivateKeyPath.IsUnknown() {
		privateKeyPath = config.PrivateKeyPath.ValueString()
	}
	knownHostsPath := "~/.ssh/known_hosts"
	if !config.KnownHostsPath.IsNull() && !config.KnownHostsPath.IsUnknown() {
		knownHostsPath = config.KnownHostsPath.ValueString()
	}

	client, err := remote.NewSSHClient(remote.SSHConfig{
		Host:                  config.Host.ValueString(),
		User:                  config.User.ValueString(),
		Port:                  int(port),
		PrivateKeyPath:        privateKeyPath,
		KnownHostsPath:        knownHostsPath,
		UseAgent:              useAgent,
		InsecureIgnoreHostKey: insecureIgnoreHostKey,
		Timeout:               timeout,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid SSH configuration", err.Error())
		return
	}

	data := &providerData{client: client, quadletDirectory: quadletDirectory}
	resp.ResourceData = data
	resp.DataSourceData = data
}

// Resources returns resources implemented by this provider.
func (p *PodletProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newNetworkResource,
		newVolumeResource,
	}
}

// DataSources returns data sources implemented by this provider.
func (p *PodletProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
