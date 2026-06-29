package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirjson"
)

// semanticJSONBody returns a plan modifier that keeps the prior state value when
// the planned FHIR body is already satisfied by it — i.e. the config is a subset
// of the stored body (ignoring key order, server-managed meta, and extra
// server-added fields). This mirrors the Read drift check (fhirjson.Contains)
// and, in particular, stops `terraform import` from producing a permanent diff:
// after import the state holds the full server body (meta.project, meta.author,
// narrative text, server defaults, ...) while config holds only the user's
// desired subset.
func semanticJSONBody() planmodifier.String { return semanticJSONModifier{} }

type semanticJSONModifier struct{}

func (m semanticJSONModifier) Description(context.Context) string {
	return "Suppress diffs when the FHIR body is semantically unchanged."
}

func (m semanticJSONModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m semanticJSONModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	// Suppress the diff when the planned config (PlanValue) is contained in the
	// stored body (StateValue) — the server already satisfies the desired
	// config. Using Contains rather than Equal lets the full server body held in
	// state after import match the user's smaller config subset.
	contained, err := fhirjson.Contains([]byte(req.PlanValue.ValueString()), []byte(req.StateValue.ValueString()))
	if err == nil && contained {
		resp.PlanValue = req.StateValue
	}
}

// serverManagedMeta returns a plan modifier for server-assigned metadata
// (version_id, last_updated) that Medplum regenerates on every write. It keeps
// the prior state value only when the FHIR body is semantically unchanged — so a
// no-op plan or `terraform import` doesn't show the value flipping to "(known
// after apply)" as a spurious diff.
//
// Crucially, when the body actually changes it leaves the value unknown ("known
// after apply"). Medplum will assign a fresh versionId/lastUpdated on the write,
// so pinning the old state value here would make the post-apply result diverge
// from the plan and trip Terraform's "Provider produced inconsistent result
// after apply" error. This replaces a plain UseStateForUnknown, which pinned the
// stale value unconditionally.
func serverManagedMeta() planmodifier.String { return serverManagedMetaModifier{} }

type serverManagedMetaModifier struct{}

func (m serverManagedMetaModifier) Description(context.Context) string {
	return "Hold prior server metadata only when the FHIR body is semantically unchanged; otherwise leave unknown."
}

func (m serverManagedMetaModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m serverManagedMetaModifier) PlanModifyString(ctx context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	// Create (no prior state) or destroy: nothing to carry forward; leave the
	// framework default (unknown / null).
	if req.StateValue.IsNull() || req.StateValue.IsUnknown() {
		return
	}

	var planBody, stateBody types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("body"), &planBody)...)
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("body"), &stateBody)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if planBody.IsNull() || planBody.IsUnknown() || stateBody.IsNull() || stateBody.IsUnknown() {
		return
	}

	if keepServerMeta(planBody.ValueString(), stateBody.ValueString()) {
		resp.PlanValue = req.StateValue
	}
}

// keepServerMeta reports whether server-managed metadata may safely be held at
// its prior state value: true only when the planned body is already satisfied by
// the stored body (no rewrite, so the server keeps the same versionId/
// lastUpdated). This mirrors the semanticJSONBody / Read drift check. It fails
// safe to false on malformed JSON, leaving the metadata "known after apply".
func keepServerMeta(planBody, stateBody string) bool {
	contained, err := fhirjson.Contains([]byte(planBody), []byte(stateBody))
	return err == nil && contained
}
