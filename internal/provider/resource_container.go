package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Xiantongc612/podlet-provider/internal/quadlet"
	"github.com/Xiantongc612/podlet-provider/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*containerResource)(nil)
	_ resource.ResourceWithConfigure      = (*containerResource)(nil)
	_ resource.ResourceWithImportState    = (*containerResource)(nil)
	_ resource.ResourceWithValidateConfig = (*containerResource)(nil)
)

type containerResource struct {
	managedResource
}

type containerResourceModel struct {
	ID       types.String       `tfsdk:"id"`
	Metadata metadataModel      `tfsdk:"metadata"`
	Spec     containerSpecModel `tfsdk:"spec"`
	Status   types.Object       `tfsdk:"status"`
}

type containerSpecModel struct {
	Image            types.String           `tfsdk:"image"`
	PullPolicy       types.String           `tfsdk:"pull_policy"`
	Command          types.List             `tfsdk:"command"`
	Arguments        types.List             `tfsdk:"arguments"`
	Environment      types.Map              `tfsdk:"environment"`
	EnvironmentFiles types.List             `tfsdk:"environment_files"`
	Ports            []containerPortModel   `tfsdk:"port"`
	Mounts           []containerMountModel  `tfsdk:"mount"`
	Networks         types.Set              `tfsdk:"networks"`
	User             types.String           `tfsdk:"user"`
	WorkingDirectory types.String           `tfsdk:"working_directory"`
	Hostname         types.String           `tfsdk:"hostname"`
	HealthCheck      *containerHealthModel  `tfsdk:"health_check"`
	Service          *containerServiceModel `tfsdk:"service"`
}

type containerPortModel struct {
	HostIP        types.String `tfsdk:"host_ip"`
	HostPort      types.Int64  `tfsdk:"host_port"`
	ContainerPort types.Int64  `tfsdk:"container_port"`
	Protocol      types.String `tfsdk:"protocol"`
}

type containerMountModel struct {
	Source   types.String `tfsdk:"source"`
	Target   types.String `tfsdk:"target"`
	ReadOnly types.Bool   `tfsdk:"read_only"`
	Options  types.List   `tfsdk:"options"`
}

type containerHealthModel struct {
	Command     types.List   `tfsdk:"command"`
	Interval    types.String `tfsdk:"interval"`
	Timeout     types.String `tfsdk:"timeout"`
	Retries     types.Int64  `tfsdk:"retries"`
	StartPeriod types.String `tfsdk:"start_period"`
}

type containerServiceModel struct {
	Restart    types.String `tfsdk:"restart"`
	RestartSec types.String `tfsdk:"restart_sec"`
}

func newContainerResource() resource.Resource {
	return &containerResource{}
}

func (r *containerResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_container"
}

func (r *containerResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages a rootless Podman Quadlet container and systemd service.",
		Attributes: map[string]schema.Attribute{
			"id":     schema.StringAttribute{Computed: true},
			"status": statusAttribute(),
		},
		Blocks: map[string]schema.Block{
			"metadata": metadataBlock(),
			"spec": schema.SingleNestedBlock{
				Attributes: map[string]schema.Attribute{
					"image":             schema.StringAttribute{Required: true},
					"pull_policy":       schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("missing"), Description: "One of always, missing, newer, or never. Defaults to missing."},
					"command":           schema.ListAttribute{Optional: true, ElementType: types.StringType},
					"arguments":         schema.ListAttribute{Optional: true, ElementType: types.StringType},
					"environment":       schema.MapAttribute{Optional: true, Sensitive: true, ElementType: types.StringType},
					"environment_files": schema.ListAttribute{Optional: true, ElementType: types.StringType},
					"networks":          schema.SetAttribute{Optional: true, ElementType: types.StringType},
					"user":              schema.StringAttribute{Optional: true},
					"working_directory": schema.StringAttribute{Optional: true},
					"hostname":          schema.StringAttribute{Optional: true},
				},
				Blocks: map[string]schema.Block{
					"port": schema.ListNestedBlock{
						NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
							"host_ip":        schema.StringAttribute{Optional: true},
							"host_port":      schema.Int64Attribute{Optional: true},
							"container_port": schema.Int64Attribute{Required: true},
							"protocol":       schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("tcp"), Description: "One of tcp, udp, or sctp. Defaults to tcp."},
						}},
					},
					"mount": schema.ListNestedBlock{
						NestedObject: schema.NestedBlockObject{Attributes: map[string]schema.Attribute{
							"source":    schema.StringAttribute{Required: true},
							"target":    schema.StringAttribute{Required: true},
							"read_only": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(false)},
							"options":   schema.ListAttribute{Optional: true, ElementType: types.StringType},
						}},
					},
					"health_check": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"command":      schema.ListAttribute{Optional: true, ElementType: types.StringType},
							"interval":     schema.StringAttribute{Optional: true},
							"timeout":      schema.StringAttribute{Optional: true},
							"retries":      schema.Int64Attribute{Optional: true},
							"start_period": schema.StringAttribute{Optional: true},
						},
					},
					"service": schema.SingleNestedBlock{
						Attributes: map[string]schema.Attribute{
							"restart":     schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("on-failure"), Description: "Systemd restart policy. Defaults to on-failure."},
							"restart_sec": schema.StringAttribute{Optional: true},
						},
					},
				},
			},
		},
	}
}

func (r *containerResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.configure(req, resp)
}

func (r *containerResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var config containerResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(validateMetadata(config.Metadata)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateContainerSpec(ctx, config.Spec, &resp.Diagnostics)
}

func (r *containerResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan containerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	content, diagnostics := renderContainer(ctx, &plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	status, err := r.apply(ctx, r.filePath(name, ".container"), name+".service", content, true)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create container", err.Error())
		return
	}
	plan.ID = types.StringValue("container/" + name)
	plan.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state containerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "container")
	if err != nil {
		resp.Diagnostics.AddError("Invalid container state", err.Error())
		return
	}
	filePath := r.filePath(name, ".container")
	content, err := r.client.ReadFile(ctx, filePath)
	if errors.Is(err, remote.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read container", err.Error())
		return
	}
	parsed, err := parseContainer(content, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to parse container", err.Error())
		return
	}
	parsed.ID = types.StringValue("container/" + name)
	status, err := r.status(ctx, filePath, name+".service", content)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read container status", err.Error())
		return
	}
	parsed.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, parsed)...)
}

func (r *containerResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan containerResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	content, diagnostics := renderContainer(ctx, &plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	status, err := r.apply(ctx, r.filePath(name, ".container"), name+".service", content, false)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update container", err.Error())
		return
	}
	plan.ID = types.StringValue("container/" + name)
	plan.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *containerResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state containerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "container")
	if err != nil {
		resp.Diagnostics.AddError("Invalid container state", err.Error())
		return
	}
	if err := r.delete(ctx, r.filePath(name, ".container"), name+".service"); err != nil {
		resp.Diagnostics.AddError("Unable to delete container", err.Error())
	}
}

func (r *containerResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	name, err := importedName(req.ID, "container")
	if err != nil {
		resp.Diagnostics.AddError("Invalid container import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("metadata"), metadataModel{
		Name:        types.StringValue(name),
		Description: types.StringNull(),
		Labels:      types.MapNull(types.StringType),
	})...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("spec"), emptyContainerSpec())...)
}

func renderContainer(ctx context.Context, model *containerResourceModel) ([]byte, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if model.Spec.PullPolicy.IsNull() {
		model.Spec.PullPolicy = types.StringValue("missing")
	}
	labels, labelDiagnostics := mapValues(ctx, model.Metadata.Labels)
	environment, environmentDiagnostics := mapValues(ctx, model.Spec.Environment)
	command, commandDiagnostics := listStrings(ctx, model.Spec.Command)
	arguments, argumentDiagnostics := listStrings(ctx, model.Spec.Arguments)
	environmentFiles, fileDiagnostics := listStrings(ctx, model.Spec.EnvironmentFiles)
	networks, networkDiagnostics := setStrings(ctx, model.Spec.Networks)
	diagnostics.Append(labelDiagnostics...)
	diagnostics.Append(environmentDiagnostics...)
	diagnostics.Append(commandDiagnostics...)
	diagnostics.Append(argumentDiagnostics...)
	diagnostics.Append(fileDiagnostics...)
	diagnostics.Append(networkDiagnostics...)

	pairs := []quadlet.Pair{
		{Key: "Image", Value: model.Spec.Image.ValueString()},
		{Key: "ContainerName", Value: model.Metadata.Name.ValueString()},
		{Key: "Pull", Value: model.Spec.PullPolicy.ValueString()},
	}
	if len(command) > 0 {
		pairs = append(pairs, quadlet.Pair{Key: "Entrypoint", Value: encodeArguments(command)})
	}
	if len(arguments) > 0 {
		pairs = append(pairs, quadlet.Pair{Key: "Exec", Value: encodeArguments(arguments)})
	}
	pairs = append(pairs, sortedMapPairsEncoded("Environment", environment)...)
	for _, file := range environmentFiles {
		pairs = append(pairs, quadlet.Pair{Key: "EnvironmentFile", Value: encodeToken(file)})
	}
	for index := range model.Spec.Ports {
		if model.Spec.Ports[index].Protocol.IsNull() {
			model.Spec.Ports[index].Protocol = types.StringValue("tcp")
		}
		pairs = append(pairs, quadlet.Pair{Key: "PublishPort", Value: renderPort(model.Spec.Ports[index])})
	}
	for index := range model.Spec.Mounts {
		if model.Spec.Mounts[index].ReadOnly.IsNull() {
			model.Spec.Mounts[index].ReadOnly = types.BoolValue(false)
		}
		value, mountDiagnostics := renderMount(ctx, model.Spec.Mounts[index])
		diagnostics.Append(mountDiagnostics...)
		pairs = append(pairs, quadlet.Pair{Key: "Volume", Value: value})
	}
	for _, network := range networks {
		pairs = append(pairs, quadlet.Pair{Key: "Network", Value: network})
	}
	pairs = append(pairs, optionalStringPairs(map[string]types.String{
		"HostName":   model.Spec.Hostname,
		"User":       model.Spec.User,
		"WorkingDir": model.Spec.WorkingDirectory,
	})...)
	pairs = append(pairs, sortedMapPairs("Label", labels, "=")...)
	if model.Spec.HealthCheck != nil {
		healthCommand, healthDiagnostics := listStrings(ctx, model.Spec.HealthCheck.Command)
		diagnostics.Append(healthDiagnostics...)
		pairs = append(pairs, quadlet.Pair{Key: "HealthCmd", Value: encodeArguments(healthCommand)})
		pairs = append(pairs, optionalStringPairs(map[string]types.String{
			"HealthInterval":    model.Spec.HealthCheck.Interval,
			"HealthStartPeriod": model.Spec.HealthCheck.StartPeriod,
			"HealthTimeout":     model.Spec.HealthCheck.Timeout,
		})...)
		if !model.Spec.HealthCheck.Retries.IsNull() && !model.Spec.HealthCheck.Retries.IsUnknown() {
			pairs = append(pairs, quadlet.Pair{Key: "HealthRetries", Value: strconv.FormatInt(model.Spec.HealthCheck.Retries.ValueInt64(), 10)})
		}
	}
	sections := []quadlet.Section{
		unitSection(model.Metadata.Description),
		{Name: "Container", Pairs: pairs},
	}
	if model.Spec.Service != nil {
		sections = append(sections, quadlet.Section{
			Name: "Service",
			Pairs: optionalStringPairs(map[string]types.String{
				"Restart":    model.Spec.Service.Restart,
				"RestartSec": model.Spec.Service.RestartSec,
			}),
		})
	}
	sections = append(sections, installSection())
	return quadlet.Render(sections...), diagnostics
}

func parseContainer(content []byte, name string) (*containerResourceModel, error) {
	sections, err := quadlet.Parse(content)
	if err != nil {
		return nil, err
	}
	unit := pairsByKey(sections, "Unit")
	containerPairs := pairsByKey(sections, "Container")
	servicePairs := pairsByKey(sections, "Service")
	if remoteName := first(containerPairs, "ContainerName"); remoteName != "" && remoteName != name {
		return nil, fmt.Errorf("ContainerName %q does not match file name %q", remoteName, name)
	}
	command, err := decodeArguments(first(containerPairs, "Entrypoint"))
	if err != nil {
		return nil, fmt.Errorf("parse Entrypoint: %w", err)
	}
	arguments, err := decodeArguments(first(containerPairs, "Exec"))
	if err != nil {
		return nil, fmt.Errorf("parse Exec: %w", err)
	}
	healthCommand, err := decodeArguments(first(containerPairs, "HealthCmd"))
	if err != nil {
		return nil, fmt.Errorf("parse HealthCmd: %w", err)
	}
	ports := make([]containerPortModel, 0, len(containerPairs["PublishPort"]))
	for _, value := range containerPairs["PublishPort"] {
		port, parseErr := parsePort(value)
		if parseErr != nil {
			return nil, parseErr
		}
		ports = append(ports, port)
	}
	mounts := make([]containerMountModel, 0, len(containerPairs["Volume"]))
	for _, value := range containerPairs["Volume"] {
		mount, parseErr := parseMount(value)
		if parseErr != nil {
			return nil, parseErr
		}
		mounts = append(mounts, mount)
	}
	environment := make(map[string]string)
	for _, encoded := range containerPairs["Environment"] {
		value, decodeErr := decodeToken(encoded)
		if decodeErr != nil {
			return nil, fmt.Errorf("parse Environment: %w", decodeErr)
		}
		key, itemValue, found := strings.Cut(value, "=")
		if found {
			environment[key] = itemValue
		}
	}
	environmentFiles := make([]string, 0, len(containerPairs["EnvironmentFile"]))
	for _, encoded := range containerPairs["EnvironmentFile"] {
		value, decodeErr := decodeToken(encoded)
		if decodeErr != nil {
			return nil, fmt.Errorf("parse EnvironmentFile: %w", decodeErr)
		}
		environmentFiles = append(environmentFiles, value)
	}
	return &containerResourceModel{
		Metadata: metadataModel{
			Name:        types.StringValue(name),
			Description: optionalStringValue(first(unit, "Description")),
			Labels:      stringMapValue(parseKeyValue(containerPairs["Label"], "=")),
		},
		Spec: containerSpecModel{
			Image:            optionalStringValue(first(containerPairs, "Image")),
			PullPolicy:       optionalStringValue(first(containerPairs, "Pull")),
			Command:          stringListValue(command),
			Arguments:        stringListValue(arguments),
			Environment:      stringMapValue(environment),
			EnvironmentFiles: stringListValue(environmentFiles),
			Ports:            ports,
			Mounts:           mounts,
			Networks:         stringSetValue(containerPairs["Network"]),
			User:             optionalStringValue(first(containerPairs, "User")),
			WorkingDirectory: optionalStringValue(first(containerPairs, "WorkingDir")),
			Hostname:         optionalStringValue(first(containerPairs, "HostName")),
			HealthCheck:      parsedHealthCheck(containerPairs, healthCommand),
			Service:          parsedService(servicePairs),
		},
	}, nil
}

func validateContainerSpec(ctx context.Context, spec containerSpecModel, diagnostics *diag.Diagnostics) {
	if spec.Image.IsNull() || !spec.Image.IsUnknown() && containsInvalidLine(spec.Image.ValueString()) {
		diagnostics.AddError("Invalid container image", "spec.image is required and must not contain newlines or NUL bytes.")
	}
	if !spec.PullPolicy.IsNull() && !spec.PullPolicy.IsUnknown() &&
		!oneOf(spec.PullPolicy.ValueString(), "always", "missing", "newer", "never") {
		diagnostics.AddError("Invalid pull policy", "spec.pull_policy must be always, missing, newer, or never.")
	}
	for _, port := range spec.Ports {
		if !port.ContainerPort.IsUnknown() && (port.ContainerPort.IsNull() || port.ContainerPort.ValueInt64() < 1 || port.ContainerPort.ValueInt64() > 65535) {
			diagnostics.AddError("Invalid container port", "container_port must be between 1 and 65535.")
		}
		if !port.HostPort.IsNull() && !port.HostPort.IsUnknown() && (port.HostPort.ValueInt64() < 1 || port.HostPort.ValueInt64() > 65535) {
			diagnostics.AddError("Invalid host port", "host_port must be between 1 and 65535.")
		}
		if !port.HostIP.IsNull() && !port.HostIP.IsUnknown() && net.ParseIP(port.HostIP.ValueString()) == nil {
			diagnostics.AddError("Invalid host IP", "host_ip must be a valid IPv4 or IPv6 address.")
		}
		if !port.Protocol.IsNull() && !port.Protocol.IsUnknown() && !oneOf(port.Protocol.ValueString(), "tcp", "udp", "sctp") {
			diagnostics.AddError("Invalid port protocol", "protocol must be tcp, udp, or sctp.")
		}
	}
	for _, mount := range spec.Mounts {
		if !mount.Source.IsUnknown() && (mount.Source.IsNull() || containsInvalidLine(mount.Source.ValueString())) {
			diagnostics.AddError("Invalid mount source", "mount.source is required and must be a single line.")
		}
		if !mount.Target.IsUnknown() && (mount.Target.IsNull() || !strings.HasPrefix(mount.Target.ValueString(), "/") || containsInvalidLine(mount.Target.ValueString())) {
			diagnostics.AddError("Invalid mount target", "mount.target must be an absolute container path on one line.")
		}
	}
	for label, value := range map[string]types.String{
		"user": spec.User, "working_directory": spec.WorkingDirectory, "hostname": spec.Hostname,
	} {
		if !value.IsNull() && !value.IsUnknown() && containsInvalidLine(value.ValueString()) {
			diagnostics.AddError("Invalid container "+label, "Container values must not contain newlines or NUL bytes.")
		}
	}
	environment, environmentDiagnostics := mapValues(ctx, spec.Environment)
	diagnostics.Append(environmentDiagnostics...)
	for key, value := range environment {
		if !environmentNamePattern.MatchString(key) || containsInvalidLine(value) {
			diagnostics.AddError("Invalid environment variable", "Environment names may contain letters, digits, and underscores; values must be a single line.")
		}
	}
	validateDuration := func(label string, value types.String) {
		if !value.IsNull() && !value.IsUnknown() {
			if duration, err := time.ParseDuration(value.ValueString()); err != nil || duration <= 0 {
				diagnostics.AddError("Invalid "+label, label+" must be a positive Go duration such as 30s.")
			}
		}
	}
	if spec.HealthCheck != nil {
		if spec.HealthCheck.Command.IsNull() {
			diagnostics.AddError("Missing health command", "health_check.command is required when the health_check block is configured.")
		}
		validateDuration("health interval", spec.HealthCheck.Interval)
		validateDuration("health timeout", spec.HealthCheck.Timeout)
		validateDuration("health start period", spec.HealthCheck.StartPeriod)
		if !spec.HealthCheck.Retries.IsNull() && !spec.HealthCheck.Retries.IsUnknown() && spec.HealthCheck.Retries.ValueInt64() < 1 {
			diagnostics.AddError("Invalid health retries", "health_check.retries must be greater than zero.")
		}
	}
	if spec.Service != nil {
		validateDuration("restart delay", spec.Service.RestartSec)
		if !spec.Service.Restart.IsNull() && !spec.Service.Restart.IsUnknown() && !oneOf(
			spec.Service.Restart.ValueString(),
			"no", "on-success", "on-failure", "on-abnormal", "on-watchdog", "on-abort", "always",
		) {
			diagnostics.AddError("Invalid restart policy", "service.restart is not a supported systemd restart policy.")
		}
	}
}

func renderPort(port containerPortModel) string {
	containerPort := strconv.FormatInt(port.ContainerPort.ValueInt64(), 10)
	value := containerPort
	if !port.HostPort.IsNull() {
		hostPort := strconv.FormatInt(port.HostPort.ValueInt64(), 10)
		value = hostPort + ":" + containerPort
		if !port.HostIP.IsNull() && port.HostIP.ValueString() != "" {
			value = net.JoinHostPort(port.HostIP.ValueString(), hostPort) + ":" + containerPort
		}
	}
	return value + "/" + port.Protocol.ValueString()
}

func parsePort(value string) (containerPortModel, error) {
	address, protocol, found := strings.Cut(value, "/")
	if !found {
		protocol = "tcp"
	}
	parts := strings.Split(address, ":")
	port := containerPortModel{HostIP: types.StringNull(), HostPort: types.Int64Null(), Protocol: types.StringValue(protocol)}
	containerText := parts[len(parts)-1]
	if len(parts) >= 2 {
		hostText := parts[len(parts)-2]
		hostPort, err := strconv.ParseInt(strings.Trim(hostText, "[]"), 10, 64)
		if err != nil {
			return port, fmt.Errorf("parse published host port %q: %w", value, err)
		}
		port.HostPort = types.Int64Value(hostPort)
	}
	if len(parts) > 2 {
		hostIP := strings.Join(parts[:len(parts)-2], ":")
		port.HostIP = types.StringValue(strings.Trim(hostIP, "[]"))
	}
	containerPort, err := strconv.ParseInt(containerText, 10, 64)
	if err != nil {
		return port, fmt.Errorf("parse published container port %q: %w", value, err)
	}
	port.ContainerPort = types.Int64Value(containerPort)
	return port, nil
}

func renderMount(ctx context.Context, mount containerMountModel) (string, diag.Diagnostics) {
	options, diagnostics := listStrings(ctx, mount.Options)
	if !mount.ReadOnly.IsNull() && mount.ReadOnly.ValueBool() && !contains(options, "ro") {
		options = append(options, "ro")
	}
	value := mount.Source.ValueString() + ":" + mount.Target.ValueString()
	if len(options) > 0 {
		value += ":" + strings.Join(options, ",")
	}
	return value, diagnostics
}

func parseMount(value string) (containerMountModel, error) {
	parts := strings.SplitN(value, ":", 3)
	if len(parts) < 2 {
		return containerMountModel{}, fmt.Errorf("invalid Volume value %q", value)
	}
	options := []string(nil)
	readOnly := false
	if len(parts) == 3 && parts[2] != "" {
		for _, option := range strings.Split(parts[2], ",") {
			if option == "ro" {
				readOnly = true
				continue
			}
			options = append(options, option)
		}
	}
	return containerMountModel{
		Source:   types.StringValue(parts[0]),
		Target:   types.StringValue(parts[1]),
		ReadOnly: types.BoolValue(readOnly),
		Options:  stringListValue(options),
	}, nil
}

func encodeArguments(values []string) string {
	encoded := make([]string, 0, len(values))
	for _, value := range values {
		encoded = append(encoded, encodeToken(value))
	}
	return strings.Join(encoded, " ")
}

func decodeArguments(value string) ([]string, error) {
	if value == "" {
		return nil, nil
	}
	var result []string
	for index := 0; index < len(value); {
		for index < len(value) && value[index] == ' ' {
			index++
		}
		if index >= len(value) {
			break
		}
		if value[index] != '"' {
			return nil, fmt.Errorf("expected quoted argument at byte %d", index)
		}
		start := index
		index++
		escaped := false
		for index < len(value) {
			character := value[index]
			index++
			if escaped {
				escaped = false
				continue
			}
			if character == '\\' {
				escaped = true
				continue
			}
			if character == '"' {
				break
			}
		}
		decoded, err := strconv.Unquote(value[start:index])
		if err != nil {
			return nil, err
		}
		result = append(result, decoded)
	}
	return result, nil
}

func encodeToken(value string) string {
	return strconv.Quote(value)
}

func decodeToken(value string) (string, error) {
	return strconv.Unquote(value)
}

func sortedMapPairsEncoded(key string, values map[string]string) []quadlet.Pair {
	pairs := sortedMapPairs(key, values, "=")
	for index := range pairs {
		pairs[index].Value = encodeToken(pairs[index].Value)
	}
	return pairs
}

func setStrings(ctx context.Context, value types.Set) ([]string, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}
	var values []string
	diagnostics := value.ElementsAs(ctx, &values, false)
	sort.Strings(values)
	return values, diagnostics
}

func stringSetValue(values []string) types.Set {
	if len(values) == 0 {
		return types.SetNull(types.StringType)
	}
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	return types.SetValueMust(types.StringType, elements)
}

func optionalInt64Value(value string) types.Int64 {
	if value == "" {
		return types.Int64Null()
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return types.Int64Null()
	}
	return types.Int64Value(parsed)
}

func emptyContainerSpec() containerSpecModel {
	return containerSpecModel{
		Image:            types.StringNull(),
		PullPolicy:       types.StringNull(),
		Command:          types.ListNull(types.StringType),
		Arguments:        types.ListNull(types.StringType),
		Environment:      types.MapNull(types.StringType),
		EnvironmentFiles: types.ListNull(types.StringType),
		Networks:         types.SetNull(types.StringType),
		User:             types.StringNull(),
		WorkingDirectory: types.StringNull(),
		Hostname:         types.StringNull(),
		HealthCheck:      nil,
		Service:          nil,
	}
}

func parsedHealthCheck(values map[string][]string, command []string) *containerHealthModel {
	if len(command) == 0 {
		return nil
	}
	return &containerHealthModel{
		Command:     stringListValue(command),
		Interval:    optionalStringValue(first(values, "HealthInterval")),
		Timeout:     optionalStringValue(first(values, "HealthTimeout")),
		Retries:     optionalInt64Value(first(values, "HealthRetries")),
		StartPeriod: optionalStringValue(first(values, "HealthStartPeriod")),
	}
}

func parsedService(values map[string][]string) *containerServiceModel {
	if first(values, "Restart") == "" && first(values, "RestartSec") == "" {
		return nil
	}
	return &containerServiceModel{
		Restart:    optionalStringValue(first(values, "Restart")),
		RestartSec: optionalStringValue(first(values, "RestartSec")),
	}
}

func oneOf(value string, allowed ...string) bool {
	return contains(allowed, value)
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
