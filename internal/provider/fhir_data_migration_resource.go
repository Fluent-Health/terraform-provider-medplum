package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirmigrate"
)

func NewFHIRDataMigrationResource() resource.Resource { return &dataMigrationResource{} }

type dataMigrationResource struct{ data *providerData }

type dataMigrationModel struct {
	Name               types.String     `tfsdk:"name"`
	TargetResourceType types.String     `tfsdk:"target_resource_type"`
	Search             types.String     `tfsdk:"search"`
	CodeRemap          []codeRemapBlock `tfsdk:"code_remap"`
	BundleType         types.String     `tfsdk:"bundle_type"`
	PageSize           types.Int64      `tfsdk:"page_size"`
	MarkerSystem       types.String     `tfsdk:"marker_system"`
	ID                 types.String     `tfsdk:"id"`
	SpecHash           types.String     `tfsdk:"spec_hash"`
	ScannedCount       types.Int64      `tfsdk:"scanned_count"`
	ChangedCount       types.Int64      `tfsdk:"changed_count"`
	FailedCount        types.Int64      `tfsdk:"failed_count"`
}

type codeRemapBlock struct {
	From codingObj `tfsdk:"from"`
	To   codingObj `tfsdk:"to"`
}

type codingObj struct {
	System  types.String `tfsdk:"system"`
	Code    types.String `tfsdk:"code"`
	Display types.String `tfsdk:"display"`
}

func (r *dataMigrationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fhir_data_migration"
}

func codingAttr(required bool) schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Required: required,
		Attributes: map[string]schema.Attribute{
			"system":  schema.StringAttribute{Required: true},
			"code":    schema.StringAttribute{Required: true},
			"display": schema.StringAttribute{Optional: true},
		},
	}
}

func (r *dataMigrationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Runs an idempotent, self-limiting bulk code-remap over live FHIR resources via Medplum batch/transaction bundles. " +
			"A task resource: it records that migration `<name>` ran at a given transform hash; it does not track drift of the migrated data.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Stable identity of this migration; also the marker-tag path segment. Changing it forces replacement.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target_resource_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "FHIR resource type to scan and rewrite, e.g. QuestionnaireResponse.",
			},
			"search": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Raw FHIR search query narrowing the scan, e.g. `questionnaire=A,B`. The provider appends a `_tag:not` self-limiting filter and `_count`.",
			},
			"bundle_type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("batch"),
				MarkdownDescription: "`batch` (default; per-entry, no rollback) or `transaction` (per-page atomic; requires the project's `transaction-bundles` feature).",
			},
			"page_size": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(50),
				MarkdownDescription: "Resources scanned per page; each page becomes one bundle.",
			},
			"marker_system": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("urn:terraform-provider-medplum:data-migration"),
				MarkdownDescription: "Base URI for the `meta.tag` marker system. The tag written is `<marker_system>/<name>` = `<spec_hash>`.",
			},
			"id":            schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"spec_hash":     schema.StringAttribute{Computed: true, MarkdownDescription: "Hash of the transform spec; the marker-tag code and the value that drives re-runs."},
			"scanned_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "Resources scanned in the last run."},
			"changed_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "Resources whose codes were rewritten in the last run."},
			"failed_count":  schema.Int64Attribute{Computed: true, MarkdownDescription: "Bundle entries that failed in the last run."},
		},
		Blocks: map[string]schema.Block{
			"code_remap": schema.ListNestedBlock{
				MarkdownDescription: "Code remaps applied to every Coding in each scanned resource (recursively). At least one is required.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"from": codingAttr(true),
						"to":   codingAttr(true),
					},
				},
			},
		},
	}
}

func (r *dataMigrationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *dataMigrationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m dataMigrationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if len(m.CodeRemap) == 0 {
		resp.Diagnostics.AddError("No transform specified", "at least one code_remap block is required.")
	}
	if bt := m.BundleType.ValueString(); bt != "" && bt != "batch" && bt != "transaction" {
		resp.Diagnostics.AddAttributeError(path.Root("bundle_type"), "Invalid bundle_type", `must be "batch" or "transaction".`)
	}
	if !m.PageSize.IsNull() && !m.PageSize.IsUnknown() && m.PageSize.ValueInt64() <= 0 {
		resp.Diagnostics.AddAttributeError(path.Root("page_size"), "Invalid page_size", "must be > 0.")
	}
}

func (m dataMigrationModel) toSpec() fhirmigrate.Spec {
	remaps := make([]fhirmigrate.Remap, 0, len(m.CodeRemap))
	for _, cr := range m.CodeRemap {
		remaps = append(remaps, fhirmigrate.Remap{
			From: fhirmigrate.Coding{System: cr.From.System.ValueString(), Code: cr.From.Code.ValueString(), Display: strOrEmpty(cr.From.Display)},
			To:   fhirmigrate.Coding{System: cr.To.System.ValueString(), Code: cr.To.Code.ValueString(), Display: strOrEmpty(cr.To.Display)},
		})
	}
	return fhirmigrate.Spec{
		TargetResourceType: m.TargetResourceType.ValueString(),
		Search:             m.Search.ValueString(),
		MarkerSystem:       m.MarkerSystem.ValueString(),
		Remaps:             remaps,
	}
}

// configUnchanged reports whether every author-controlled field is equal, i.e.
// no Update will run. Used by ModifyPlan to decide whether to pin the computed
// outputs to state (so an unchanged config plans as a no-op).
func configUnchanged(a, b dataMigrationModel) bool {
	if !(a.Name.Equal(b.Name) && a.TargetResourceType.Equal(b.TargetResourceType) &&
		a.Search.Equal(b.Search) && a.MarkerSystem.Equal(b.MarkerSystem) &&
		a.BundleType.Equal(b.BundleType) && a.PageSize.Equal(b.PageSize)) {
		return false
	}
	if len(a.CodeRemap) != len(b.CodeRemap) {
		return false
	}
	for i := range a.CodeRemap {
		x, y := a.CodeRemap[i], b.CodeRemap[i]
		if !(x.From.System.Equal(y.From.System) && x.From.Code.Equal(y.From.Code) && x.From.Display.Equal(y.From.Display) &&
			x.To.System.Equal(y.To.System) && x.To.Code.Equal(y.To.Code) && x.To.Display.Equal(y.To.Display)) {
			return false
		}
	}
	return true
}

// ModifyPlan pins the computed outputs to their prior state when the config is
// unchanged, so a no-op apply produces an empty plan (computed attrs otherwise
// plan as "known after apply" and always show a diff). When the config changed,
// they are left unknown and Update recomputes them — avoiding the
// "inconsistent result after apply" trap.
func (r *dataMigrationResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // create or destroy: nothing to pin
	}
	var plan, state dataMigrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if configUnchanged(plan, state) {
		plan.ID = state.ID
		plan.SpecHash = state.SpecHash
		plan.ScannedCount = state.ScannedCount
		plan.ChangedCount = state.ChangedCount
		plan.FailedCount = state.FailedCount
		resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
	}
}

func (r *dataMigrationResource) notConfigured(resp *diag.Diagnostics) bool {
	if r.data == nil {
		resp.AddError("Provider not configured", "The Medplum provider was not configured; cannot call the API.")
		return true
	}
	return false
}

const maxMigrationPages = 100000

// runMigration executes the converging scan → remap → tag → write loop and
// records the run summary into m. Safe to re-run: already-tagged records are
// excluded by the scan, and the remap is a fixed point.
func (r *dataMigrationResource) runMigration(ctx context.Context, m *dataMigrationModel) error {
	spec := m.toSpec()
	hash := fhirmigrate.SpecHash(spec)
	name := m.Name.ValueString()
	target := m.TargetResourceType.ValueString()
	markerSys := m.MarkerSystem.ValueString()
	bundleType := m.BundleType.ValueString()
	pageSize := int(m.PageSize.ValueInt64())
	query := fhirmigrate.BuildScanQuery(spec.Search, markerSys, name, hash, pageSize)

	var scanned, changed, failed, pages int
	for {
		body, err := r.data.Client.FHIRSearch(ctx, target, query)
		if err != nil {
			return fmt.Errorf("scan search failed: %w", err)
		}
		entries, err := fhirmigrate.ParseSearchEntries(body)
		if err != nil {
			return fmt.Errorf("parse search page: %w", err)
		}
		if len(entries) == 0 {
			break // converged
		}
		resources := make([]map[string]any, 0, len(entries))
		pageChanged := 0
		for _, res := range entries {
			if fhirmigrate.ApplyRemaps(res, spec.Remaps) {
				pageChanged++
			}
			fhirmigrate.SetMarkerTag(res, markerSys+"/"+name, hash)
			resources = append(resources, res)
		}
		bundle, err := fhirmigrate.BuildBundle(bundleType, resources)
		if err != nil {
			return fmt.Errorf("build bundle: %w", err)
		}
		respBody, err := r.data.Client.FHIRBundle(ctx, bundle)
		if err != nil {
			return fmt.Errorf("bundle apply failed: %w", err)
		}
		result, err := fhirmigrate.ParseBundleResponse(respBody)
		if err != nil {
			return fmt.Errorf("parse bundle response: %w", err)
		}
		scanned += len(entries)
		changed += pageChanged
		failed += result.Failed
		pages++
		if result.Succeeded == 0 {
			return fmt.Errorf("migration made no progress: a page of %d resources all failed to write (failed=%d); aborting to avoid an infinite scan loop", len(entries), result.Failed)
		}
		if pages > maxMigrationPages {
			return fmt.Errorf("exceeded max pages (%d); aborting", maxMigrationPages)
		}
	}
	m.ID = types.StringValue(name)
	m.SpecHash = types.StringValue(hash)
	m.ScannedCount = types.Int64Value(int64(scanned))
	m.ChangedCount = types.Int64Value(int64(changed))
	m.FailedCount = types.Int64Value(int64(failed))
	return nil
}

func (r *dataMigrationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	if r.notConfigured(&resp.Diagnostics) {
		return
	}
	var m dataMigrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.runMigration(ctx, &m); err != nil {
		resp.Diagnostics.AddError("Migration failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *dataMigrationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	if r.notConfigured(&resp.Diagnostics) {
		return
	}
	var m dataMigrationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.runMigration(ctx, &m); err != nil {
		resp.Diagnostics.AddError("Migration failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Read is inert: it returns stored state without re-scanning. This resource
// records that a migration ran, not the live state of the migrated data.
func (r *dataMigrationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m dataMigrationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// Delete removes the record only; migrated data and marker tags are left as-is.
func (r *dataMigrationResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Migration record removed",
		"The migration record was removed from state. Migrated data was NOT reverted and meta.tag markers remain on affected resources.")
}

var (
	_ resource.Resource                   = (*dataMigrationResource)(nil)
	_ resource.ResourceWithConfigure      = (*dataMigrationResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*dataMigrationResource)(nil)
	_ resource.ResourceWithValidateConfig = (*dataMigrationResource)(nil)
)
