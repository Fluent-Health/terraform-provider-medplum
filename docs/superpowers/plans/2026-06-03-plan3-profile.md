# Medplum Terraform Provider — Plan 3: `medplum_fhir_profile`

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A `medplum_fhir_profile` resource that applies a compiled `StructureDefinition` to Medplum (reusing the existing subset-containment drift model) and, at plan time, classifies every construct as REJECT / WARN / ENFORCED against what Medplum actually enforces — failing loud on inert constructs instead of letting Medplum fail silent.

**Architecture:** A pure `internal/fhirprofile.Analyze(sdJSON)` validator (no network) encodes the spike-verified, version-pinned Medplum support matrix and returns a `Report`. A thin `medplum_fhir_profile` resource does FHIR CRUD on `StructureDefinition` reusing `client` + `fhirjson.Contains` (drift) + `fhirschema` (base R4 validation); its `ModifyPlan` runs `Analyze`, emits diagnostics (errors for REJECT, warnings for WARN, a summary of enforced vs decorative), and escalates everything to errors under `strict`.

**Tech Stack:** Go 1.22+, terraform-plugin-framework, the Plan-1/2 `internal/{client,fhirjson,fhirschema}`.

**Inputs:** `docs/superpowers/specs/2026-06-03-plan3-profile-design.md` + `...-plan3-profile-spike-findings.md`. Builds on merged Plans 1 & 2.

**Conventions (every task):** TDD; before each commit `gofmt -w . && go vet ./... && go test ./... -count=1`; Conventional Commits with footer `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>` via `git -c commit.gpgsign=false commit`. Acceptance tests run only under `TF_ACC=1` (skip otherwise) and reuse `testAccProtoV6ProviderFactories`/`testAccPreCheck`.

**Scope:** Phases 1 (resource) + 2 (validator). Phase 3 (FSH→SD compile + IG Publisher) is CI-only, out of the provider — not in this plan.

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `internal/fhirprofile/analyze.go` | Pure validator: `Analyze(sdJSON) (Report, error)` + `Report`/`Finding` types + summary. |
| `internal/fhirprofile/analyze_test.go` | Table-driven unit tests over crafted SDs (every rule). |
| `internal/provider/fhir_profile_resource.go` | `medplum_fhir_profile` resource (CRUD + drift + ModifyPlan gate). |
| `internal/provider/fhir_profile_resource_test.go` | Unit + acceptance tests. |
| `internal/provider/provider.go` | Register the resource. |

Reused: `client.FHIR*`/`IsNotFound`, `fhirjson.Contains`, `fhirschema.New().Validate`, `extractMeta` (package `provider`).

---

## Task 0: Branch

- [ ] **Step 1**

Run:
```bash
cd /home/ivan/Developer/terraform-provider-medplum
git checkout main && git pull --ff-only && git checkout -b feat/plan-3-fhir-profile
go test ./... -count=1
```
Expected: branch created; baseline green.

---

## Task 1: `internal/fhirprofile` validator

**Files:** Create `internal/fhirprofile/analyze.go`, `internal/fhirprofile/analyze_test.go`.

The classifier walks `snapshot.element[]` once. Rules are the spike-verified matrix.

- [ ] **Step 1: Write the failing tests**

`internal/fhirprofile/analyze_test.go`:

```go
package fhirprofile

import "testing"

// helper: an SD with the given snapshot elements (as a raw JSON array string).
func sdWith(elementsJSON string) []byte {
	return []byte(`{"resourceType":"StructureDefinition","url":"http://x/p","snapshot":{"element":` + elementsJSON + `}}`)
}

func TestAnalyze_EmptySnapshot_Rejects(t *testing.T) {
	r, err := Analyze([]byte(`{"resourceType":"StructureDefinition","url":"http://x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rejects()) != 1 {
		t.Fatalf("want 1 reject, got %d (%+v)", len(r.Rejects()), r.Findings)
	}
}

func TestAnalyze_BadDiscriminatorType_Rejects(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.identifier","path":"Patient.identifier","slicing":{"rules":"open","discriminator":[{"type":"exists","path":"system"}]}}
	]`))
	if len(r.Rejects()) != 1 {
		t.Fatalf("want reject for 'exists' discriminator, got %+v", r.Findings)
	}
}

func TestAnalyze_FHIRPathDiscriminatorPath_Rejects(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.extension","path":"Patient.extension","slicing":{"rules":"open","discriminator":[{"type":"value","path":"resolve()"}]}}
	]`))
	if len(r.Rejects()) != 1 {
		t.Fatalf("want reject for FHIRPath discriminator path, got %+v", r.Findings)
	}
}

func TestAnalyze_ExtensionSliceMissingFixedURL_Rejects(t *testing.T) {
	// extension slice with NO `.url` fixed child
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.extension:race","path":"Patient.extension","sliceName":"race","min":1,"max":"1"}
	]`))
	if len(r.Rejects()) != 1 {
		t.Fatalf("want reject for extension slice missing fixed url, got %+v", r.Findings)
	}
}

func TestAnalyze_ExtensionSliceWithFixedURL_Enforced(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.extension:race","path":"Patient.extension","sliceName":"race","min":1,"max":"1"},
	  {"id":"Patient.extension:race.url","path":"Patient.extension.url","fixedUri":"http://x/race"}
	]`))
	if len(r.Rejects()) != 0 {
		t.Fatalf("want no rejects, got %+v", r.Findings)
	}
	if r.EnforcedCount == 0 {
		t.Fatal("want extension presence/cardinality counted as enforced")
	}
}

func TestAnalyze_ClosedSlicing_Warns(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.identifier","path":"Patient.identifier","slicing":{"rules":"closed","discriminator":[{"type":"value","path":"system"}]}}
	]`))
	if len(r.Warns()) == 0 {
		t.Fatalf("want warn for closed slicing, got %+v", r.Findings)
	}
}

func TestAnalyze_EnforcedCardinalityAndFixed(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.active","path":"Patient.active","min":1,"max":"1"},
	  {"id":"Patient.gender","path":"Patient.gender","fixedCode":"female"}
	]`))
	if r.EnforcedCount < 2 {
		t.Fatalf("want >=2 enforced (cardinality + fixed), got %d", r.EnforcedCount)
	}
	if len(r.Rejects()) != 0 {
		t.Fatalf("unexpected rejects: %+v", r.Findings)
	}
}

func TestAnalyze_DecorativeOnly_NoEnforced(t *testing.T) {
	// mustSupport + required binding only → zero enforced
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.maritalStatus","path":"Patient.maritalStatus","mustSupport":true,"binding":{"strength":"required","valueSet":"http://x/vs"}}
	]`))
	if r.EnforcedCount != 0 {
		t.Fatalf("want 0 enforced, got %d", r.EnforcedCount)
	}
	if r.DecorativeCount == 0 {
		t.Fatal("want decorative signals counted")
	}
}

func TestAnalyze_InvalidJSON(t *testing.T) {
	if _, err := Analyze([]byte(`{bad`)); err == nil {
		t.Fatal("want error on invalid json")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/fhirprofile/... -v`
Expected: FAIL — package/`Analyze` undefined.

- [ ] **Step 3: Implement `internal/fhirprofile/analyze.go`**

```go
// Package fhirprofile statically classifies a FHIR R4 StructureDefinition against
// what Medplum actually enforces (verified vs @medplum/core v5.1.14). It reports
// REJECT (inert/throws in Medplum), WARN (accepted but silently unenforced), and
// counts ENFORCED constraints, so a profile that constrains nothing can fail loud.
package fhirprofile

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Severity classifies a finding.
type Severity int

const (
	// SeverityReject: Medplum would treat the construct as inert or throw on load.
	SeverityReject Severity = iota
	// SeverityWarn: Medplum accepts it but silently does not enforce it.
	SeverityWarn
)

// Finding is one classified construct.
type Finding struct {
	Severity Severity
	Path     string // element id/path
	Message  string
}

// Report is the result of analyzing one StructureDefinition.
type Report struct {
	Findings        []Finding
	EnforcedCount   int // number of genuinely-enforced constraints
	DecorativeCount int // count of decorative signals (mustSupport, required-binding, targetProfile, constraint)
}

// Rejects returns the REJECT findings.
func (r Report) Rejects() []Finding { return r.bySeverity(SeverityReject) }

// Warns returns the WARN findings.
func (r Report) Warns() []Finding { return r.bySeverity(SeverityWarn) }

func (r Report) bySeverity(s Severity) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Severity == s {
			out = append(out, f)
		}
	}
	return out
}

// Summary is a one-line human summary for the plan output.
func (r Report) Summary() string {
	return fmt.Sprintf("%d enforced constraint(s), %d decorative signal(s), %d reject(s), %d warning(s)",
		r.EnforcedCount, r.DecorativeCount, len(r.Rejects()), len(r.Warns()))
}

type sdElement struct {
	ID          string `json:"id"`
	Path        string `json:"path"`
	SliceName   string `json:"sliceName"`
	Min         *int   `json:"min"`
	Max         string `json:"max"`
	MustSupport *bool  `json:"mustSupport"`
	Type        []struct {
		Code          string   `json:"code"`
		TargetProfile []string `json:"targetProfile"`
	} `json:"type"`
	Binding *struct {
		Strength string `json:"strength"`
		ValueSet string `json:"valueSet"`
	} `json:"binding"`
	Slicing *struct {
		Rules         string `json:"rules"`
		Ordered       *bool  `json:"ordered"`
		Discriminator []struct {
			Type string `json:"type"`
			Path string `json:"path"`
		} `json:"discriminator"`
	} `json:"slicing"`
	Constraint []json.RawMessage `json:"constraint"`
}

func hasFixedOrPattern(raw map[string]json.RawMessage) bool {
	for k := range raw {
		if strings.HasPrefix(k, "fixed") || strings.HasPrefix(k, "pattern") {
			return true
		}
	}
	return false
}

// Analyze classifies the SD's snapshot elements. It returns an error only for
// malformed JSON; classification problems are returned as Findings.
func Analyze(sdJSON []byte) (Report, error) {
	var doc struct {
		Snapshot struct {
			Element []json.RawMessage `json:"element"`
		} `json:"snapshot"`
	}
	if err := json.Unmarshal(sdJSON, &doc); err != nil {
		return Report{}, fmt.Errorf("invalid StructureDefinition JSON: %w", err)
	}

	var rep Report
	if len(doc.Snapshot.Element) == 0 {
		rep.Findings = append(rep.Findings, Finding{SeverityReject, "snapshot",
			"snapshot.element is empty: Medplum validates snapshot-only and treats a snapshot-less profile as inert"})
		return rep, nil
	}

	type parsedEl struct {
		el  sdElement
		raw map[string]json.RawMessage
	}
	all := make([]parsedEl, 0, len(doc.Snapshot.Element))
	fixedURLChild := map[string]bool{} // parent element id -> has a fixed/pattern `.url` child
	for _, rawEl := range doc.Snapshot.Element {
		var el sdElement
		if err := json.Unmarshal(rawEl, &el); err != nil {
			return Report{}, fmt.Errorf("invalid element: %w", err)
		}
		var m map[string]json.RawMessage
		_ = json.Unmarshal(rawEl, &m)
		all = append(all, parsedEl{el, m})
		if strings.HasSuffix(el.ID, ".url") && hasFixedOrPattern(m) {
			fixedURLChild[strings.TrimSuffix(el.ID, ".url")] = true
		}
	}

	for _, p := range all {
		el := p.el

		// --- Slicing discriminators ---
		if el.Slicing != nil {
			for _, d := range el.Slicing.Discriminator {
				if d.Type != "value" && d.Type != "pattern" && d.Type != "type" {
					rep.reject(el.ID, fmt.Sprintf("unsupported slicing discriminator type %q: Medplum throws on load (only value/pattern/type)", d.Type))
				}
				if d.Path != "$this" && strings.ContainsAny(d.Path, "()") {
					rep.reject(el.ID, fmt.Sprintf("discriminator path %q uses FHIRPath functions: Medplum resolves only dotted paths + $this, so this slice never matches", d.Path))
				}
			}
			if r := el.Slicing.Rules; r == "closed" || r == "openAtEnd" {
				rep.warn(el.ID, fmt.Sprintf("slicing rule %q is parsed but NOT enforced by Medplum (all slicing behaves as open)", r))
			}
			if el.Slicing.Ordered != nil && *el.Slicing.Ordered {
				rep.warn(el.ID, "slicing 'ordered' is not enforced by Medplum")
			}
		}

		// --- Extension slices ---
		isExtSlice := el.SliceName != "" && strings.HasSuffix(el.Path, ".extension")
		if isExtSlice {
			if fixedURLChild[el.ID] {
				rep.EnforcedCount++ // extension presence + cardinality keyed by url IS enforced
			} else {
				rep.reject(el.ID, "extension slice has no fixed 'url' child: Medplum matches extension entries by url.fixed, so this slice never matches")
			}
		}

		// --- Enforced constraints ---
		if (el.Min != nil && *el.Min > 0) || (el.Max != "" && el.Max != "*") {
			rep.EnforcedCount++ // cardinality / required presence
		}
		// fixed[x]/pattern[x] on a non-url element (url fixed already counted via the extension slice)
		if hasFixedOrPattern(p.raw) && !strings.HasSuffix(el.ID, ".url") {
			rep.EnforcedCount++
		}

		// --- Decorative signals (parsed by Medplum, not enforced) ---
		if el.MustSupport != nil && *el.MustSupport {
			rep.DecorativeCount++
		}
		if el.Binding != nil && el.Binding.Strength == "required" && el.Binding.ValueSet != "" {
			rep.DecorativeCount++
		}
		for _, t := range el.Type {
			if len(t.TargetProfile) > 0 {
				rep.DecorativeCount++
				break
			}
		}
		if len(el.Constraint) > 0 {
			rep.DecorativeCount++
		}
	}
	return rep, nil
}

func (r *Report) reject(path, msg string) {
	r.Findings = append(r.Findings, Finding{SeverityReject, path, msg})
}

func (r *Report) warn(path, msg string) {
	r.Findings = append(r.Findings, Finding{SeverityWarn, path, msg})
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/fhirprofile/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/fhirprofile
git -c commit.gpgsign=false commit -m "feat(fhirprofile): Medplum-context useful-profile validator"
```

---

## Task 2: `medplum_fhir_profile` resource

**Files:** Create `internal/provider/fhir_profile_resource.go`; modify `internal/provider/provider.go`.

Reuses the generic resource's body+`Contains` pattern, specialized to `StructureDefinition`, plus the validator gate.

- [ ] **Step 1: Create `internal/provider/fhir_profile_resource.go`**

```go
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
	r.apply(ctx, &m, out, &resp.Diagnostics, &resp.State)
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
	r.apply(ctx, &plan, out, &resp.Diagnostics, &resp.State)
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

// apply sets computed fields from a create/update response and writes state.
func (r *fhirProfileResource) apply(ctx context.Context, m *fhirProfileModel, out []byte, diags *diagAppender, state stateSetter) {
	id, _, _, err := extractMeta(out)
	if err != nil || id == "" {
		diags.AddError("Invalid server response", "could not parse StructureDefinition id from response")
		return
	}
	r.setComputed(m, out)
	diags.Append(state.Set(ctx, m)...)
}

var (
	_ resource.Resource                   = (*fhirProfileResource)(nil)
	_ resource.ResourceWithConfigure      = (*fhirProfileResource)(nil)
	_ resource.ResourceWithImportState    = (*fhirProfileResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*fhirProfileResource)(nil)
)
```

NOTE on the `apply` helper signatures: the framework's `resp.State` is `tfsdk.State` and `resp.Diagnostics` is `diag.Diagnostics`. The pseudo-types `diagAppender`/`stateSetter` above are a shorthand — **do not** introduce new interfaces; instead inline `apply`'s body into `Create` and `Update` (they each have concrete `resp.State`/`resp.Diagnostics`). Concretely, replace the `r.apply(...)` calls with:
```go
	id, _, _, merr := extractMeta(out)
	if merr != nil || id == "" {
		resp.Diagnostics.AddError("Invalid server response", "could not parse StructureDefinition id from response")
		return
	}
	r.setComputed(&m, out)        // Create uses &m; Update uses &plan
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
```
and delete the `apply` method + the `diagAppender`/`stateSetter` shorthand. (This keeps the code framework-correct; the helper was only to avoid repetition in the plan listing.)

- [ ] **Step 2: Register in `internal/provider/provider.go`**

Add `NewFHIRProfileResource` to the `Resources()` slice (alongside the existing six constructors).

- [ ] **Step 3: Build + vet + unit tests**

Run: `gofmt -w . && go vet ./... && go build ./... && go test ./... -count=1`
Expected: clean; all existing tests pass.

- [ ] **Step 4: Commit**

```bash
git add internal/provider/fhir_profile_resource.go internal/provider/provider.go
git -c commit.gpgsign=false commit -m "feat(provider): medplum_fhir_profile resource with plan-time Medplum-context gate"
```

---

## Task 3: Acceptance + reject/strict tests

**Files:** Create `internal/provider/fhir_profile_resource_test.go`.

- [ ] **Step 1: Write the tests**

```go
package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// enforcedProfile is a minimal SD with one genuinely-enforced constraint
// (required cardinality) so the gate passes and drift is testable.
func enforcedProfile(url string) string {
	sd := fmt.Sprintf(`{
  "resourceType": "StructureDefinition",
  "url": %q,
  "name": "TfAccProfile",
  "status": "active",
  "kind": "resource",
  "abstract": false,
  "type": "Patient",
  "baseDefinition": "http://hl7.org/fhir/StructureDefinition/Patient",
  "derivation": "constraint",
  "snapshot": { "element": [
    { "id": "Patient", "path": "Patient" },
    { "id": "Patient.active", "path": "Patient.active", "min": 1, "max": "1" }
  ]}
}`, url)
	return fmt.Sprintf("resource \"medplum_fhir_profile\" \"test\" {\n  structure_definition = jsonencode(%s)\n}\n", "jsondecode("+jsonQuote(sd)+")")
}

// jsonQuote wraps a JSON document as a quoted HCL string for jsondecode().
func jsonQuote(s string) string { return fmt.Sprintf("%q", s) }

func TestAccFHIRProfile_basic(t *testing.T) {
	url := "http://example.com/fhir/StructureDefinition/tf-acc-" + acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := enforcedProfile(url)
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_fhir_profile.test", "id"),
					resource.TestCheckResourceAttr("medplum_fhir_profile.test", "url", url),
				),
			},
			{Config: cfg, PlanOnly: true},
			{ResourceName: "medplum_fhir_profile.test", ImportState: true, ImportStateVerify: true, ImportStateVerifyIgnore: []string{"structure_definition", "strict"}},
		},
	})
}

func TestAccFHIRProfile_rejectsEmptySnapshot(t *testing.T) {
	cfg := `resource "medplum_fhir_profile" "test" {
  structure_definition = jsonencode({
    resourceType  = "StructureDefinition"
    url           = "http://example.com/fhir/StructureDefinition/tf-acc-empty"
    name          = "Empty"
    status        = "active"
    kind          = "resource"
    abstract      = false
    type          = "Patient"
    baseDefinition = "http://hl7.org/fhir/StructureDefinition/Patient"
    derivation    = "constraint"
  })
}`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg, ExpectError: regexpMustMatch("snapshot")},
		},
	})
}
```

NOTE for the implementer: the `enforcedProfile`/`jsonQuote` helper above is fiddly — simplify it. The cleanest is to embed the SD JSON directly via Terraform `jsondecode("<escaped json>")` OR build the HCL with a raw heredoc. Use whichever compiles cleanly; the REQUIREMENT is: step 1 applies an SD whose only constraint is `Patient.active` min=1 (enforced, so the gate passes and a no-op plan is empty), step 2 asserts empty plan, step 3 imports (ignoring `structure_definition`/`strict`). For `ExpectError`, import `regexp` and use `regexp.MustCompile("snapshot")` (replace the `regexpMustMatch` placeholder with `regexp.MustCompile`). Drop `jsonQuote` if you inline the JSON.

- [ ] **Step 2: Verify compile + skip**

Run: `gofmt -w internal/provider && go vet ./... && go test ./internal/provider/... -run 'TestAccFHIRProfile' -v`
Expected: tests SKIP (no TF_ACC); package compiles. Fix the config helpers until it compiles cleanly.

- [ ] **Step 3: Commit**

```bash
git add internal/provider/fhir_profile_resource_test.go
git -c commit.gpgsign=false commit -m "test(provider): acceptance tests for medplum_fhir_profile (apply, reject empty snapshot)"
```

---

## Self-Review (completed during plan authoring)

**Spec coverage:**
- Drift via `Contains` → Task 2 Read (reuses `fhirjson.Contains`). ✓
- Reject empty `snapshot.element` → Task 1 (Analyze) + surfaced in Task 2 ModifyPlan. ✓
- Validator matrix (reject/warn/enforced) → Task 1 with the spike-verified rules. ✓
- Per-profile report + enforced/decorative counts → Task 1 `Report.Summary()`, surfaced in Task 2. ✓
- Warn-by-default + `strict` opt-in escalation → Task 2 ModifyPlan. ✓
- Extension SDs managed by same resource → no type restriction beyond `StructureDefinition`; validator handles extension slices. ✓
- Import, register → Tasks 2. ✓
- Acceptance (apply enforced / reject empty snapshot) → Task 3. ✓
- Phase 3 (FSH/IG) → out of scope (documented). ✓

**Placeholder scan:** The `apply`/`diagAppender`/`stateSetter` shorthand in Task 2 is explicitly flagged as NOT real code with the concrete inline replacement given — the implementer inlines it. The Task 3 config helper is flagged as fiddly with a precise requirement + the `regexp.MustCompile` fix. No other placeholders.

**Type consistency:** `fhirprofile.{Analyze, Report, Finding, Severity, SeverityReject, SeverityWarn}`, `Report.{Rejects,Warns,Summary,EnforcedCount,DecorativeCount}`, `fhirjson.Contains`, `client.IsNotFound`, `extractMeta` are defined once and used consistently. The resource model field `StructureDefinition`/`Strict`/`URL` names match between schema tags and usage.

**Known follow-ups (not placeholders):** validator matrix is version-pinned (re-verify on Medplum upgrade); FHIRPath `constraint` coverage is classified decorative pending a deeper trace; live empty-array round-trip confirmation (shared with the generic resource).
