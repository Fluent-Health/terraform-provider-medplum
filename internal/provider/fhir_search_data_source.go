package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func NewFHIRSearchDataSource() datasource.DataSource { return &fhirSearchDataSource{} }

type fhirSearchDataSource struct{ data *providerData }

type fhirSearchModel struct {
	TargetResourceType types.String `tfsdk:"target_resource_type"`
	Search             types.String `tfsdk:"search"`
	Count              types.Int64  `tfsdk:"count"`
}

func (d *fhirSearchDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fhir_search"
}

func (d *fhirSearchDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Counts FHIR resources matching a search (via `_summary=count`). Useful for previewing migration scope.",
		Attributes: map[string]schema.Attribute{
			"target_resource_type": schema.StringAttribute{Required: true},
			"search":               schema.StringAttribute{Required: true, MarkdownDescription: "Raw FHIR search query (e.g. `questionnaire=X&_tag:not=...`)."},
			"count":                schema.Int64Attribute{Computed: true, MarkdownDescription: "Total matching resources."},
		},
	}
}

func (d *fhirSearchDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	pd, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	d.data = pd
}

func (d *fhirSearchDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m fhirSearchModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || d.data == nil {
		return
	}
	query := m.Search.ValueString()
	if query != "" {
		query += "&"
	}
	query += "_summary=count"
	out, err := d.data.Client.FHIRSearch(ctx, m.TargetResourceType.ValueString(), query)
	if err != nil {
		resp.Diagnostics.AddError("Search failed", err.Error())
		return
	}
	var b struct {
		Total int64 `json:"total"`
	}
	if err := json.Unmarshal(out, &b); err != nil {
		resp.Diagnostics.AddError("Invalid search response", err.Error())
		return
	}
	m.Count = types.Int64Value(b.Total)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

var (
	_ datasource.DataSource              = (*fhirSearchDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*fhirSearchDataSource)(nil)
)
