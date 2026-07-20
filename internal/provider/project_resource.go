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

func NewProjectResource() resource.Resource { return &projectResource{} }

type projectResource struct{ data *providerData }

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	Ref         types.String `tfsdk:"ref"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Features    types.List   `tfsdk:"features"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum Project. Creation requires super-admin credentials.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"ref": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Full FHIR reference to this resource, e.g. Project/abc. Use it wherever another resource takes a reference.",
			},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"features":    schema.ListAttribute{Optional: true, ElementType: types.StringType, MarkdownDescription: "Project features. Omit when none."},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (m projectModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{"resourceType": "Project", "name": m.Name.ValueString(), "id": id}
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	}
	if features := listToStrings(m.Features); len(features) > 0 {
		doc["features"] = features
	}
	return json.Marshal(doc)
}

func (m *projectModel) fromFHIR(body []byte) error {
	var doc struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Features    []string `json:"features"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Ref = refValue("Project", doc.ID)
	m.Name = types.StringValue(doc.Name)
	m.Description = optString(doc.Description)
	m.Features = stringsToList(doc.Features)
	return nil
}

// initProject calls Project/$init and returns the new project's bare id.
func (r *projectResource) initProject(ctx context.Context, name string) (string, error) {
	params := map[string]any{
		"resourceType": "Parameters",
		"parameter":    []map[string]any{{"name": "name", "valueString": name}},
	}
	body, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	out, err := r.data.Client.Operation(ctx, "Project", "", "$init", body)
	if err != nil {
		return "", err
	}
	// $init returns a Parameters resource wrapping the created Project, OR the
	// Project directly depending on version. Handle both: look for an id at the
	// top level, else dig into parameter[].resource.
	var top struct {
		ResourceType string `json:"resourceType"`
		ID           string `json:"id"`
		Parameter    []struct {
			Name     string          `json:"name"`
			Resource json.RawMessage `json:"resource"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(out, &top); err != nil {
		return "", err
	}
	if top.ResourceType == "Project" && top.ID != "" {
		return top.ID, nil
	}
	for _, p := range top.Parameter {
		if len(p.Resource) > 0 {
			var res struct {
				ResourceType string `json:"resourceType"`
				ID           string `json:"id"`
			}
			if json.Unmarshal(p.Resource, &res) == nil && res.ResourceType == "Project" && res.ID != "" {
				return res.ID, nil
			}
		}
	}
	return "", fmt.Errorf("project/$init response did not contain a project id: %s", string(out))
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := r.initProject(ctx, m.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Project init failed", err.Error()+"\n(Project creation requires super-admin credentials.)")
		return
	}
	// $init only sets name; if extra fields are configured, PUT the full resource.
	if strOrEmpty(m.Description) != "" || len(listToStrings(m.Features)) > 0 {
		body, encErr := m.toFHIR(id)
		if encErr != nil {
			resp.Diagnostics.AddError("Encoding failed", encErr.Error())
			return
		}
		out, updErr := r.data.Client.FHIRUpdate(ctx, "Project", id, body)
		if updErr != nil {
			resp.Diagnostics.AddError("Project configure failed", updErr.Error())
			return
		}
		if err := m.fromFHIR(out); err != nil {
			resp.Diagnostics.AddError("Decoding failed", err.Error())
			return
		}
	} else {
		m.ID = types.StringValue(id)
		m.Ref = refValue("Project", id)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "Project", m.ID.ValueString())
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

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectModel
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
	out, err := r.data.Client.FHIRUpdate(ctx, "Project", state.ID.ValueString(), body)
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

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "Project", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)
