package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"

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
