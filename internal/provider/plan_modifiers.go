package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirjson"
)

// semanticJSONBody returns a plan modifier that keeps the prior state value when
// the planned FHIR body is semantically equal to it (ignoring key order and
// server-managed meta fields), so cosmetic reformatting does not produce a diff.
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
	eq, err := fhirjson.Equal([]byte(req.StateValue.ValueString()), []byte(req.PlanValue.ValueString()))
	if err == nil && eq {
		resp.PlanValue = req.StateValue
	}
}

// emptyListAsNull normalizes an explicit empty list ([]) to null at plan time so
// that `attr = []` and an omitted attr behave identically (Medplum strips empty
// arrays). Prevents "inconsistent result" for optional lists the server may drop.
func emptyListAsNull() planmodifier.List { return emptyListModifier{} }

type emptyListModifier struct{}

func (m emptyListModifier) Description(context.Context) string {
	return "Treat an empty list as null (Medplum strips empty arrays)."
}

func (m emptyListModifier) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (m emptyListModifier) PlanModifyList(ctx context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if req.PlanValue.IsNull() || req.PlanValue.IsUnknown() {
		return
	}
	if len(req.PlanValue.Elements()) == 0 {
		resp.PlanValue = types.ListNull(req.PlanValue.ElementType(ctx))
	}
}
