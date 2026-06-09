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
