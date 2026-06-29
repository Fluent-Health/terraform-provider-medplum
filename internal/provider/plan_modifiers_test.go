package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// After import, state holds the full server body (server-managed meta, text,
// defaults); config holds only the user's subset. The modifier must suppress
// the diff so import does not leave a permanent change.
func TestSemanticJSONBody_SuppressesWhenConfigContainedInState(t *testing.T) {
	state := types.StringValue(`{"resourceType":"ValueSet","status":"active","id":"1","meta":{"project":"p","author":"a"},"text":{"status":"generated"}}`)
	plan := types.StringValue(`{"resourceType":"ValueSet","status":"active"}`)

	resp := &planmodifier.StringResponse{PlanValue: plan}
	semanticJSONModifier{}.PlanModifyString(
		context.Background(),
		planmodifier.StringRequest{StateValue: state, PlanValue: plan},
		resp,
	)
	if resp.PlanValue.ValueString() != state.ValueString() {
		t.Fatalf("expected diff suppressed (plan reset to state); got %q", resp.PlanValue.ValueString())
	}
}

// A config that adds a field the server lacks is a real change and must not be
// suppressed.
func TestSemanticJSONBody_KeepsDiffWhenConfigAddsField(t *testing.T) {
	state := types.StringValue(`{"resourceType":"ValueSet","status":"active"}`)
	plan := types.StringValue(`{"resourceType":"ValueSet","status":"active","url":"http://x"}`)

	resp := &planmodifier.StringResponse{PlanValue: plan}
	semanticJSONModifier{}.PlanModifyString(
		context.Background(),
		planmodifier.StringRequest{StateValue: state, PlanValue: plan},
		resp,
	)
	if resp.PlanValue.ValueString() != plan.ValueString() {
		t.Fatalf("expected diff preserved (plan unchanged); got %q", resp.PlanValue.ValueString())
	}
}

// Server-managed meta (version_id, last_updated) must be held at the prior state
// value only when the body is semantically unchanged — a no-op or post-import
// plan. Medplum keeps the same versionId/lastUpdated, so pinning is correct.
func TestKeepServerMeta_TrueWhenBodyUnchanged(t *testing.T) {
	state := `{"resourceType":"ValueSet","status":"active","meta":{"versionId":"v1"},"text":{"status":"generated"}}`
	plan := `{"resourceType":"ValueSet","status":"active"}`
	if !keepServerMeta(plan, state) {
		t.Fatal("expected meta held (body contained in state) but it was not")
	}
}

// When the body actually changes, Medplum reassigns versionId/lastUpdated. The
// meta must NOT be pinned to the old state value — otherwise the post-apply
// result is inconsistent with the plan ("Provider produced inconsistent result
// after apply"). This is the regression that failed the IG publish build.
func TestKeepServerMeta_FalseWhenBodyChanges(t *testing.T) {
	state := `{"resourceType":"ValueSet","status":"active"}`
	plan := `{"resourceType":"ValueSet","status":"draft"}`
	if keepServerMeta(plan, state) {
		t.Fatal("expected meta left unknown (body changed) but it was pinned to state")
	}
}

// Malformed JSON must not pin (fail safe to "known after apply").
func TestKeepServerMeta_FalseOnInvalidJSON(t *testing.T) {
	if keepServerMeta("{not json", `{"resourceType":"ValueSet"}`) {
		t.Fatal("expected meta left unknown on invalid JSON")
	}
}

// On create (null prior state) the modifier must leave the value untouched
// (unknown / known after apply) without reaching into the plan body.
func TestServerManagedMeta_LeavesUnknownOnCreate(t *testing.T) {
	unknown := types.StringUnknown()
	resp := &planmodifier.StringResponse{PlanValue: unknown}
	serverManagedMetaModifier{}.PlanModifyString(
		context.Background(),
		planmodifier.StringRequest{StateValue: types.StringNull(), PlanValue: unknown},
		resp,
	)
	if !resp.PlanValue.IsUnknown() {
		t.Fatalf("expected plan value to remain unknown on create; got %q", resp.PlanValue.ValueString())
	}
}
