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

func NewProjectMembershipResource() resource.Resource { return &projectMembershipResource{} }

type projectMembershipResource struct{ data *providerData }

type projectMembershipModel struct {
	ID           types.String `tfsdk:"id"`
	Project      types.String `tfsdk:"project"`
	User         types.String `tfsdk:"user"`
	Profile      types.String `tfsdk:"profile"`
	AccessPolicy types.String `tfsdk:"access_policy"`
	Admin        types.Bool   `tfsdk:"admin"`
}

func (r *projectMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_membership"
}

func (r *projectMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	rr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Binds a profile (ClientApplication, User, Practitioner, ...) to a Medplum Project with an access policy.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project":       schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "Project reference, e.g. Project/abc."},
			"user":          schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "User/ClientApplication/Bot reference."},
			"profile":       schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "Profile reference (Practitioner/Patient/ClientApplication/...)."},
			"access_policy": schema.StringAttribute{Optional: true, MarkdownDescription: "AccessPolicy reference."},
			"admin":         schema.BoolAttribute{Optional: true},
		},
	}
}

func (r *projectMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func refObj(ref string) map[string]any { return map[string]any{"reference": ref} }

func (m projectMembershipModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{
		"resourceType": "ProjectMembership",
		"project":      refObj(m.Project.ValueString()),
		"user":         refObj(m.User.ValueString()),
		"profile":      refObj(m.Profile.ValueString()),
	}
	if id != "" {
		doc["id"] = id
	}
	if v := strOrEmpty(m.AccessPolicy); v != "" {
		doc["accessPolicy"] = refObj(v)
	}
	if !m.Admin.IsNull() && !m.Admin.IsUnknown() {
		doc["admin"] = m.Admin.ValueBool()
	}
	return json.Marshal(doc)
}

func (m *projectMembershipModel) fromFHIR(body []byte) error {
	var doc struct {
		ID      string `json:"id"`
		Project struct {
			Reference string `json:"reference"`
		} `json:"project"`
		User struct {
			Reference string `json:"reference"`
		} `json:"user"`
		Profile struct {
			Reference string `json:"reference"`
		} `json:"profile"`
		AccessPolicy struct {
			Reference string `json:"reference"`
		} `json:"accessPolicy"`
		Admin *bool `json:"admin"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Project = types.StringValue(doc.Project.Reference)
	m.User = types.StringValue(doc.User.Reference)
	m.Profile = types.StringValue(doc.Profile.Reference)
	m.AccessPolicy = optString(doc.AccessPolicy.Reference)
	if doc.Admin != nil {
		m.Admin = types.BoolValue(*doc.Admin)
	} else {
		m.Admin = types.BoolNull()
	}
	return nil
}

// isProjectOwner reads the project and reports whether userRef is its owner.
func (r *projectMembershipResource) isProjectOwner(ctx context.Context, projectRef, userRef string) (bool, error) {
	// projectRef is like "Project/abc"; FHIRRead wants the bare id.
	id := projectRef
	for i := 0; i < len(projectRef); i++ {
		if projectRef[i] == '/' {
			id = projectRef[i+1:]
			break
		}
	}
	out, err := r.data.Client.FHIRRead(ctx, "Project", id)
	if err != nil {
		return false, err
	}
	var proj struct {
		Owner struct {
			Reference string `json:"reference"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &proj); err != nil {
		return false, err
	}
	return proj.Owner.Reference != "" && proj.Owner.Reference == userRef, nil
}

func (r *projectMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := m.toFHIR("")
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "ProjectMembership", body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "ProjectMembership", m.ID.ValueString())
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
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := plan.toFHIR(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "ProjectMembership", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Owner guard: refuse to delete the project owner's membership (would brick the project).
	owner, err := r.isProjectOwner(ctx, m.Project.ValueString(), m.User.ValueString())
	if err == nil && owner {
		resp.Diagnostics.AddError("Refusing to delete project owner membership",
			"This membership belongs to the project owner; deleting it can render the project unusable. Remove the project instead.")
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "ProjectMembership", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *projectMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*projectMembershipResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectMembershipResource)(nil)
	_ resource.ResourceWithImportState = (*projectMembershipResource)(nil)
)
