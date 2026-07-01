package fhirmigrate

import "testing"

func qr() map[string]any {
	return map[string]any{
		"resourceType": "QuestionnaireResponse",
		"id":           "abc",
		"status":       "completed",
		"item": []any{
			map[string]any{
				"linkId": "diet",
				"answer": []any{
					map[string]any{"valueCoding": map[string]any{"system": "http://old", "code": "1001"}},
				},
				"item": []any{
					map[string]any{
						"linkId": "sub",
						"answer": []any{map[string]any{"valueCoding": map[string]any{"system": "http://old", "code": "1002"}}},
					},
				},
			},
		},
	}
}

var testRemaps = []Remap{
	{From: Coding{System: "http://old", Code: "1001"}, To: Coding{System: "http://new", Code: "breakfast", Display: "Breakfast"}},
	{From: Coding{System: "http://old", Code: "1002"}, To: Coding{System: "http://new", Code: "lunch"}},
}

func codingAt(t *testing.T, r map[string]any, idx int) map[string]any {
	t.Helper()
	item := r["item"].([]any)[0].(map[string]any)
	if idx == 0 {
		return item["answer"].([]any)[0].(map[string]any)["valueCoding"].(map[string]any)
	}
	sub := item["item"].([]any)[0].(map[string]any)
	return sub["answer"].([]any)[0].(map[string]any)["valueCoding"].(map[string]any)
}

func TestApplyRemaps_rewritesNestedAndSetsDisplay(t *testing.T) {
	r := qr()
	if !ApplyRemaps(r, testRemaps) {
		t.Fatal("expected changed=true")
	}
	top := codingAt(t, r, 0)
	if top["system"] != "http://new" || top["code"] != "breakfast" || top["display"] != "Breakfast" {
		t.Fatalf("top coding not remapped: %v", top)
	}
	sub := codingAt(t, r, 1)
	if sub["system"] != "http://new" || sub["code"] != "lunch" {
		t.Fatalf("nested coding not remapped: %v", sub)
	}
	if _, ok := sub["display"]; ok {
		t.Fatalf("display must not be set when To.Display empty: %v", sub)
	}
}

func TestApplyRemaps_fixedPoint(t *testing.T) {
	r := qr()
	ApplyRemaps(r, testRemaps)
	if ApplyRemaps(r, testRemaps) {
		t.Fatal("second application must report no change (fixed point)")
	}
}

func TestSetMarkerTag_replacesSameSystem(t *testing.T) {
	r := qr()
	SetMarkerTag(r, "urn:m/diet", "h1")
	SetMarkerTag(r, "urn:other/x", "keep")
	SetMarkerTag(r, "urn:m/diet", "h2") // must replace h1, not append
	tags := r["meta"].(map[string]any)["tag"].([]any)
	var mine, others int
	for _, tv := range tags {
		tm := tv.(map[string]any)
		if tm["system"] == "urn:m/diet" {
			mine++
			if tm["code"] != "h2" {
				t.Fatalf("expected code h2, got %v", tm["code"])
			}
		} else {
			others++
		}
	}
	if mine != 1 || others != 1 {
		t.Fatalf("expected 1 own tag + 1 other, got mine=%d others=%d", mine, others)
	}
}

func TestSpecHash_stableAndSensitive(t *testing.T) {
	s := Spec{TargetResourceType: "QuestionnaireResponse", Search: "questionnaire=X", MarkerSystem: "urn:m", Remaps: testRemaps}
	if SpecHash(s) != SpecHash(s) {
		t.Fatal("hash must be stable")
	}
	s2 := s
	s2.Search = "questionnaire=Y"
	if SpecHash(s) == SpecHash(s2) {
		t.Fatal("hash must change when spec changes")
	}
}
