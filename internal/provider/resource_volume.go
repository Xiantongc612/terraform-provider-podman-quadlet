package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/quadlet"
	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*volumeResource)(nil)
	_ resource.ResourceWithConfigure      = (*volumeResource)(nil)
	_ resource.ResourceWithImportState    = (*volumeResource)(nil)
	_ resource.ResourceWithValidateConfig = (*volumeResource)(nil)
)

type volumeResource struct {
	managedResource
}

type volumeResourceModel struct {
	ID        types.String    `tfsdk:"id"`
	Metadata  metadataModel   `tfsdk:"metadata"`
	Spec      volumeSpecModel `tfsdk:"spec"`
	Reference types.String    `tfsdk:"reference"`
	Status    types.Object    `tfsdk:"status"`
}

type volumeSpecModel struct {
	Driver       types.String `tfsdk:"driver"`
	Device       types.String `tfsdk:"device"`
	Type         types.String `tfsdk:"type"`
	MountOptions types.List   `tfsdk:"mount_options"`
	Copy         types.Bool   `tfsdk:"copy"`
}

func newVolumeResource() resource.Resource {
	return &volumeResource{}
}

func (r *volumeResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_volume"
}

func (r *volumeResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages a Podman Quadlet volume.",
		Attributes: map[string]schema.Attribute{
			"id":        schema.StringAttribute{Computed: true},
			"reference": schema.StringAttribute{Computed: true, Description: "Quadlet reference for containers."},
			"status":    statusAttribute(),
		},
		Blocks: map[string]schema.Block{
			"metadata": metadataBlock(),
			"spec": schema.SingleNestedBlock{
				Attributes: map[string]schema.Attribute{
					"driver":        schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("local"), Description: "Podman volume driver. Defaults to local."},
					"device":        schema.StringAttribute{Optional: true, Description: "Host device or path for the local driver."},
					"type":          schema.StringAttribute{Optional: true, Description: "Filesystem type for the local driver."},
					"mount_options": schema.ListAttribute{Optional: true, ElementType: types.StringType},
					"copy":          schema.BoolAttribute{Optional: true, Description: "Copy image content into a new volume."},
				},
			},
		},
	}
}

func (r *volumeResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.configure(req, resp)
}

func (r *volumeResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var config volumeResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(validateMetadata(config.Metadata)...)
	for label, value := range map[string]types.String{
		"driver": config.Spec.Driver,
		"device": config.Spec.Device,
		"type":   config.Spec.Type,
	} {
		if !value.IsNull() && !value.IsUnknown() && containsInvalidLine(value.ValueString()) {
			resp.Diagnostics.AddError("Invalid volume "+label, "Volume values must not contain newlines or NUL bytes.")
		}
	}
	if !config.Spec.MountOptions.IsNull() && !config.Spec.MountOptions.IsUnknown() {
		var options []string
		resp.Diagnostics.Append(config.Spec.MountOptions.ElementsAs(ctx, &options, false)...)
		for _, option := range options {
			if containsInvalidLine(option) {
				resp.Diagnostics.AddError("Invalid mount option", "Mount options must not contain newlines or NUL bytes.")
			}
		}
	}
}

func (r *volumeResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan volumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	content, diagnostics := renderVolume(ctx, &plan, r.installTarget)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	status, err := r.apply(ctx, r.filePath(name, ".volume"), name+"-volume.service", content, true)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create volume", err.Error())
		return
	}
	plan.ID = types.StringValue("volume/" + name)
	plan.Reference = types.StringValue(name + ".volume")
	plan.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state volumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "volume")
	if err != nil {
		resp.Diagnostics.AddError("Invalid volume state", err.Error())
		return
	}
	filePath := r.filePath(name, ".volume")
	content, err := r.client.ReadFile(ctx, filePath)
	if errors.Is(err, remote.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read volume", err.Error())
		return
	}
	parsed, err := parseVolume(content, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to parse volume", err.Error())
		return
	}
	parsed.ID = types.StringValue("volume/" + name)
	parsed.Reference = types.StringValue(name + ".volume")
	status, err := r.status(ctx, filePath, name+"-volume.service", content)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read volume status", err.Error())
		return
	}
	parsed.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, parsed)...)
}

func (r *volumeResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan volumeResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	content, diagnostics := renderVolume(ctx, &plan, r.installTarget)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	status, err := r.apply(ctx, r.filePath(name, ".volume"), name+"-volume.service", content, false)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update volume", err.Error())
		return
	}
	plan.ID = types.StringValue("volume/" + name)
	plan.Reference = types.StringValue(name + ".volume")
	plan.Status = statusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *volumeResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state volumeResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "volume")
	if err != nil {
		resp.Diagnostics.AddError("Invalid volume state", err.Error())
		return
	}
	if err := r.delete(ctx, r.filePath(name, ".volume"), name+"-volume.service"); err != nil {
		resp.Diagnostics.AddError("Unable to delete volume", err.Error())
	}
}

func (r *volumeResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	name, err := importedName(req.ID, "volume")
	if err != nil {
		resp.Diagnostics.AddError("Invalid volume import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("metadata"), metadataModel{
		Name:        types.StringValue(name),
		Description: types.StringNull(),
		Labels:      types.MapNull(types.StringType),
	})...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("spec"), volumeSpecModel{})...)
}

func renderVolume(ctx context.Context, model *volumeResourceModel, target string) ([]byte, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if model.Spec.Driver.IsNull() {
		model.Spec.Driver = types.StringValue("local")
	}
	labels, labelDiagnostics := mapValues(ctx, model.Metadata.Labels)
	diagnostics.Append(labelDiagnostics...)
	pairs := []quadlet.Pair{
		{Key: "VolumeName", Value: model.Metadata.Name.ValueString()},
		{Key: "Driver", Value: model.Spec.Driver.ValueString()},
	}
	pairs = append(pairs, optionalStringPairs(map[string]types.String{
		"Device": model.Spec.Device,
		"Type":   model.Spec.Type,
	})...)
	if !model.Spec.MountOptions.IsNull() && !model.Spec.MountOptions.IsUnknown() {
		var options []string
		diagnostics.Append(model.Spec.MountOptions.ElementsAs(ctx, &options, false)...)
		for _, option := range options {
			pairs = append(pairs, quadlet.Pair{Key: "Options", Value: option})
		}
	}
	pairs = append(pairs, optionalBoolPair("Copy", model.Spec.Copy)...)
	pairs = append(pairs, sortedMapPairs("Label", labels, "=")...)
	return quadlet.Render(
		unitSection(model.Metadata.Description),
		quadlet.Section{Name: "Volume", Pairs: pairs},
		installSection(target),
	), diagnostics
}

func parseVolume(content []byte, name string) (*volumeResourceModel, error) {
	sections, err := quadlet.Parse(content)
	if err != nil {
		return nil, err
	}
	unit := pairsByKey(sections, "Unit")
	volumePairs := pairsByKey(sections, "Volume")
	if remoteName := first(volumePairs, "VolumeName"); remoteName != "" && remoteName != name {
		return nil, fmt.Errorf("VolumeName %q does not match file name %q", remoteName, name)
	}
	return &volumeResourceModel{
		Metadata: metadataModel{
			Name:        types.StringValue(name),
			Description: optionalStringValue(first(unit, "Description")),
			Labels:      stringMapValue(parseKeyValue(volumePairs["Label"], "=")),
		},
		Spec: volumeSpecModel{
			Driver:       optionalStringValue(first(volumePairs, "Driver")),
			Device:       optionalStringValue(first(volumePairs, "Device")),
			Type:         optionalStringValue(first(volumePairs, "Type")),
			MountOptions: stringListValue(volumePairs["Options"]),
			Copy:         optionalBoolValue(first(volumePairs, "Copy")),
		},
	}, nil
}

func listStrings(ctx context.Context, value types.List) ([]string, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}
	var values []string
	diagnostics := value.ElementsAs(ctx, &values, false)
	return values, diagnostics
}
