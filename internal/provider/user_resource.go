package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewUserResource() resource.Resource { return &userResource{} }

type userResource struct{ data *providerData }

type userModel struct {
	ID         types.String `tfsdk:"id"`
	FirstName  types.String `tfsdk:"first_name"`
	LastName   types.String `tfsdk:"last_name"`
	Email      types.String `tfsdk:"email"`
	ExternalID types.String `tfsdk:"external_id"`
	ProjectID  types.String `tfsdk:"project_id"`
	Admin      types.Bool   `tfsdk:"admin"`
	Password   types.String `tfsdk:"password"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum User. Project-scoped when project_id is set (else server-scoped).",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"first_name":  schema.StringAttribute{Required: true},
			"last_name":   schema.StringAttribute{Required: true},
			"email":       schema.StringAttribute{Optional: true},
			"external_id": schema.StringAttribute{Optional: true},
			"project_id":  schema.StringAttribute{Optional: true, MarkdownDescription: "Project id (bare uuid). Sets project scope and is required when password is set.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"admin":       schema.BoolAttribute{Optional: true},
			"password":    schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Write-only. Applied via /setpassword (no email). Requires email + project_id. Not read back."},
		},
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *userResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m userModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.Password.IsNull() && !m.Password.IsUnknown() && m.Password.ValueString() != "" {
		if strOrEmpty(m.Email) == "" || strOrEmpty(m.ProjectID) == "" {
			resp.Diagnostics.AddAttributeError(path.Root("password"), "password requires email + project_id",
				"Medplum /setpassword is project-scoped and keyed by email; set both email and project_id when using password.")
		}
	}
}

func (m userModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{
		"resourceType": "User",
		"firstName":    m.FirstName.ValueString(),
		"lastName":     m.LastName.ValueString(),
	}
	if id != "" {
		doc["id"] = id
	}
	if v := strOrEmpty(m.Email); v != "" {
		doc["email"] = v
	}
	if v := strOrEmpty(m.ExternalID); v != "" {
		doc["externalId"] = v
	}
	if v := strOrEmpty(m.ProjectID); v != "" {
		doc["project"] = refObj("Project/" + v)
	}
	if !m.Admin.IsNull() && !m.Admin.IsUnknown() {
		doc["admin"] = m.Admin.ValueBool()
	}
	return json.Marshal(doc)
}

// fromFHIR populates server-derived fields. Password is never read back.
func (m *userModel) fromFHIR(body []byte) error {
	var doc struct {
		ID         string `json:"id"`
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
		Email      string `json:"email"`
		ExternalID string `json:"externalId"`
		Project    struct {
			Reference string `json:"reference"`
		} `json:"project"`
		Admin *bool `json:"admin"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.FirstName = types.StringValue(doc.FirstName)
	m.LastName = types.StringValue(doc.LastName)
	m.Email = optString(doc.Email)
	m.ExternalID = optString(doc.ExternalID)
	// project reference "Project/<id>" → bare id; null when server-scoped.
	if ref := doc.Project.Reference; len(ref) > len("Project/") {
		m.ProjectID = types.StringValue(ref[len("Project/"):])
	} else {
		m.ProjectID = types.StringNull()
	}
	if doc.Admin != nil {
		m.Admin = types.BoolValue(*doc.Admin)
	} else {
		m.Admin = types.BoolNull()
	}
	return nil
}

// applyPassword sets the password via /setpassword when configured.
func (r *userResource) applyPassword(ctx context.Context, m userModel) error {
	pw := strOrEmpty(m.Password)
	if pw == "" {
		return nil
	}
	return r.data.Client.SetPassword(ctx, m.ProjectID.ValueString(), m.Email.ValueString(), pw)
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pwPlan := m.Password // preserve write-only value for state
	body, err := m.toFHIR("")
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "User", body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	m.Password = pwPlan
	if err := r.applyPassword(ctx, m); err != nil {
		resp.Diagnostics.AddError("setpassword failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pw := m.Password
	out, err := r.data.Client.FHIRRead(ctx, "User", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	m.Password = pw // keep write-only value from prior state (never read from server)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pwPlan := plan.Password
	body, err := plan.toFHIR(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "User", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	plan.Password = pwPlan
	if err := r.applyPassword(ctx, plan); err != nil {
		resp.Diagnostics.AddError("setpassword failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "User", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                   = (*userResource)(nil)
	_ resource.ResourceWithConfigure      = (*userResource)(nil)
	_ resource.ResourceWithImportState    = (*userResource)(nil)
	_ resource.ResourceWithValidateConfig = (*userResource)(nil)
)
