package provider

import (
	"encoding/json"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestAccessPolicy_toFHIR_WriteConstraint(t *testing.T) {
	m := accessPolicyModel{
		Name: types.StringValue("document-validator"),
		Resource: []accessPolicyResourceRow{
			{
				ResourceType:   types.StringValue("Task"),
				Criteria:       types.StringNull(),
				Readonly:       types.BoolNull(),
				HiddenFields:   types.ListNull(types.StringType),
				ReadonlyFields: types.ListNull(types.StringType),
				Compartment:    types.StringNull(),
				Interaction:    stringsToList([]string{"update"}),
				WriteConstraint: []accessPolicyWriteConstraint{
					{
						Expression:  types.StringValue("%after.businessStatus.text.startsWith('workflow:validator:')"),
						Description: types.StringValue("a validator may only write validator-stage statuses"),
					},
					{
						Expression:  types.StringValue("%before.businessStatus.text != %after.businessStatus.text"),
						Description: types.StringNull(),
					},
				},
			},
		},
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Resource []struct {
			WriteConstraint []map[string]any `json:"writeConstraint"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Resource) != 1 || len(doc.Resource[0].WriteConstraint) != 2 {
		t.Fatalf("expected 2 write constraints; got: %s", b)
	}
	wc := doc.Resource[0].WriteConstraint

	// language is never user-supplied and must always be FHIRPath: Medplum evaluates
	// with evalFhirPathTyped regardless of what this field says.
	for i, c := range wc {
		if c["language"] != "text/fhirpath" {
			t.Errorf("constraint %d language: got %v, want text/fhirpath", i, c["language"])
		}
	}
	if wc[0]["expression"] != "%after.businessStatus.text.startsWith('workflow:validator:')" {
		t.Errorf("expression: got %v", wc[0]["expression"])
	}
	if wc[0]["description"] != "a validator may only write validator-stage statuses" {
		t.Errorf("description: got %v", wc[0]["description"])
	}
	// An omitted description must not serialize as "" — FHIR rejects empty strings.
	if _, present := wc[1]["description"]; present {
		t.Errorf("null description should be omitted entirely; got %v", wc[1]["description"])
	}
}

func TestAccessPolicy_toFHIR_NoWriteConstraint(t *testing.T) {
	m := accessPolicyModel{
		Name: types.StringValue("plain"),
		Resource: []accessPolicyResourceRow{
			{
				ResourceType:   types.StringValue("Patient"),
				Criteria:       types.StringNull(),
				Readonly:       types.BoolNull(),
				HiddenFields:   types.ListNull(types.StringType),
				ReadonlyFields: types.ListNull(types.StringType),
				Compartment:    types.StringNull(),
				Interaction:    types.ListNull(types.StringType),
			},
		},
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	rows, ok := doc["resource"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("expected 1 resource row; got: %s", b)
	}
	row := rows[0].(map[string]any)
	if _, present := row["writeConstraint"]; present {
		t.Errorf("writeConstraint must be omitted when none are set; got: %s", b)
	}
}

func TestAccessPolicy_fromFHIR_WriteConstraint(t *testing.T) {
	raw := []byte(`{
		"id": "pol2",
		"name": "document-validator",
		"resource": [
			{"resourceType": "Task", "interaction": ["update"], "writeConstraint": [
				{"language": "text/fhirpath", "expression": "%after.status = 'completed'", "description": "only completed"},
				{"language": "text/fhirpath", "expression": "%before.status != %after.status"}
			]},
			{"resourceType": "Patient", "interaction": ["read"]}
		]
	}`)
	var m accessPolicyModel
	if err := m.fromFHIR(raw); err != nil {
		t.Fatal(err)
	}
	if len(m.Resource) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(m.Resource))
	}
	wc := m.Resource[0].WriteConstraint
	if len(wc) != 2 {
		t.Fatalf("expected 2 write constraints, got %d", len(wc))
	}
	if wc[0].Expression.ValueString() != "%after.status = 'completed'" {
		t.Errorf("expression: got %v", wc[0].Expression)
	}
	if wc[0].Description.ValueString() != "only completed" {
		t.Errorf("description: got %v", wc[0].Description)
	}
	if !wc[1].Description.IsNull() {
		t.Errorf("absent description should be null, got %v", wc[1].Description)
	}
	// A row without constraints must come back nil, not an empty slice, or a
	// constraint-free entry round-trips to an empty block and diffs forever.
	if m.Resource[1].WriteConstraint != nil {
		t.Errorf("row without writeConstraint should be nil, got %v", m.Resource[1].WriteConstraint)
	}
}

func TestAccessPolicy_WriteConstraint_RoundTrip(t *testing.T) {
	original := []byte(`{
		"id": "pol3",
		"name": "rt",
		"resource": [
			{"resourceType": "Task", "interaction": ["update"], "writeConstraint": [
				{"language": "text/fhirpath", "expression": "%after.businessStatus.text.startsWith('x:')", "description": "d"}
			]}
		]
	}`)
	var m accessPolicyModel
	if err := m.fromFHIR(original); err != nil {
		t.Fatal(err)
	}
	b, err := m.toFHIR("pol3")
	if err != nil {
		t.Fatal(err)
	}
	var back accessPolicyModel
	if err := back.fromFHIR(b); err != nil {
		t.Fatal(err)
	}
	if len(back.Resource) != 1 || len(back.Resource[0].WriteConstraint) != 1 {
		t.Fatalf("round trip lost the constraint: %s", b)
	}
	got := back.Resource[0].WriteConstraint[0]
	if got.Expression.ValueString() != "%after.businessStatus.text.startsWith('x:')" {
		t.Errorf("expression drifted: %v", got.Expression)
	}
	if got.Description.ValueString() != "d" {
		t.Errorf("description drifted: %v", got.Description)
	}
}
