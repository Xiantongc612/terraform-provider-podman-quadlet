package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const maxSecretBytes = 512 * 1024

var (
	_ resource.Resource                   = (*secretResource)(nil)
	_ resource.ResourceWithConfigure      = (*secretResource)(nil)
	_ resource.ResourceWithValidateConfig = (*secretResource)(nil)
)

type secretResource struct {
	managedResource
}

type secretResourceModel struct {
	ID       types.String    `tfsdk:"id"`
	Metadata metadataModel   `tfsdk:"metadata"`
	Spec     secretSpecModel `tfsdk:"spec"`
	Status   types.Object    `tfsdk:"status"`
}

type secretSpecModel struct {
	Value      types.String `tfsdk:"value"`
	Driver     types.String `tfsdk:"driver"`
	DriverOpts types.Map    `tfsdk:"driver_opts"`
}

type secretStatusModel struct {
	ID        types.String `tfsdk:"id"`
	CreatedAt types.String `tfsdk:"created_at"`
	Driver    types.String `tfsdk:"driver"`
}

type secretInspect struct {
	ID        string `json:"ID"`
	CreatedAt string `json:"CreatedAt"`
	Spec      struct {
		Name   string `json:"Name"`
		Driver struct {
			Name    string            `json:"Name"`
			Options map[string]string `json:"Options"`
		} `json:"Driver"`
		Labels map[string]string `json:"Labels"`
	} `json:"Spec"`
}

func newSecretResource() resource.Resource {
	return &secretResource{}
}

func (r *secretResource) Metadata(
	_ context.Context,
	req resource.MetadataRequest,
	resp *resource.MetadataResponse,
) {
	resp.TypeName = req.ProviderTypeName + "_secret"
}

func (r *secretResource) Schema(
	_ context.Context,
	_ resource.SchemaRequest,
	resp *resource.SchemaResponse,
) {
	resp.Schema = schema.Schema{
		Description: "Manages a Podman secret holding a sensitive value.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true},
			"status": schema.SingleNestedAttribute{
				Computed:    true,
				Description: "Observed remote secret metadata.",
				Attributes: map[string]schema.Attribute{
					"id":         schema.StringAttribute{Computed: true},
					"created_at": schema.StringAttribute{Computed: true},
					"driver":     schema.StringAttribute{Computed: true},
				},
			},
		},
		Blocks: map[string]schema.Block{
			"metadata": metadataBlock(),
			"spec": schema.SingleNestedBlock{
				Attributes: map[string]schema.Attribute{
					"value": schema.StringAttribute{
						Required:    true,
						Sensitive:   true,
						Description: "Secret value. Stored by Podman and never read back; changes recreate the secret.",
					},
					"driver": schema.StringAttribute{
						Optional:    true,
						Computed:    true,
						Default:     stringdefault.StaticString("file"),
						Description: "Podman secret driver. Defaults to file.",
					},
					"driver_opts": schema.MapAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Options passed to the secret driver.",
					},
				},
			},
		},
	}
}

func (r *secretResource) Configure(
	_ context.Context,
	req resource.ConfigureRequest,
	resp *resource.ConfigureResponse,
) {
	r.configure(req, resp)
}

func (r *secretResource) ValidateConfig(
	ctx context.Context,
	req resource.ValidateConfigRequest,
	resp *resource.ValidateConfigResponse,
) {
	var config secretResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	resp.Diagnostics.Append(validateMetadata(config.Metadata)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateSecretSpec(ctx, config.Spec, &resp.Diagnostics)
}

func validateSecretSpec(ctx context.Context, spec secretSpecModel, diagnostics *diag.Diagnostics) {
	if spec.Value.IsNull() || spec.Value.IsUnknown() {
		diagnostics.AddError("Missing secret value", "spec.value is required.")
		return
	}
	if len(spec.Value.ValueString()) > maxSecretBytes {
		diagnostics.AddError("Invalid secret value", fmt.Sprintf("spec.value must be at most %d bytes.", maxSecretBytes))
	}
	if !spec.Driver.IsNull() && !spec.Driver.IsUnknown() && containsInvalidLine(spec.Driver.ValueString()) {
		diagnostics.AddError("Invalid secret driver", "spec.driver must be a single line.")
	}
	driverOpts, driverOptDiagnostics := mapValues(ctx, spec.DriverOpts)
	diagnostics.Append(driverOptDiagnostics...)
	for key, value := range driverOpts {
		if containsInvalidLine(key) || containsInvalidLine(value) {
			diagnostics.AddError("Invalid secret driver option", "driver option names and values must be single lines.")
		}
	}
}

func (r *secretResource) Create(
	ctx context.Context,
	req resource.CreateRequest,
	resp *resource.CreateResponse,
) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	command, diagnostics := renderSecretCreate(ctx, &plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.RunWithInput(ctx, command, []byte(plan.Spec.Value.ValueString())); err != nil {
		resp.Diagnostics.AddError("Unable to create secret", err.Error())
		return
	}
	status, _, err := r.inspectSecret(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read created secret", err.Error())
		return
	}
	plan.ID = types.StringValue("secret/" + name)
	plan.Status = secretStatusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Read(
	ctx context.Context,
	req resource.ReadRequest,
	resp *resource.ReadResponse,
) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "secret")
	if err != nil {
		resp.Diagnostics.AddError("Invalid secret state", err.Error())
		return
	}
	status, report, err := r.inspectSecret(ctx, name)
	if errors.Is(err, remote.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read secret", err.Error())
		return
	}
	state.Status = secretStatusObject(status)
	state.Metadata.Labels = stringMapValue(report.Spec.Labels)
	state.Spec.Driver = optionalStringValue(report.Spec.Driver.Name)
	state.Spec.DriverOpts = stringMapValue(report.Spec.Driver.Options)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *secretResource) Update(
	ctx context.Context,
	req resource.UpdateRequest,
	resp *resource.UpdateResponse,
) {
	var plan secretResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name := plan.Metadata.Name.ValueString()
	command, diagnostics := renderSecretCreate(ctx, &plan)
	resp.Diagnostics.Append(diagnostics...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.client.Run(ctx, "podman secret rm "+remote.ShellQuote(name)); err != nil && !isSecretNotFound(err) {
		resp.Diagnostics.AddError("Unable to update secret", "remove existing secret: "+err.Error())
		return
	}
	if _, err := r.client.RunWithInput(ctx, command, []byte(plan.Spec.Value.ValueString())); err != nil {
		resp.Diagnostics.AddError("Unable to update secret", err.Error())
		return
	}
	status, _, err := r.inspectSecret(ctx, name)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read updated secret", err.Error())
		return
	}
	plan.ID = types.StringValue("secret/" + name)
	plan.Status = secretStatusObject(status)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *secretResource) Delete(
	ctx context.Context,
	req resource.DeleteRequest,
	resp *resource.DeleteResponse,
) {
	var state secretResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	name, err := resourceName(state.ID, state.Metadata.Name, "secret")
	if err != nil {
		resp.Diagnostics.AddError("Invalid secret state", err.Error())
		return
	}
	if _, err := r.client.Run(ctx, "podman secret rm "+remote.ShellQuote(name)); err != nil && !isSecretNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete secret", err.Error())
	}
}

func renderSecretCreate(ctx context.Context, model *secretResourceModel) (string, diag.Diagnostics) {
	var diagnostics diag.Diagnostics
	if model.Spec.Driver.IsNull() {
		model.Spec.Driver = types.StringValue("file")
	}
	driverOpts, driverOptDiagnostics := mapValues(ctx, model.Spec.DriverOpts)
	labels, labelDiagnostics := mapValues(ctx, model.Metadata.Labels)
	diagnostics.Append(driverOptDiagnostics...)
	diagnostics.Append(labelDiagnostics...)
	if diagnostics.HasError() {
		return "", diagnostics
	}

	args := []string{"podman", "secret", "create", "--driver", model.Spec.Driver.ValueString()}
	for _, key := range sortedKeys(driverOpts) {
		args = append(args, "--driver-opts", key+"="+driverOpts[key])
	}
	for _, key := range sortedKeys(labels) {
		args = append(args, "--label", key+"="+labels[key])
	}
	args = append(args, model.Metadata.Name.ValueString(), "-")
	for index := range args {
		args[index] = remote.ShellQuote(args[index])
	}
	return strings.Join(args, " "), diagnostics
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (r *secretResource) inspectSecret(ctx context.Context, name string) (secretStatusModel, secretInspect, error) {
	output, err := r.client.Run(ctx, "podman secret inspect "+remote.ShellQuote(name))
	if err != nil {
		if isSecretNotFound(err) {
			return secretStatusModel{}, secretInspect{}, remote.ErrNotFound
		}
		return secretStatusModel{}, secretInspect{}, err
	}
	var reports []secretInspect
	if err := json.Unmarshal([]byte(output), &reports); err != nil {
		return secretStatusModel{}, secretInspect{}, fmt.Errorf("parse secret inspect output: %w", err)
	}
	if len(reports) == 0 {
		return secretStatusModel{}, secretInspect{}, remote.ErrNotFound
	}
	report := reports[0]
	return secretStatusModel{
		ID:        types.StringValue(report.ID),
		CreatedAt: types.StringValue(report.CreatedAt),
		Driver:    types.StringValue(report.Spec.Driver.Name),
	}, report, nil
}

func isSecretNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "no secret with name or ID")
}

func secretStatusObject(status secretStatusModel) types.Object {
	return types.ObjectValueMust(
		map[string]attr.Type{
			"id":         types.StringType,
			"created_at": types.StringType,
			"driver":     types.StringType,
		},
		map[string]attr.Value{
			"id":         status.ID,
			"created_at": status.CreatedAt,
			"driver":     status.Driver,
		},
	)
}

func (r *secretResource) ImportState(
	ctx context.Context,
	req resource.ImportStateRequest,
	resp *resource.ImportStateResponse,
) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
