package fhirmigrate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildScanQuery(t *testing.T) {
	q := BuildScanQuery("questionnaire=X", "urn:m", "diet", "abcd1234", 50)
	if !strings.HasPrefix(q, "questionnaire=X&_tag:not=") {
		t.Fatalf("missing search + tag filter: %s", q)
	}
	if !strings.Contains(q, "_count=50") {
		t.Fatalf("missing count: %s", q)
	}
	// tag value urn:m/diet|abcd1234 must be URL-escaped
	if !strings.Contains(q, "urn%3Am%2Fdiet%7Cabcd1234") {
		t.Fatalf("tag value not escaped: %s", q)
	}
}

func TestBuildScanQuery_emptySearch(t *testing.T) {
	q := BuildScanQuery("", "urn:m", "diet", "h", 10)
	if strings.HasPrefix(q, "&") {
		t.Fatalf("must not start with &: %s", q)
	}
}

func TestParseSearchEntries_skipsOutcome(t *testing.T) {
	body := []byte(`{"resourceType":"Bundle","type":"searchset","entry":[
		{"resource":{"resourceType":"QuestionnaireResponse","id":"a"}},
		{"resource":{"resourceType":"OperationOutcome"}},
		{"resource":{"resourceType":"QuestionnaireResponse","id":"b"}}
	]}`)
	got, err := ParseSearchEntries(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(got))
	}
	if got[0]["id"] != "a" || got[1]["id"] != "b" {
		t.Fatalf("wrong resources: %v", got)
	}
}

func TestBuildBundle(t *testing.T) {
	res := []map[string]any{{"resourceType": "QuestionnaireResponse", "id": "a"}}
	out, err := BuildBundle("batch", res)
	if err != nil {
		t.Fatal(err)
	}
	var b map[string]any
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatal(err)
	}
	if b["type"] != "batch" {
		t.Fatalf("wrong type: %v", b["type"])
	}
	entry := b["entry"].([]any)[0].(map[string]any)
	req := entry["request"].(map[string]any)
	if req["method"] != "PUT" || req["url"] != "QuestionnaireResponse/a" {
		t.Fatalf("wrong request: %v", req)
	}
}

func TestBuildBundle_missingIDErrors(t *testing.T) {
	_, err := BuildBundle("batch", []map[string]any{{"resourceType": "QuestionnaireResponse"}})
	if err == nil {
		t.Fatal("expected error for missing id")
	}
}

func TestParseBundleResponse(t *testing.T) {
	body := []byte(`{"resourceType":"Bundle","type":"batch-response","entry":[
		{"response":{"status":"200"}},
		{"response":{"status":"201"}},
		{"response":{"status":"409 Conflict"}}
	]}`)
	r, err := ParseBundleResponse(body)
	if err != nil {
		t.Fatal(err)
	}
	if r.Succeeded != 2 || r.Failed != 1 {
		t.Fatalf("got %+v", r)
	}
}
