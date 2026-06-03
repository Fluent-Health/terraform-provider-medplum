package provider

import (
	"encoding/json"
	"testing"
)

func TestUser_toFHIR_ProjectScope(t *testing.T) {
	m := userModel{
		FirstName: typesStr("Jane"),
		LastName:  typesStr("Doe"),
		Email:     typesStr("jane@example.com"),
		ProjectID: typesStr("p1"),
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	proj, _ := doc["project"].(map[string]any)
	if proj["reference"] != "Project/p1" {
		t.Fatalf("bad project ref: %v", doc["project"])
	}
}
