// Package provider implements the podlet Terraform provider.
package provider

import (
	"context"
	"fmt"
	"path"
	"strings"
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
	Password              types.String `tfsdk:"password"`
	BecomePassword        types.String `tfsdk:"become_password"`
	KnownHostsPath        types.String `tfsdk:"known_hosts_path"`
	UseAgent              types.Bool   `tfsdk:"use_agent"`
	InsecureIgnoreHostKey types.Bool   `tfsdk:"insecure_ignore_host_key"`
	ConnectionTimeout     types.String `tfsdk:"connection_timeout"`
	QuadletDirectory      types.String `tfsdk:"quadlet_directory"`
	Mode                  types.String `tfsdk:"mode"`
	Sudo                  types.Bool   `tfsdk:"sudo"`
}

type providerData struct {
	client           remote.Client
	quadletDirectory string
	systemctlPrefix  string
	installTarget    string
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
		Description: "Manage Podman Quadlets on a remote machine.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Required:    true,
				Description: "Hostname or IP address of the remote machine.",
			},
			"user": schema.StringAttribute{
				Required:    true,
				Description: "Remote user used to connect over SSH.",
			},
			"port": schema.Int64Attribute{
				Optional:    true,
				Description: "SSH port. Defaults to 22.",
			},
			"private_key_path": schema.StringAttribute{
				Optional:    true,
				Description: "Path to an unencrypted SSH private key.",
			},
			"password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "SSH password for authentication. An alternative to an SSH agent or private key.",
			},
			"become_password": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Password for sudo elevation. Required with sudo = true when the remote user is not configured for NOPASSWD sudo. Passed to sudo via standard input and never used for SSH login.",
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
				Description: "Remote Quadlet directory. In user mode this is relative to the user's home and defaults to .config/containers/systemd; in system mode it must be absolute and defaults to /etc/containers/systemd.",
			},
			"mode": schema.StringAttribute{
				Optional:    true,
				Description: "Manage user (rootless) or system (rootful) Quadlets and systemd. Defaults to user.",
			},
			"sudo": schema.BoolAttribute{
				Optional:    true,
				Description: "Use sudo to write system Quadlets and run systemd. Requires mode=system; either NOPASSWD sudo or a become_password. Defaults to false.",
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

	mode := remote.ModeUser
	if !config.Mode.IsNull() && !config.Mode.IsUnknown() {
		mode = config.Mode.ValueString()
	}
	if mode != remote.ModeUser && mode != remote.ModeSystem {
		resp.Diagnostics.AddAttributeError(
			frameworkpath.Root("mode"),
			"Invalid mode",
			"mode must be user or system.",
		)
		return
	}
	sudo := false
	if !config.Sudo.IsNull() && !config.Sudo.IsUnknown() {
		sudo = config.Sudo.ValueBool()
	}
	if sudo && mode != remote.ModeSystem {
		resp.Diagnostics.AddAttributeError(
			frameworkpath.Root("sudo"),
			"Invalid sudo",
			"sudo is only valid when mode is system.",
		)
		return
	}
	if mode == remote.ModeSystem && !sudo && config.User.ValueString() != "root" {
		resp.Diagnostics.AddAttributeError(
			frameworkpath.Root("sudo"),
			"Root login required",
			"System mode without sudo requires root SSH login. Set sudo = true for a user with NOPASSWD sudo, or user = \"root\".",
		)
		return
	}

	quadletDirectory := ".config/containers/systemd"
	if mode == remote.ModeSystem {
		quadletDirectory = "/etc/containers/systemd"
	}
	if !config.QuadletDirectory.IsNull() && !config.QuadletDirectory.IsUnknown() {
		quadletDirectory = config.QuadletDirectory.ValueString()
	}
	if mode == remote.ModeSystem {
		if !strings.HasPrefix(quadletDirectory, "/") || path.Clean(quadletDirectory) != quadletDirectory {
			resp.Diagnostics.AddAttributeError(
				frameworkpath.Root("quadlet_directory"),
				"Invalid Quadlet directory",
				"In system mode quadlet_directory must be an absolute, clean path.",
			)
			return
		}
	} else if quadletDirectory == "" || quadletDirectory == "." || path.Clean(quadletDirectory) != quadletDirectory ||
		quadletDirectory == ".." || len(quadletDirectory) >= 3 && quadletDirectory[:3] == "../" {
		resp.Diagnostics.AddAttributeError(
			frameworkpath.Root("quadlet_directory"),
			"Invalid Quadlet directory",
			"quadlet_directory must be a clean remote path and must not traverse to a parent directory.",
		)
		return
	}

	systemctlPrefix := "systemctl --user"
	installTarget := "default.target"
	if mode == remote.ModeSystem {
		installTarget = "multi-user.target"
		systemctlPrefix = "systemctl"
	}

	privateKeyPath := ""
	if !config.PrivateKeyPath.IsNull() && !config.PrivateKeyPath.IsUnknown() {
		privateKeyPath = config.PrivateKeyPath.ValueString()
	}
	password := ""
	if !config.Password.IsNull() && !config.Password.IsUnknown() {
		password = config.Password.ValueString()
	}
	becomePassword := ""
	if !config.BecomePassword.IsNull() && !config.BecomePassword.IsUnknown() {
		becomePassword = config.BecomePassword.ValueString()
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
		Password:              password,
		BecomePassword:        becomePassword,
		KnownHostsPath:        knownHostsPath,
		UseAgent:              useAgent,
		InsecureIgnoreHostKey: insecureIgnoreHostKey,
		Timeout:               timeout,
		Mode:                  mode,
		Sudo:                  sudo,
	})
	if err != nil {
		resp.Diagnostics.AddError("Invalid SSH configuration", err.Error())
		return
	}

	data := &providerData{
		client:           client,
		quadletDirectory: quadletDirectory,
		systemctlPrefix:  systemctlPrefix,
		installTarget:    installTarget,
	}
	resp.ResourceData = data
	resp.DataSourceData = data
}

// Resources returns resources implemented by this provider.
func (p *PodletProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newContainerResource,
		newNetworkResource,
		newVolumeResource,
	}
}

// DataSources returns data sources implemented by this provider.
func (p *PodletProvider) DataSources(context.Context) []func() datasource.DataSource {
	return nil
}
