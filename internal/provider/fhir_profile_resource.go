package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirjson"
	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirprofile"
)

func NewFHIRProfileResource() resource.Resource { return &fhirProfileResource{} }

type fhirProfileResource struct{ data *providerData }

type fhirProfileModel struct {
	StructureDefinition types.String `tfsdk:"structure_definition"`
	Strict              types.Bool   `tfsdk:"strict"`
	ID                  types.String `tfsdk:"id"`
	URL                 types.String `tfsdk:"url"`
	VersionID           types.String `tfsdk:"version_id"`
	LastUpdated         types.String `tfsdk:"last_updated"`
}

func (r *fhirProfileResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fhir_profile"
}

func (r *fhirProfileResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A FHIR R4 StructureDefinition (profile). Validated at plan time against what Medplum actually enforces.",
		Attributes: map[string]schema.Attribute{
			"structure_definition": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The compiled StructureDefinition as JSON. Must include a non-empty snapshot.",
			},
			"strict": schema.BoolAttribute{
				Optional:            true,
				MarkdownDescription: "When true, WARN findings and decorative-only profiles become plan errors.",
			},
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"url":          schema.StringAttribute{Computed: true, MarkdownDescription: "StructureDefinition.url."},
			"version_id":   schema.StringAttribute{Computed: true},
			"last_updated": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *fhirProfileResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

// ModifyPlan runs base R4 schema validation + the Medplum-context profile gate.
func (r *fhirProfileResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy
	}
	var m fhirProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.StructureDefinition.IsUnknown() || m.StructureDefinition.IsNull() {
		return
	}
	body := []byte(m.StructureDefinition.ValueString())

	// 1. Base R4 schema validation (when configured).
	if r.data != nil && r.data.Validator != nil {
		if err := r.data.Validator.Validate("StructureDefinition", body); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("structure_definition"), "FHIR schema validation failed", err.Error())
			return
		}
	}

	// 2. Medplum-context useful-profile gate.
	report, err := fhirprofile.Analyze(body)
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("structure_definition"), "Profile analysis failed", err.Error())
		return
	}
	strict := !m.Strict.IsNull() && m.Strict.ValueBool()

	for _, f := range report.Rejects() {
		resp.Diagnostics.AddAttributeError(path.Root("structure_definition"),
			"Profile construct rejected by Medplum", fmt.Sprintf("%s: %s", f.Path, f.Message))
	}
	for _, f := range report.Warns() {
		msg := fmt.Sprintf("%s: %s", f.Path, f.Message)
		if strict {
			resp.Diagnostics.AddAttributeError(path.Root("structure_definition"), "Profile construct unenforced (strict)", msg)
		} else {
			resp.Diagnostics.AddAttributeWarning(path.Root("structure_definition"), "Profile construct unenforced by Medplum", msg)
		}
	}
	// Decorative-only profile: no enforced constraints at all.
	if len(report.Rejects()) == 0 && report.EnforcedCount == 0 {
		detail := "this profile contributes no constraints Medplum enforces; it is decorative. " + report.Summary()
		if strict {
			resp.Diagnostics.AddAttributeError(path.Root("structure_definition"), "Decorative-only profile (strict)", detail)
		} else {
			resp.Diagnostics.AddAttributeWarning(path.Root("structure_definition"), "Decorative-only profile", detail)
		}
	} else {
		resp.Diagnostics.AddAttributeWarning(path.Root("structure_definition"), "Profile enforcement summary", report.Summary())
	}
}

func (r *fhirProfileResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m fhirProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "StructureDefinition", []byte(m.StructureDefinition.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	id, _, _, merr := extractMeta(out)
	if merr != nil || id == "" {
		resp.Diagnostics.AddError("Invalid server response", "could not parse StructureDefinition id from response")
		return
	}
	r.setComputed(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *fhirProfileResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m fhirProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "StructureDefinition", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	stateBody := m.StructureDefinition.ValueString()
	trimmed := strings.TrimSpace(stateBody)
	if trimmed == "" || trimmed == "{}" {
		m.StructureDefinition = types.StringValue(string(out))
	} else {
		contained, cerr := fhirjson.Contains([]byte(stateBody), out)
		if cerr != nil {
			resp.Diagnostics.AddError("Drift comparison failed", cerr.Error())
			return
		}
		if !contained {
			m.StructureDefinition = types.StringValue(string(out))
		}
	}
	r.setComputed(&m, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *fhirProfileResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state fhirProfileModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(plan.StructureDefinition.ValueString()), &doc); err != nil {
		resp.Diagnostics.AddError("Invalid StructureDefinition JSON", err.Error())
		return
	}
	doc["id"] = state.ID.ValueString()
	putBody, _ := json.Marshal(doc)
	out, err := r.data.Client.FHIRUpdate(ctx, "StructureDefinition", state.ID.ValueString(), putBody)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	id, _, _, merr := extractMeta(out)
	if merr != nil || id == "" {
		resp.Diagnostics.AddError("Invalid server response", "could not parse StructureDefinition id from response")
		return
	}
	r.setComputed(&plan, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fhirProfileResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m fhirProfileModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "StructureDefinition", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *fhirProfileResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id := req.ID
	if rt, rest, ok := strings.Cut(req.ID, "/"); ok && rt == "StructureDefinition" {
		id = rest
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("structure_definition"), "{}")...)
}

// setComputed fills id/url/version_id/last_updated from the server body.
func (r *fhirProfileResource) setComputed(m *fhirProfileModel, serverBody []byte) {
	id, ver, upd, _ := extractMeta(serverBody)
	var doc struct {
		URL string `json:"url"`
	}
	_ = json.Unmarshal(serverBody, &doc)
	m.ID = types.StringValue(id)
	m.URL = types.StringValue(doc.URL)
	m.VersionID = types.StringValue(ver)
	m.LastUpdated = types.StringValue(upd)
}

// interface assertions
var (
	_ resource.Resource                = (*fhirProfileResource)(nil)
	_ resource.ResourceWithConfigure   = (*fhirProfileResource)(nil)
	_ resource.ResourceWithImportState = (*fhirProfileResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*fhirProfileResource)(nil)
)
