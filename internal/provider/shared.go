package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// strOrEmpty returns "" for null/unknown, else the string value.
func strOrEmpty(s types.String) string {
	if s.IsNull() || s.IsUnknown() {
		return ""
	}
	return s.ValueString()
}

// optString returns a types.String that is null when v is empty, else the value.
// Used in Read so absent server fields become null (not "") and don't churn the plan.
func optString(v string) types.String {
	if v == "" {
		return types.StringNull()
	}
	return types.StringValue(v)
}
