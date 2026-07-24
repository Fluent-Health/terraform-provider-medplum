package provider

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

type botModel struct {
	ID             types.String `tfsdk:"id"`
	Ref            types.String `tfsdk:"ref"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	Code           types.String `tfsdk:"code"`
	SourcePath     types.String `tfsdk:"source_path"`
	SourceHash     types.String `tfsdk:"source_hash"`
	RuntimeVersion types.String `tfsdk:"runtime_version"`
	Timeout        types.Int64  `tfsdk:"timeout"`
	RunAsUser      types.Bool   `tfsdk:"run_as_user"`
	AccessPolicy   types.String `tfsdk:"access_policy"`
	Admin          types.Bool   `tfsdk:"admin"`
	CronString     types.String `tfsdk:"cron_string"`
	ProjectID      types.String `tfsdk:"project_id"`
	MembershipID   types.String `tfsdk:"membership_id"`
}

// resolveCode returns the bundled bot code from `code` or `source_path`.
// ok=false with a nil error means the value is not yet known (unknown in plan).
func (m botModel) resolveCode() (code string, ok bool, err error) {
	switch {
	case !m.Code.IsNull() && !m.Code.IsUnknown():
		return m.Code.ValueString(), true, nil
	case m.Code.IsUnknown() || m.SourcePath.IsUnknown():
		return "", false, nil
	case !m.SourcePath.IsNull():
		b, err := os.ReadFile(m.SourcePath.ValueString())
		if err != nil {
			return "", false, fmt.Errorf("reading source_path: %w", err)
		}
		return string(b), true, nil
	}
	return "", false, fmt.Errorf("one of code or source_path must be set")
}

func sourceHashOf(code string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(code)))
}

// cronField is one position in a standard 5-field cron expression.
type cronField struct {
	name     string
	min, max int
}

var cronFields = [5]cronField{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day-of-month", 1, 31},
	{"month", 1, 12},
	{"day-of-week", 0, 6},
}

// validateCronString reports whether expr is a valid standard 5-field cron
// expression (minute hour day-of-month month day-of-week). It mirrors the
// defaults of the `cron-validator` package Medplum uses server-side
// (packages/server/src/workers/cron.ts): no seconds field, no month/day
// aliases. An expression Medplum would silently drop is rejected here at plan
// time instead.
func validateCronString(expr string) error {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return fmt.Errorf("expected 5 space-separated fields (minute hour day-of-month month day-of-week), got %d", len(parts))
	}
	for i, part := range parts {
		if err := validateCronField(part, cronFields[i]); err != nil {
			return fmt.Errorf("%s field %q: %w", cronFields[i].name, part, err)
		}
	}
	return nil
}

// validateCronField validates one comma-separated cron field: each term is
// "*", a number, a range "a-b", any of those followed by a "/step", or a bare
// "*/step".
func validateCronField(field string, f cronField) error {
	for _, term := range strings.Split(field, ",") {
		if term == "" {
			return fmt.Errorf("empty term")
		}
		base := term
		if slash := strings.Index(term, "/"); slash >= 0 {
			base = term[:slash]
			stepStr := term[slash+1:]
			if strings.HasPrefix(stepStr, "+") {
				return fmt.Errorf("invalid step %q", stepStr)
			}
			step, err := strconv.Atoi(stepStr)
			if err != nil || step < 1 {
				return fmt.Errorf("invalid step %q", stepStr)
			}
		}
		if base == "*" {
			continue
		}
		if dash := strings.Index(base, "-"); dash >= 0 {
			loStr := base[:dash]
			hiStr := base[dash+1:]
			if strings.HasPrefix(loStr, "+") || strings.HasPrefix(hiStr, "+") {
				return fmt.Errorf("invalid range %q", base)
			}
			lo, err1 := strconv.Atoi(loStr)
			hi, err2 := strconv.Atoi(hiStr)
			if err1 != nil || err2 != nil {
				return fmt.Errorf("invalid range %q", base)
			}
			if lo < f.min || hi > f.max || lo > hi {
				return fmt.Errorf("range %q out of bounds %d-%d", base, f.min, f.max)
			}
			continue
		}
		if strings.HasPrefix(base, "+") {
			return fmt.Errorf("not a number: %q", base)
		}
		n, err := strconv.Atoi(base)
		if err != nil {
			return fmt.Errorf("not a number: %q", base)
		}
		if n < f.min || n > f.max {
			return fmt.Errorf("%d out of bounds %d-%d", n, f.min, f.max)
		}
	}
	return nil
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// binaryIDFromURL extracts the Binary resource id from Bot.executableCode.url.
// Medplum rewrites attachment URLs on read, so the value can be a plain
// reference ("Binary/{id}"), a FHIR API URL (".../fhir/R4/Binary/{id}"
// optionally followed by "/_history/{vid}"), or a presigned storage URL whose
// path ends with "/{id}/{versionId}". Returns "" when the id is unrecoverable.
func binaryIDFromURL(raw string) string {
	if rest, found := strings.CutPrefix(raw, "Binary/"); found {
		return strings.SplitN(rest, "/", 2)[0]
	}
	if i := strings.Index(raw, "/Binary/"); i >= 0 {
		rest := raw[i+len("/Binary/"):]
		return strings.SplitN(rest, "/", 2)[0]
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) >= 2 && uuidRe.MatchString(segs[len(segs)-1]) && uuidRe.MatchString(segs[len(segs)-2]) {
		return segs[len(segs)-2]
	}
	return ""
}

// adminCreateBody builds the plain-JSON BotInitParameters payload for
// POST /admin/projects/{id}/bot. timeout and runAsUser are not part of that
// contract — they are applied with a follow-up PUT (see updateBotFields).
func (m botModel) adminCreateBody() ([]byte, error) {
	doc := map[string]any{
		"name":           m.Name.ValueString(),
		"runtimeVersion": m.RuntimeVersion.ValueString(),
	}
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	}
	if v := strOrEmpty(m.AccessPolicy); v != "" {
		doc["accessPolicy"] = refObj(v)
	}
	return json.Marshal(doc)
}

// applyBotFields writes the model's Bot fields onto doc (the server's current
// Bot JSON), preserving fields the provider does not model (sourceCode,
// executableCode, meta, ...).
func (m botModel) applyBotFields(doc map[string]any) {
	doc["name"] = m.Name.ValueString()
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	} else {
		delete(doc, "description")
	}
	doc["runtimeVersion"] = m.RuntimeVersion.ValueString()
	if m.Timeout.IsNull() || m.Timeout.IsUnknown() {
		delete(doc, "timeout")
	} else {
		doc["timeout"] = m.Timeout.ValueInt64()
	}
	if m.RunAsUser.IsNull() || m.RunAsUser.IsUnknown() {
		delete(doc, "runAsUser")
	} else {
		doc["runAsUser"] = m.RunAsUser.ValueBool()
	}
	if v := strOrEmpty(m.CronString); v != "" {
		doc["cronString"] = v
	} else {
		delete(doc, "cronString")
	}
}

type botDoc struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	RuntimeVersion string `json:"runtimeVersion"`
	Timeout        *int64 `json:"timeout"`
	RunAsUser      *bool  `json:"runAsUser"`
	CronString     string `json:"cronString"`
	Meta           struct {
		Project string `json:"project"`
	} `json:"meta"`
	ExecutableCode struct {
		URL string `json:"url"`
	} `json:"executableCode"`
}

// fromDoc maps server Bot fields into the model. Code, SourcePath, SourceHash,
// AccessPolicy and MembershipID are managed by the callers.
func (m *botModel) fromDoc(doc botDoc) {
	m.ID = types.StringValue(doc.ID)
	m.Ref = refValue("Bot", doc.ID)
	m.Name = types.StringValue(doc.Name)
	m.Description = optString(doc.Description)
	m.RuntimeVersion = optString(doc.RuntimeVersion)
	if doc.Timeout != nil {
		m.Timeout = types.Int64Value(*doc.Timeout)
	} else {
		m.Timeout = types.Int64Null()
	}
	if doc.RunAsUser != nil {
		m.RunAsUser = types.BoolValue(*doc.RunAsUser)
	} else {
		m.RunAsUser = types.BoolNull()
	}
	m.CronString = optString(doc.CronString)
	m.ProjectID = types.StringValue(doc.Meta.Project)
}

func NewBotResource() resource.Resource { return &botResource{} }

type botResource struct{ data *providerData }

func (r *botResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_bot"
}

func (r *botResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum Bot with its full lifecycle: creation (Bot + ProjectMembership via the project-admin endpoint), live code deployment ($deploy, no server restart), and per-bot runtime selection. The bundled code never enters Terraform state; only its SHA-256 (source_hash) is stored.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"ref": schema.StringAttribute{
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Full FHIR reference to this resource, e.g. Bot/abc. Use it wherever another resource takes a reference.",
			},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"code": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Inline bundled JavaScript (CommonJS: `exports.handler = async (medplum, event) => ...`). Exactly one of code / source_path. Prefer source_path for anything beyond trivial bots — inline code is stored in state.",
			},
			"source_path": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Path to the bundled single-file JavaScript (e.g. an esbuild output). Must exist at plan time. Exactly one of code / source_path. The file content is deployed but never stored in state.",
			},
			"source_hash": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "SHA-256 of the deployed bundle. Changes when the local bundle or the server-side deployed code changes, driving re-deploys.",
			},
			"runtime_version": schema.StringAttribute{
				Optional: true, Computed: true,
				Default:             stringdefault.StaticString("vmcontext"),
				MarkdownDescription: "Bot runtime: vmcontext, fission, or awslambda. Default vmcontext. Must be enabled in the provider's supported_bot_runtimes.",
			},
			"timeout":     schema.Int64Attribute{Optional: true, MarkdownDescription: "Execution timeout in seconds."},
			"run_as_user": schema.BoolAttribute{Optional: true, MarkdownDescription: "Run as the invoking user instead of the bot's own identity."},
			"cron_string": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Cron schedule for the bot, as a standard 5-field expression " +
					"(minute hour day-of-month month day-of-week), evaluated in UTC — e.g. `0 2 * * *` " +
					"for 02:00 daily. Requires the `cron` feature enabled on the Medplum Project; without " +
					"it the schedule is stored on the Bot but never runs. An invalid expression is rejected " +
					"at plan time.",
			},
			"access_policy": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "AccessPolicy reference for the bot's ProjectMembership, e.g. AccessPolicy/abc.",
			},
			"admin": schema.BoolAttribute{
				Optional: true, Computed: true,
				Default: booldefault.StaticBool(false),
				MarkdownDescription: "Make the bot's ProjectMembership a project admin (default false). " +
					"ProjectMembership, Project, and User are project-admin-only resource types in Medplum, " +
					"so bots that must write them — e.g. a group→AccessPolicy mapper writing membership.access[] — " +
					"need an admin membership; no ordinary AccessPolicy can grant that. Reads reflect the live " +
					"membership.admin, so out-of-band changes surface as drift.",
			},
			"project_id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Project the bot lives in. Always the provider session's project: Medplum creates bots in the authenticated project.",
			},
			"membership_id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				MarkdownDescription: "Id of the bot's ProjectMembership (created by the server together with the bot; access_policy lives here).",
			},
		},
	}
}

func (r *botResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *botResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m botModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	codeSet := !m.Code.IsNull()
	pathSet := !m.SourcePath.IsNull()
	if codeSet && pathSet {
		resp.Diagnostics.AddAttributeError(path.Root("code"), "Conflicting bot code", "Exactly one of code or source_path must be set, not both.")
	}
	if !codeSet && !pathSet {
		resp.Diagnostics.AddAttributeError(path.Root("code"), "Missing bot code", "Exactly one of code or source_path must be set.")
	}
	if rv := strOrEmpty(m.RuntimeVersion); rv != "" && !slices.Contains(botRuntimes, rv) {
		resp.Diagnostics.AddAttributeError(path.Root("runtime_version"), "Invalid runtime_version",
			fmt.Sprintf("%q is not a Medplum bot runtime; must be one of %s.", rv, strings.Join(botRuntimes, ", ")))
	}
	if cs := m.CronString; !cs.IsNull() && !cs.IsUnknown() {
		if err := validateCronString(cs.ValueString()); err != nil {
			resp.Diagnostics.AddAttributeError(path.Root("cron_string"), "Invalid cron_string",
				fmt.Sprintf("%q is not a valid 5-field cron expression: %v", cs.ValueString(), err))
		}
	}
}

func (r *botResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return // destroy plan
	}
	var plan botModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Gate the requested runtime on the environment's declared capabilities so
	// e.g. "fission" fails at plan time, not at first execution. Runs here (not
	// ValidateConfig) because provider data is only available after Configure.
	if r.data != nil && !plan.RuntimeVersion.IsNull() && !plan.RuntimeVersion.IsUnknown() {
		rv := plan.RuntimeVersion.ValueString()
		if !slices.Contains(r.data.SupportedBotRuntimes, rv) {
			resp.Diagnostics.AddAttributeError(path.Root("runtime_version"),
				"Bot runtime not supported by this environment",
				fmt.Sprintf("runtime_version %q is not in the provider's supported_bot_runtimes (%s). Add it to the provider configuration once the environment supports it.",
					rv, strings.Join(r.data.SupportedBotRuntimes, ", ")))
			return
		}
	}

	// Plan the code hash so the plan shows a diff whenever the bundle changes.
	code, ok, err := plan.resolveCode()
	switch {
	case err != nil:
		resp.Diagnostics.AddAttributeError(path.Root("source_path"), "Cannot read bot code", err.Error())
	case ok:
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("source_hash"), sourceHashOf(code))...)
	default:
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("source_hash"), types.StringUnknown())...)
	}
}

func (r *botResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan botModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	code, ok, err := plan.resolveCode()
	if err != nil || !ok {
		resp.Diagnostics.AddError("Cannot read bot code", fmt.Sprintf("resolving code/source_path: %v", err))
		return
	}

	// Bots are always created in the authenticated session's project.
	projectID, err := r.data.Client.CurrentProjectID(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Project discovery failed", err.Error())
		return
	}

	body, err := plan.adminCreateBody()
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.AdminCreateBot(ctx, projectID, body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &created); err != nil || created.ID == "" {
		resp.Diagnostics.AddError("Create failed", fmt.Sprintf("no bot id in response: %s", out))
		return
	}
	plan.ID = types.StringValue(created.ID)
	plan.Ref = refValue("Bot", created.ID)
	plan.ProjectID = types.StringValue(projectID)

	// The server creates the ProjectMembership together with the Bot, so look
	// it up now — before the taint snapshot below — instead of at the end of
	// Create. That way, if a later step (e.g. $deploy) fails, the tainted
	// resource still carries membership_id and destroy can clean up the
	// membership instead of orphaning it. A lookup failure here must not
	// abort before the snapshot: record it and surface it right after.
	mid, midErr := r.findMembershipID(ctx, created.ID)
	if midErr == nil && mid != "" {
		plan.MembershipID = types.StringValue(mid)
	} else {
		plan.MembershipID = types.StringNull()
	}

	// Track the resource as soon as it exists so a failure below leaves it
	// managed (tainted) instead of orphaned.
	plan.SourceHash = types.StringNull()
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if midErr != nil {
		resp.Diagnostics.AddError("Membership lookup failed", midErr.Error())
		return
	}
	if mid == "" {
		resp.Diagnostics.AddError("Membership lookup failed", "server did not create a ProjectMembership for the bot")
		return
	}

	// timeout / run_as_user are not part of the admin create contract — apply
	// them with a read-modify-write PUT that preserves sourceCode et al.
	if !plan.Timeout.IsNull() || !plan.RunAsUser.IsNull() || !plan.CronString.IsNull() {
		if err := r.updateBotFields(ctx, created.ID, plan); err != nil {
			resp.Diagnostics.AddError("Update failed", err.Error())
			return
		}
	}

	// admin lives on the ProjectMembership the server just created; the admin
	// create contract has no slot for it, so promote with a follow-up PUT.
	if plan.Admin.ValueBool() {
		if err := r.updateMembershipAdmin(ctx, mid, true); err != nil {
			resp.Diagnostics.AddError("Membership update failed", err.Error())
			return
		}
	}

	if err := r.deploy(ctx, created.ID, code); err != nil {
		resp.Diagnostics.AddError("Deploy failed", err.Error())
		return
	}
	plan.SourceHash = types.StringValue(sourceHashOf(code))

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *botResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m botModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "Bot", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	var doc botDoc
	if err := json.Unmarshal(out, &doc); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	m.fromDoc(doc)

	// Recompute source_hash from the live bundle so out-of-band deploys show
	// up as drift and get reverted on the next apply.
	if doc.ExecutableCode.URL == "" {
		m.SourceHash = types.StringNull()
	} else if codeBytes, err := r.fetchDeployedCode(ctx, doc.ExecutableCode.URL); err != nil {
		if !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Reading deployed bot code failed", err.Error())
			return
		}
		// The deployed Binary was deleted out-of-band; treat it as drift
		// (same as an empty executableCode.url) rather than a hard error, so
		// it gets repaired by a redeploy on the next apply instead of wedging
		// every refresh.
		m.SourceHash = types.StringNull()
	} else {
		m.SourceHash = types.StringValue(sourceHashOf(string(codeBytes)))
	}

	mid := strOrEmpty(m.MembershipID)
	if mid == "" { // import path: membership not in state yet
		mid, err = r.findMembershipID(ctx, m.ID.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Membership lookup failed", err.Error())
			return
		}
	}
	var memOut []byte
	if mid != "" {
		memOut, err = r.data.Client.FHIRRead(ctx, "ProjectMembership", mid)
		if err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Read failed", err.Error())
			return
		}
	}
	if memOut == nil {
		// The membership was deleted out-of-band; the bot cannot act without
		// one, so treat the whole resource as gone and recreate both on apply.
		// (A computed membership_id flipping to null would produce no diff.)
		resp.State.RemoveResource(ctx)
		return
	}
	var mem struct {
		ID           string `json:"id"`
		AccessPolicy struct {
			Reference string `json:"reference"`
		} `json:"accessPolicy"`
		Admin *bool `json:"admin"`
	}
	if err := json.Unmarshal(memOut, &mem); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	m.MembershipID = types.StringValue(mem.ID)
	m.AccessPolicy = optString(mem.AccessPolicy.Reference)
	// Absent admin means false (the schema default), so an out-of-band
	// promotion/demotion shows up as drift.
	m.Admin = types.BoolValue(mem.Admin != nil && *mem.Admin)

	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *botResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state botModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	plan.ID = state.ID
	plan.Ref = state.Ref
	plan.ProjectID = state.ProjectID
	plan.MembershipID = state.MembershipID

	if err := r.updateBotFields(ctx, id, plan); err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}

	code, ok, err := plan.resolveCode()
	if err != nil || !ok {
		resp.Diagnostics.AddError("Cannot read bot code", fmt.Sprintf("resolving code/source_path: %v", err))
		return
	}
	newHash := sourceHashOf(code)
	// Re-deploy when the bundle or the runtime changed (a runtime switch must
	// re-push the code to the new runtime, e.g. Fission).
	if newHash != strOrEmpty(state.SourceHash) || strOrEmpty(plan.RuntimeVersion) != strOrEmpty(state.RuntimeVersion) {
		if err := r.deploy(ctx, id, code); err != nil {
			resp.Diagnostics.AddError("Deploy failed", err.Error())
			return
		}
	}
	plan.SourceHash = types.StringValue(newHash)

	if strOrEmpty(plan.AccessPolicy) != strOrEmpty(state.AccessPolicy) {
		if err := r.updateMembershipAccessPolicy(ctx, state.MembershipID.ValueString(), strOrEmpty(plan.AccessPolicy)); err != nil {
			resp.Diagnostics.AddError("Membership update failed", err.Error())
			return
		}
	}

	// admin: plan is always known (schema default false); a null state value
	// (resource created before the attribute existed) reads as false.
	if plan.Admin.ValueBool() != (!state.Admin.IsNull() && state.Admin.ValueBool()) {
		if err := r.updateMembershipAdmin(ctx, state.MembershipID.ValueString(), plan.Admin.ValueBool()); err != nil {
			resp.Diagnostics.AddError("Membership update failed", err.Error())
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *botResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m botModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Membership first, via plain FHIR DELETE (never the admin /members
	// endpoint, which cascade-deletes), then the Bot. The deployed Binary is
	// left behind; that is harmless.
	if mid := strOrEmpty(m.MembershipID); mid != "" {
		if err := r.data.Client.FHIRDelete(ctx, "ProjectMembership", mid); err != nil && !client.IsNotFound(err) {
			resp.Diagnostics.AddError("Membership delete failed", err.Error())
			return
		}
	}
	if err := r.data.Client.FHIRDelete(ctx, "Bot", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *botResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

// deploy pushes the bundled code live via POST Bot/{id}/$deploy. The body is
// raw JSON {code, filename}, not FHIR Parameters (server deploy.ts reads
// req.body.code directly).
func (r *botResource) deploy(ctx context.Context, id, code string) error {
	body, err := json.Marshal(map[string]string{"code": code, "filename": "index.js"})
	if err != nil {
		return err
	}
	_, err = r.data.Client.Operation(ctx, "Bot", id, "$deploy", body)
	return err
}

// updateBotFields read-modify-writes the Bot so unmodeled fields (sourceCode,
// executableCode, ...) survive the PUT.
func (r *botResource) updateBotFields(ctx context.Context, id string, m botModel) error {
	cur, err := r.data.Client.FHIRRead(ctx, "Bot", id)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(cur, &doc); err != nil {
		return err
	}
	m.applyBotFields(doc)
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = r.data.Client.FHIRUpdate(ctx, "Bot", id, body)
	return err
}

// findMembershipID locates the bot's implicit ProjectMembership. Returns ""
// (no error) when none exists.
func (r *botResource) findMembershipID(ctx context.Context, botID string) (string, error) {
	out, err := r.data.Client.FHIRSearch(ctx, "ProjectMembership", "profile=Bot/"+botID)
	if err != nil {
		return "", err
	}
	var bundle struct {
		Entry []struct {
			Resource struct {
				ID string `json:"id"`
			} `json:"resource"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(out, &bundle); err != nil {
		return "", err
	}
	if len(bundle.Entry) == 0 {
		return "", nil
	}
	return bundle.Entry[0].Resource.ID, nil
}

func (r *botResource) updateMembershipAccessPolicy(ctx context.Context, membershipID, policyRef string) error {
	cur, err := r.data.Client.FHIRRead(ctx, "ProjectMembership", membershipID)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(cur, &doc); err != nil {
		return err
	}
	if policyRef == "" {
		delete(doc, "accessPolicy")
	} else {
		doc["accessPolicy"] = refObj(policyRef)
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = r.data.Client.FHIRUpdate(ctx, "ProjectMembership", membershipID, body)
	return err
}

// updateMembershipAdmin read-modify-writes ProjectMembership.admin. false is
// written as an absent field (Medplum's own default), not admin:false.
func (r *botResource) updateMembershipAdmin(ctx context.Context, membershipID string, admin bool) error {
	cur, err := r.data.Client.FHIRRead(ctx, "ProjectMembership", membershipID)
	if err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(cur, &doc); err != nil {
		return err
	}
	if admin {
		doc["admin"] = true
	} else {
		delete(doc, "admin")
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	_, err = r.data.Client.FHIRUpdate(ctx, "ProjectMembership", membershipID, body)
	return err
}

// fetchDeployedCode fetches the bot's deployed bundle for drift detection.
// Prefers an authenticated raw Binary read; falls back to plain GET for a
// presigned URL whose Binary id cannot be recovered.
func (r *botResource) fetchDeployedCode(ctx context.Context, execURL string) ([]byte, error) {
	if id := binaryIDFromURL(execURL); id != "" {
		return r.data.Client.FHIRReadBinaryContent(ctx, id)
	}
	if strings.HasPrefix(execURL, "http://") || strings.HasPrefix(execURL, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, execURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req) // presigned: no auth required
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("fetch deployed code from %s: HTTP %d", execURL, resp.StatusCode)
		}
		return io.ReadAll(resp.Body)
	}
	return nil, fmt.Errorf("cannot resolve executableCode.url %q to a Binary", execURL)
}

var (
	_ resource.Resource                   = (*botResource)(nil)
	_ resource.ResourceWithConfigure      = (*botResource)(nil)
	_ resource.ResourceWithImportState    = (*botResource)(nil)
	_ resource.ResourceWithValidateConfig = (*botResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*botResource)(nil)
)
