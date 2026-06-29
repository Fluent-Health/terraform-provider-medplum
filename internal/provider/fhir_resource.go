package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirjson"
)

func NewFHIRResource() resource.Resource { return &fhirResource{} }

type fhirResource struct {
	data *providerData
}

type fhirResourceModel struct {
	ResourceType types.String `tfsdk:"resource_type"`
	Body         types.String `tfsdk:"body"`
	ID           types.String `tfsdk:"id"`
	VersionID    types.String `tfsdk:"version_id"`
	LastUpdated  types.String `tfsdk:"last_updated"`
	Validation   types.String `tfsdk:"validation"`
}

func (r *fhirResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fhir_resource"
}

func (r *fhirResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A generic FHIR R4 resource, validated against the R4 JSON schema at plan time.",
		Attributes: map[string]schema.Attribute{
			"resource_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "FHIR resourceType, e.g. ValueSet. Must match body.resourceType.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"body": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The FHIR resource as JSON. Do not set 'id'; it is server-assigned.",
				PlanModifiers:       []planmodifier.String{semanticJSONBody()},
			},
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			// Hold the prior server-managed metadata only when the body is
			// semantically unchanged, so an `import` (or any no-op plan) doesn't
			// show version_id/last_updated flipping to "(known after apply)" as a
			// spurious diff. When the body changes, Medplum reassigns these, so
			// they must stay unknown — a plain UseStateForUnknown would pin the
			// stale value and trip "Provider produced inconsistent result after
			// apply" on update.
			"version_id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{serverManagedMeta()}},
			"last_updated": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{serverManagedMeta()}},
			"validation": schema.StringAttribute{
				// Optional only (no Computed/Default): a Computed default would
				// surface as a spurious "+ validation" diff on every import, since
				// imported state has no value. Unset is treated as "error" in code.
				Optional: true,
				MarkdownDescription: "How FHIR R4 schema-validation results are reported: `error` (default when unset — fails the plan), " +
					"`warning` (report but allow), or `none` (skip). Use `warning`/`none` for resources that " +
					"intentionally use Medplum-accepted constructs outside strict R4 (e.g. custom StructureMap transforms).",
			},
		},
	}
}

func (r *fhirResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = data
}

// ValidateConfig performs config-only structural checks. It must NOT depend on
// provider data (r.data): the framework runs the validation RPC without invoking
// the resource's Configure, so r.data is nil here. Schema validation that needs
// the compiled validator lives in ModifyPlan (where Configure has run).
func (r *fhirResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m fhirResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Body.IsUnknown() || m.Body.IsNull() || m.ResourceType.IsUnknown() {
		return
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(m.Body.ValueString()), &doc); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "Invalid JSON", err.Error())
		return
	}
	if doc["resourceType"] != m.ResourceType.ValueString() {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "resourceType mismatch",
			fmt.Sprintf("body.resourceType (%v) must equal resource_type (%s)", doc["resourceType"], m.ResourceType.ValueString()))
	}
	if _, ok := doc["id"]; ok {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "id must not be set",
			"The 'id' field is assigned by the server; remove it from body.")
	}
}

// ModifyPlan runs full R4 schema validation at plan time. Configure has reliably
// run before this hook, so r.data (and the compiled Validator) is available.
func (r *fhirResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() { // resource is being destroyed; nothing to validate
		return
	}
	var m fhirResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Body.IsUnknown() || m.Body.IsNull() || m.ResourceType.IsUnknown() {
		return
	}
	if r.data == nil || r.data.Validator == nil {
		return
	}
	if err := r.data.Validator.Validate(m.ResourceType.ValueString(), []byte(m.Body.ValueString())); err != nil {
		switch m.Validation.ValueString() {
		case "none":
			// Validation explicitly disabled for this resource.
		case "warning":
			resp.Diagnostics.AddAttributeWarning(path.Root("body"), "FHIR schema validation failed", err.Error())
		default: // "error" (also the default) — any unrecognized value is strict.
			resp.Diagnostics.AddAttributeError(path.Root("body"), "FHIR schema validation failed", err.Error())
		}
	}
}

func extractMeta(serverBody []byte) (id, versionID, lastUpdated string, err error) {
	var doc struct {
		ID   string `json:"id"`
		Meta struct {
			VersionID   string `json:"versionId"`
			LastUpdated string `json:"lastUpdated"`
		} `json:"meta"`
	}
	if err = json.Unmarshal(serverBody, &doc); err != nil {
		return "", "", "", err
	}
	return doc.ID, doc.Meta.VersionID, doc.Meta.LastUpdated, nil
}

func (r *fhirResource) notConfigured(resp *diag.Diagnostics) bool {
	if r.data == nil {
		resp.AddError("Provider not configured", "The Medplum provider was not configured; cannot call the API.")
		return true
	}
	return false
}

func (r *fhirResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.notConfigured(&resp.Diagnostics) {
		return
	}
	var m fhirResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, m.ResourceType.ValueString(), []byte(m.Body.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	id, ver, upd, err := extractMeta(out)
	if err != nil {
		resp.Diagnostics.AddError("Invalid create response", fmt.Sprintf("could not parse server response: %s", err))
		return
	}
	if id == "" {
		resp.Diagnostics.AddError("Create response missing id", "the server did not return an id for the created resource")
		return
	}
	m.ID = types.StringValue(id)
	m.VersionID = types.StringValue(ver)
	m.LastUpdated = types.StringValue(upd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *fhirResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	if r.notConfigured(&resp.Diagnostics) {
		return
	}
	var m fhirResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, m.ResourceType.ValueString(), m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	stateBody := m.Body.ValueString()
	trimmed := strings.TrimSpace(stateBody)
	if trimmed == "" || trimmed == "{}" {
		// Import (or empty) sentinel: adopt the server body as the starting point.
		m.Body = types.StringValue(string(out))
	} else {
		contained, cerr := fhirjson.Contains([]byte(stateBody), out)
		if cerr != nil {
			resp.Diagnostics.AddError("Drift comparison failed", fmt.Sprintf("could not compare state to server response: %s", cerr))
			return
		}
		if !contained {
			// Genuine drift: the server no longer satisfies the desired config.
			// Surface the server's actual state so the next plan shows a diff.
			m.Body = types.StringValue(string(out))
		}
		// Otherwise the server still satisfies the desired config; keep stateBody.
	}

	id, ver, upd, err := extractMeta(out)
	if err != nil {
		resp.Diagnostics.AddError("Invalid read response", fmt.Sprintf("could not parse server response: %s", err))
		return
	}
	m.ID = types.StringValue(id)
	m.VersionID = types.StringValue(ver)
	m.LastUpdated = types.StringValue(upd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *fhirResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.notConfigured(&resp.Diagnostics) {
		return
	}
	var plan, state fhirResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Re-inject the server id into the body for the PUT.
	var doc map[string]any
	if err := json.Unmarshal([]byte(plan.Body.ValueString()), &doc); err != nil {
		resp.Diagnostics.AddError("Invalid body JSON", err.Error())
		return
	}
	doc["id"] = state.ID.ValueString()
	putBody, _ := json.Marshal(doc)

	out, err := r.data.Client.FHIRUpdate(ctx, plan.ResourceType.ValueString(), state.ID.ValueString(), putBody)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	id, ver, upd, err := extractMeta(out)
	if err != nil {
		resp.Diagnostics.AddError("Invalid update response", fmt.Sprintf("could not parse server response: %s", err))
		return
	}
	plan.ID = types.StringValue(id)
	plan.VersionID = types.StringValue(ver)
	plan.LastUpdated = types.StringValue(upd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fhirResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	if r.notConfigured(&resp.Diagnostics) {
		return
	}
	var m fhirResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, m.ResourceType.ValueString(), m.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

// ImportState accepts "ResourceType/id" and populates resource_type + id.
func (r *fhirResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	rt, id, ok := splitRef(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "expected format ResourceType/id, e.g. ValueSet/abc123")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_type"), rt)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	// body is populated by the subsequent Read.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("body"), "{}")...)
}

func splitRef(s string) (resourceType, id string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:], i > 0 && i < len(s)-1
		}
	}
	return "", "", false
}

// interface assertions
var (
	_ resource.Resource                   = (*fhirResource)(nil)
	_ resource.ResourceWithConfigure      = (*fhirResource)(nil)
	_ resource.ResourceWithImportState    = (*fhirResource)(nil)
	_ resource.ResourceWithValidateConfig = (*fhirResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*fhirResource)(nil)
)
