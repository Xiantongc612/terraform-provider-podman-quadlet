package provider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/Xiantongc612/podlet-provider/internal/quadlet"
	"github.com/Xiantongc612/podlet-provider/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*networkResource)(nil)
	_ resource.ResourceWithConfigure      = (*networkResource)(nil)
	_ resource.ResourceWithImportState    = (*networkResource)(nil)
	_ resource.ResourceWithValidateConfig = (*networkResource)(nil)
)

type networkResource struct {
	managedResource
}

type networkResourceModel struct {
	ID        types.String     `tfsdk:"id"`
	Metadata  metadataModel    `tfsdk:"metadata"`
	Spec      networkSpecModel `tfsdk:"spec"`
	Reference types.String     `tfsdk:"reference"`
	Status    types.Object     `tfsdk:"status"`
}

type networkSpecModel struct {
	Driver     types.String `tfsdk:"driver"`
	Subnet     types.String `tfsdk:"subnet"`
	Gateway    types.String `tfsdk:"gateway"`
	IPRange    types.String `tfsdk:"ip_range"`
	IPv6       types.Bool   `tfsdk:"ipv6"`
	Internal   types.Bool   `tfsdk:"internal"`
	DNSEnabled types.Bool   `tfsdk:"dns_enabled"`
	Options    types.Map    `tfsdk:"options"`
}

func newNetworkResource() resource.Resource {
	return &networkResource{}
}

func (r *networkResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_network"
}

func (r *networkResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages a Podman Quadlet network.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Computed: true},
			"reference": schema.StringAttribute{Computed: true, Description: "Quadlet reference for containers."},
			"status":    statusAttribute(),
		},
		Blocks: map[string]schema.Block{
			"metadata": metadataBlock(),
			"spec": schema.SingleNestedBlock{
				Attributes: map[string]schema.Attribute{
					"driver":      schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("bridge"), Description: "Podman network driver. Defaults to bridge."},
					"subnet":      schema.StringAttribute{Optional: true},
					"gateway":     schema.StringAttribute{Optional: true},
					"ip_range":    schema.StringAttribute{Optional: true},
					"ipv6":        schema.BoolAttribute{Optional: true},
					"internal":    schema.BoolAttribute{Optional: true},
					"dns_enabled": schema.BoolAttribute{Optional: true},
					"options":     schema.MapAttribute{Optional: true, ElementType: types.StringType},
				},
			},
		},
	}
}

func (r *networkResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.configure(req, resp)
}

func (r *networkResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var config networkResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(validateMetadata(config.Metadata)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !config.Spec.Subnet.IsNull() && !config.Spec.Subnet.IsUnknown() {
		if _, _, err := net.ParseCIDR(config.Spec.Subnet.ValueString()); err != nil {
			resp.Diagnostics.AddError("Invalid network subnet", "spec.subnet must use CIDR notation.")
		}
	}
	for label, value := range map[string]types.String{
		"driver":   config.Spec.Driver,
		"gateway":  config.Spec.Gateway,
		"ip_range": config.Spec.IPRange,
	} {
		if !value.IsNull() && !value.IsUnknown() && containsInvalidLine(value.ValueString()) {
			resp.Diagnostics.AddError("Invalid network "+label, "Network values must not contain newlines or NUL bytes.")
		}
	}
	_, diagnostics := mapValues(ctx, config.Spec.Options)
	resp.Diagnostics.Append(diagnostics...)
}

func (r *networkResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan networkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	content, diagnostics := renderNetwork(ctx, &plan, r.installTarget)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	status, err := r.apply(ctx, r.filePath(name, ".network"), name+"-network.service", content, true)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create network", err.Error())
		return
	}
	plan.ID = types.StringValue("network/" + name)
	plan.Reference = types.StringValue(name + ".network")
	plan.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state networkResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "network")
	if err != nil {
		resp.Diagnostics.AddError("Invalid network state", err.Error())
		return
	}
	filePath := r.filePath(name, ".network")
	content, err := r.client.ReadFile(ctx, filePath)
	if errors.Is(err, remote.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read network", err.Error())
		return
	}
	parsed, err := parseNetwork(content, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to parse network", err.Error())
		return
	}
	parsed.ID = types.StringValue("network/" + name)
	parsed.Reference = types.StringValue(name + ".network")
	status, err := r.status(ctx, filePath, name+"-network.service", content)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read network status", err.Error())
		return
	}
	parsed.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, parsed)...)
}

func (r *networkResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan networkResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	content, diagnostics := renderNetwork(ctx, &plan, r.installTarget)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	status, err := r.apply(ctx, r.filePath(name, ".network"), name+"-network.service", content, false)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update network", err.Error())
		return
	}
	plan.ID = types.StringValue("network/" + name)
	plan.Reference = types.StringValue(name + ".network")
	plan.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *networkResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state networkResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "network")
	if err != nil {
		resp.Diagnostics.AddError("Invalid network state", err.Error())
		return
	}
	if err := r.delete(ctx, r.filePath(name, ".network"), name+"-network.service"); err != nil {
		resp.Diagnostics.AddError("Unable to delete network", err.Error())
	}
}

func (r *networkResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	name, err := importedName(req.ID, "network")
	if err != nil {
		resp.Diagnostics.AddError("Invalid network import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("metadata"), metadataModel{
		Name:        types.StringValue(name),
		Description: types.StringNull(),
		Labels:      types.MapNull(types.StringType),
	})...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("spec"), networkSpecModel{})...)
}

func renderNetwork(ctx context.Context, model *networkResourceModel, target string) ([]byte, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if model.Spec.Driver.IsNull() {
		model.Spec.Driver = types.StringValue("bridge")
	}
	labels, labelDiagnostics := mapValues(ctx, model.Metadata.Labels)
	options, optionDiagnostics := mapValues(ctx, model.Spec.Options)
	diagnostics.Append(labelDiagnostics...)
	diagnostics.Append(optionDiagnostics...)
	pairs := []quadlet.Pair{
		{Key: "NetworkName", Value: model.Metadata.Name.ValueString()},
		{Key: "Driver", Value: model.Spec.Driver.ValueString()},
	}
	pairs = append(pairs, optionalStringPairs(map[string]types.String{
		"Subnet":  model.Spec.Subnet,
		"Gateway": model.Spec.Gateway,
		"IPRange": model.Spec.IPRange,
	})...)
	pairs = append(pairs, optionalBoolPair("IPv6", model.Spec.IPv6)...)
	pairs = append(pairs, optionalBoolPair("Internal", model.Spec.Internal)...)
	if !model.Spec.DNSEnabled.IsNull() && !model.Spec.DNSEnabled.IsUnknown() {
		pairs = append(pairs, quadlet.Pair{Key: "DisableDNS", Value: strconv.FormatBool(!model.Spec.DNSEnabled.ValueBool())})
	}
	pairs = append(pairs, sortedMapPairs("Label", labels, "=")...)
	pairs = append(pairs, sortedMapPairs("Options", options, "=")...)
	return quadlet.Render(
		unitSection(model.Metadata.Description),
		quadlet.Section{Name: "Network", Pairs: pairs},
		installSection(target),
	), diagnostics
}

func parseNetwork(content []byte, name string) (*networkResourceModel, error) {
	sections, err := quadlet.Parse(content)
	if err != nil {
		return nil, err
	}
	unit := pairsByKey(sections, "Unit")
	networkPairs := pairsByKey(sections, "Network")
	if remoteName := first(networkPairs, "NetworkName"); remoteName != "" && remoteName != name {
		return nil, fmt.Errorf("NetworkName %q does not match file name %q", remoteName, name)
	}
	return &networkResourceModel{
		Metadata: metadataModel{
			Name:        types.StringValue(name),
			Description: optionalStringValue(first(unit, "Description")),
			Labels:      stringMapValue(parseKeyValue(networkPairs["Label"], "=")),
		},
		Spec: networkSpecModel{
			Driver:     optionalStringValue(first(networkPairs, "Driver")),
			Subnet:     optionalStringValue(first(networkPairs, "Subnet")),
			Gateway:    optionalStringValue(first(networkPairs, "Gateway")),
			IPRange:    optionalStringValue(first(networkPairs, "IPRange")),
			IPv6:       optionalBoolValue(first(networkPairs, "IPv6")),
			Internal:   optionalBoolValue(first(networkPairs, "Internal")),
			DNSEnabled: invertedOptionalBoolValue(first(networkPairs, "DisableDNS")),
			Options:    stringMapValue(parseKeyValue(networkPairs["Options"], "=")),
		},
	}, nil
}

func resourceName(id types.String, metadataName types.String, kind string) (string, error) {
	if !metadataName.IsNull() && !metadataName.IsUnknown() && metadataName.ValueString() != "" {
		return metadataName.ValueString(), nil
	}
	prefix := kind + "/"
	if id.IsNull() || id.IsUnknown() || !strings.HasPrefix(id.ValueString(), prefix) {
		return "", fmt.Errorf("expected import ID in the form %s<name>", prefix)
	}
	name := strings.TrimPrefix(id.ValueString(), prefix)
	if !resourceNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid resource name %q in import ID", name)
	}
	return name, nil
}

func importedName(id, kind string) (string, error) {
	prefix := kind + "/"
	if !strings.HasPrefix(id, prefix) {
		return "", fmt.Errorf("expected import ID in the form %s<name>", prefix)
	}
	name := strings.TrimPrefix(id, prefix)
	if !resourceNamePattern.MatchString(name) {
		return "", fmt.Errorf("invalid resource name %q in import ID", name)
	}
	return name, nil
}
