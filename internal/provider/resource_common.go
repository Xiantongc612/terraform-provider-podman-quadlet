package provider

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/quadlet"
	"github.com/Xiantongc612/terraform-provider-podman-quadlet/internal/remote"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var resourceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var environmentNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type metadataModel struct {
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Labels      types.Map    `tfsdk:"labels"`
}

type statusModel struct {
	Path        types.String `tfsdk:"path"`
	Unit        types.String `tfsdk:"unit"`
	Checksum    types.String `tfsdk:"checksum"`
	LoadState   types.String `tfsdk:"load_state"`
	ActiveState types.String `tfsdk:"active_state"`
	SubState    types.String `tfsdk:"sub_state"`
}

type managedResource struct {
	client           remote.Client
	quadletDirectory string
	systemctlPrefix  string
	installTarget    string
}

func metadataBlock() schema.SingleNestedBlock {
	return schema.SingleNestedBlock{
		Description: "Kubernetes-inspired resource metadata.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "Immutable Quadlet and Podman object name.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"description": schema.StringAttribute{
				Optional:    true,
				Description: "Human-readable systemd unit description.",
			},
			"labels": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Labels applied to the Podman object.",
			},
		},
	}
}

func statusAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Computed:    true,
		Description: "Observed remote Quadlet and systemd status.",
		Attributes: map[string]schema.Attribute{
			"path":         schema.StringAttribute{Computed: true},
			"unit":         schema.StringAttribute{Computed: true},
			"checksum":     schema.StringAttribute{Computed: true},
			"load_state":   schema.StringAttribute{Computed: true},
			"active_state": schema.StringAttribute{Computed: true},
			"sub_state":    schema.StringAttribute{Computed: true},
		},
	}
}

func statusObject(status statusModel) types.Object {
	return types.ObjectValueMust(
		map[string]attr.Type{
			"path":         types.StringType,
			"unit":         types.StringType,
			"checksum":     types.StringType,
			"load_state":   types.StringType,
			"active_state": types.StringType,
			"sub_state":    types.StringType,
		},
		map[string]attr.Value{
			"path":         status.Path,
			"unit":         status.Unit,
			"checksum":     status.Checksum,
			"load_state":   status.LoadState,
			"active_state": status.ActiveState,
			"sub_state":    status.SubState,
		},
	)
}

func (r *managedResource) configure(req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *providerData, got %T.", req.ProviderData),
		)
		return
	}
	r.client = data.client
	r.quadletDirectory = data.quadletDirectory
	r.systemctlPrefix = data.systemctlPrefix
	r.installTarget = data.installTarget
}

func (r *managedResource) filePath(name, extension string) string {
	return path.Join(r.quadletDirectory, name+extension)
}

func (r *managedResource) systemctl(ctx context.Context, command string) (string, error) {
	return r.client.Run(ctx, r.systemctlPrefix+" "+command)
}

func (r *managedResource) apply(
	ctx context.Context,
	filePath string,
	unit string,
	content []byte,
	create bool,
) (statusModel, error) {
	existing, err := r.client.ReadFile(ctx, filePath)
	if err == nil && !quadlet.Managed(existing) {
		return statusModel{}, fmt.Errorf("refusing to overwrite unmanaged remote file %q", filePath)
	}
	if err != nil && !errors.Is(err, remote.ErrNotFound) {
		return statusModel{}, fmt.Errorf("check remote file: %w", err)
	}
	if err := r.client.WriteFile(ctx, filePath, content); err != nil {
		return statusModel{}, fmt.Errorf("write Quadlet: %w", err)
	}
	if _, err := r.systemctl(ctx, "daemon-reload"); err != nil {
		return statusModel{}, err
	}
	action := "restart"
	if create {
		action = "enable --now"
	}
	if _, err := r.systemctl(ctx, action+" "+remote.ShellQuote(unit)); err != nil {
		return statusModel{}, err
	}
	return r.status(ctx, filePath, unit, content)
}

func (r *managedResource) status(
	ctx context.Context,
	filePath string,
	unit string,
	content []byte,
) (statusModel, error) {
	output, err := r.systemctl(
		ctx,
		"show --property=LoadState --property=ActiveState --property=SubState "+remote.ShellQuote(unit),
	)
	if err != nil {
		return statusModel{}, err
	}
	values := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		key, value, found := strings.Cut(line, "=")
		if found {
			values[key] = value
		}
	}
	return statusModel{
		Path:        types.StringValue(filePath),
		Unit:        types.StringValue(unit),
		Checksum:    types.StringValue(quadlet.Checksum(content)),
		LoadState:   types.StringValue(values["LoadState"]),
		ActiveState: types.StringValue(values["ActiveState"]),
		SubState:    types.StringValue(values["SubState"]),
	}, nil
}

func (r *managedResource) delete(ctx context.Context, filePath, unit string) error {
	content, err := r.client.ReadFile(ctx, filePath)
	if errors.Is(err, remote.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read remote file before deletion: %w", err)
	}
	if !quadlet.Managed(content) {
		return fmt.Errorf("refusing to delete unmanaged remote file %q", filePath)
	}
	if _, err := r.systemctl(ctx, "disable --now "+remote.ShellQuote(unit)); err != nil {
		return err
	}
	if err := r.client.RemoveFile(ctx, filePath); err != nil {
		return err
	}
	if _, err := r.systemctl(ctx, "daemon-reload"); err != nil {
		return err
	}
	return nil
}

func validateMetadata(metadata metadataModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics
	if metadata.Name.IsUnknown() || metadata.Name.IsNull() {
		if metadata.Name.IsNull() {
			diagnostics.AddError("Missing resource name", "A metadata block with a name is required.")
		}
		return diagnostics
	}
	name := metadata.Name.ValueString()
	if len(name) > 128 || !resourceNamePattern.MatchString(name) {
		diagnostics.AddError(
			"Invalid resource name",
			"metadata.name must be at most 128 characters and contain only letters, digits, dots, underscores, and hyphens.",
		)
	}
	validateString := func(label string, value types.String) {
		if !value.IsNull() && !value.IsUnknown() && containsInvalidLine(value.ValueString()) {
			diagnostics.AddError("Invalid "+label, label+" must not contain newlines or NUL bytes.")
		}
	}
	validateString("description", metadata.Description)
	if !metadata.Labels.IsNull() && !metadata.Labels.IsUnknown() {
		for key, value := range metadata.Labels.Elements() {
			stringValue, ok := value.(types.String)
			if !ok || containsInvalidLine(key) || containsInvalidLine(stringValue.ValueString()) {
				diagnostics.AddError("Invalid resource label", "Label names and values must be strings without newlines or NUL bytes.")
			}
		}
	}
	return diagnostics
}

func containsInvalidLine(value string) bool {
	return strings.ContainsAny(value, "\r\n\x00")
}

func mapValues(ctx context.Context, value types.Map) (map[string]string, diag.Diagnostics) {
	if value.IsNull() || value.IsUnknown() {
		return nil, nil
	}
	result := make(map[string]string, len(value.Elements()))
	diagnostics := value.ElementsAs(ctx, &result, false)
	return result, diagnostics
}

func sortedMapPairs(key string, values map[string]string, separator string) []quadlet.Pair {
	keys := make([]string, 0, len(values))
	for itemKey := range values {
		keys = append(keys, itemKey)
	}
	sort.Strings(keys)
	pairs := make([]quadlet.Pair, 0, len(keys))
	for _, itemKey := range keys {
		pairs = append(pairs, quadlet.Pair{Key: key, Value: itemKey + separator + values[itemKey]})
	}
	return pairs
}

func pairsByKey(sections map[string][]quadlet.Pair, section string) map[string][]string {
	result := make(map[string][]string)
	for _, pair := range sections[section] {
		result[pair.Key] = append(result[pair.Key], pair.Value)
	}
	return result
}

func first(values map[string][]string, key string) string {
	if len(values[key]) == 0 {
		return ""
	}
	return values[key][0]
}

func parseKeyValue(values []string, separator string) map[string]string {
	result := make(map[string]string)
	for _, value := range values {
		key, itemValue, found := strings.Cut(value, separator)
		if found {
			result[key] = itemValue
		}
	}
	return result
}

func stringMapValue(values map[string]string) types.Map {
	if len(values) == 0 {
		return types.MapNull(types.StringType)
	}
	elements := make(map[string]attr.Value, len(values))
	for key, value := range values {
		elements[key] = types.StringValue(value)
	}
	return types.MapValueMust(types.StringType, elements)
}

func optionalStringValue(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func optionalBoolValue(value string) types.Bool {
	if value == "" {
		return types.BoolNull()
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return types.BoolNull()
	}
	return types.BoolValue(parsed)
}

func invertedOptionalBoolValue(value string) types.Bool {
	parsed := optionalBoolValue(value)
	if parsed.IsNull() {
		return parsed
	}
	return types.BoolValue(!parsed.ValueBool())
}

func optionalStringPairs(values map[string]types.String) []quadlet.Pair {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]quadlet.Pair, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if !value.IsNull() && !value.IsUnknown() {
			result = append(result, quadlet.Pair{Key: key, Value: value.ValueString()})
		}
	}
	return result
}

func optionalBoolPair(key string, value types.Bool) []quadlet.Pair {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	return []quadlet.Pair{{Key: key, Value: strconv.FormatBool(value.ValueBool())}}
}

func unitSection(description types.String) quadlet.Section {
	section := quadlet.Section{Name: "Unit"}
	if !description.IsNull() && !description.IsUnknown() {
		section.Pairs = append(section.Pairs, quadlet.Pair{Key: "Description", Value: description.ValueString()})
	}
	return section
}

func installSection(target string) quadlet.Section {
	return quadlet.Section{
		Name:  "Install",
		Pairs: []quadlet.Pair{{Key: "WantedBy", Value: target}},
	}
}

func stringListValue(values []string) types.List {
	if len(values) == 0 {
		return types.ListNull(types.StringType)
	}
	elements := make([]attr.Value, 0, len(values))
	for _, value := range values {
		elements = append(elements, types.StringValue(value))
	}
	return types.ListValueMust(types.StringType, elements)
}
