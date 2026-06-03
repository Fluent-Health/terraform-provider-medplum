package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/attr"
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

// listToStrings extracts a []string from a types.List (nil for null/unknown).
func listToStrings(l types.List) []string {
	if l.IsNull() || l.IsUnknown() {
		return nil
	}
	out := make([]string, 0, len(l.Elements()))
	for _, e := range l.Elements() {
		if s, ok := e.(types.String); ok {
			out = append(out, s.ValueString())
		}
	}
	return out
}

// stringsToList builds a types.List of strings; empty/nil becomes a null list
// (Medplum strips empty arrays, so empty == absent).
func stringsToList(ss []string) types.List {
	if len(ss) == 0 {
		return types.ListNull(types.StringType)
	}
	vals := make([]attr.Value, len(ss))
	for i, s := range ss {
		vals[i] = types.StringValue(s)
	}
	return types.ListValueMust(types.StringType, vals)
}
