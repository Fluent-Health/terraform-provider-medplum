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

func NewAccessPolicyResource() resource.Resource { return &accessPolicyResource{} }

type accessPolicyResource struct{ data *providerData }

type accessPolicyModel struct {
	ID           types.String              `tfsdk:"id"`
	Name         types.String              `tfsdk:"name"`
	Resource     []accessPolicyResourceRow `tfsdk:"resource"`
	IPAccessRule []accessPolicyIPRule      `tfsdk:"ip_access_rule"`
}

type accessPolicyResourceRow struct {
	ResourceType   types.String `tfsdk:"resource_type"`
	Criteria       types.String `tfsdk:"criteria"`
	Readonly       types.Bool   `tfsdk:"readonly"`
	HiddenFields   types.List   `tfsdk:"hidden_fields"`
	ReadonlyFields types.List   `tfsdk:"readonly_fields"`
	Compartment    types.String `tfsdk:"compartment"`
}

type accessPolicyIPRule struct {
	Name   types.String `tfsdk:"name"`
	Value  types.String `tfsdk:"value"`
	Action types.String `tfsdk:"action"`
}

func (r *accessPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_access_policy"
}

func (r *accessPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum AccessPolicy.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true},
		},
		Blocks: map[string]schema.Block{
			"resource": schema.ListNestedBlock{
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"resource_type":   schema.StringAttribute{Required: true},
						"criteria":        schema.StringAttribute{Optional: true},
						"readonly":        schema.BoolAttribute{Optional: true},
						"hidden_fields":   schema.ListAttribute{Optional: true, ElementType: types.StringType, PlanModifiers: []planmodifier.List{emptyListAsNull()}},
						"readonly_fields": schema.ListAttribute{Optional: true, ElementType: types.StringType, PlanModifiers: []planmodifier.List{emptyListAsNull()}},
						"compartment":     schema.StringAttribute{Optional: true, MarkdownDescription: "Compartment reference, e.g. Patient/123."},
					},
				},
			},
			"ip_access_rule": schema.ListNestedBlock{
				MarkdownDescription: "IP-based access rules applied to this policy.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name":   schema.StringAttribute{Optional: true, MarkdownDescription: "Human-readable label for the rule."},
						"value":  schema.StringAttribute{Required: true, MarkdownDescription: "CIDR or IP address, e.g. 192.168.1.0/24."},
						"action": schema.StringAttribute{Required: true, MarkdownDescription: "\"allow\" or \"block\"."},
					},
				},
			},
		},
	}
}

func (r *accessPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// toFHIR builds the AccessPolicy JSON from the model. id is included when non-empty (for PUT).
func (m accessPolicyModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{"resourceType": "AccessPolicy", "name": m.Name.ValueString()}
	if id != "" {
		doc["id"] = id
	}
	rows := make([]map[string]any, 0, len(m.Resource))
	for _, row := range m.Resource {
		entry := map[string]any{"resourceType": row.ResourceType.ValueString()}
		if v := strOrEmpty(row.Criteria); v != "" {
			entry["criteria"] = v
		}
		if !row.Readonly.IsNull() && !row.Readonly.IsUnknown() {
			entry["readonly"] = row.Readonly.ValueBool()
		}
		if hf := listToStrings(row.HiddenFields); len(hf) > 0 {
			entry["hiddenFields"] = hf
		}
		if rf := listToStrings(row.ReadonlyFields); len(rf) > 0 {
			entry["readonlyFields"] = rf
		}
		if v := strOrEmpty(row.Compartment); v != "" {
			entry["compartment"] = refObj(v)
		}
		rows = append(rows, entry)
	}
	doc["resource"] = rows
	if len(m.IPAccessRule) > 0 {
		ipRules := make([]map[string]any, 0, len(m.IPAccessRule))
		for _, rule := range m.IPAccessRule {
			r := map[string]any{
				"value":  rule.Value.ValueString(),
				"action": rule.Action.ValueString(),
			}
			if v := strOrEmpty(rule.Name); v != "" {
				r["name"] = v
			}
			ipRules = append(ipRules, r)
		}
		doc["ipAccessRule"] = ipRules
	}
	return json.Marshal(doc)
}

// fromFHIR populates the model's server-derived fields. Only modeled fields are
// read back, so server-managed fields (meta.*) never cause drift.
func (m *accessPolicyModel) fromFHIR(body []byte) error {
	var doc struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Resource []struct {
			ResourceType   string   `json:"resourceType"`
			Criteria       string   `json:"criteria"`
			Readonly       *bool    `json:"readonly"`
			HiddenFields   []string `json:"hiddenFields"`
			ReadonlyFields []string `json:"readonlyFields"`
			Compartment    struct {
				Reference string `json:"reference"`
			} `json:"compartment"`
		} `json:"resource"`
		IPAccessRule []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Action string `json:"action"`
		} `json:"ipAccessRule"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Name = types.StringValue(doc.Name)
	rows := make([]accessPolicyResourceRow, 0, len(doc.Resource))
	for _, row := range doc.Resource {
		rr := accessPolicyResourceRow{
			ResourceType:   types.StringValue(row.ResourceType),
			Criteria:       optString(row.Criteria),
			HiddenFields:   stringsToList(row.HiddenFields),
			ReadonlyFields: stringsToList(row.ReadonlyFields),
			Compartment:    optString(row.Compartment.Reference),
		}
		if row.Readonly != nil {
			rr.Readonly = types.BoolValue(*row.Readonly)
		} else {
			rr.Readonly = types.BoolNull()
		}
		rows = append(rows, rr)
	}
	// Use a nil slice (not an empty one) when the server returns no rows, so a
	// zero-block config round-trips to null (matches ip_access_rule below and
	// avoids an "inconsistent result" diff for a degenerate empty policy).
	if len(rows) == 0 {
		m.Resource = nil
	} else {
		m.Resource = rows
	}
	if len(doc.IPAccessRule) > 0 {
		ipRules := make([]accessPolicyIPRule, 0, len(doc.IPAccessRule))
		for _, rule := range doc.IPAccessRule {
			ipRules = append(ipRules, accessPolicyIPRule{
				Name:   optString(rule.Name),
				Value:  types.StringValue(rule.Value),
				Action: types.StringValue(rule.Action),
			})
		}
		m.IPAccessRule = ipRules
	} else {
		m.IPAccessRule = nil
	}
	return nil
}

func (r *accessPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m accessPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := m.toFHIR("")
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "AccessPolicy", body)
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

func (r *accessPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m accessPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "AccessPolicy", m.ID.ValueString())
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

func (r *accessPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state accessPolicyModel
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
	out, err := r.data.Client.FHIRUpdate(ctx, "AccessPolicy", state.ID.ValueString(), body)
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

func (r *accessPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m accessPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "AccessPolicy", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *accessPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*accessPolicyResource)(nil)
	_ resource.ResourceWithConfigure   = (*accessPolicyResource)(nil)
	_ resource.ResourceWithImportState = (*accessPolicyResource)(nil)
)
