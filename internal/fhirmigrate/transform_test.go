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

func TestApplyRemaps_matchesSystemlessCodingByEmptyFromSystem(t *testing.T) {
	r := map[string]any{
		"resourceType": "Condition",
		"id":           "c1",
		"severity": map[string]any{
			"coding": []any{
				map[string]any{"code": "1255665007", "display": "Moderate"},
			},
			"text": "Moderate",
		},
	}
	remaps := []Remap{
		{From: Coding{System: "", Code: "1255665007"},
			To: Coding{System: "http://snomed.info/sct", Code: "6736007", Display: "Moderate"}},
	}
	if !ApplyRemaps(r, remaps) {
		t.Fatal("expected changed=true")
	}
	c := r["severity"].(map[string]any)["coding"].([]any)[0].(map[string]any)
	if c["system"] != "http://snomed.info/sct" || c["code"] != "6736007" || c["display"] != "Moderate" {
		t.Fatalf("system-less coding not remapped: %v", c)
	}
	if ApplyRemaps(r, remaps) {
		t.Fatal("second application must report no change (fixed point)")
	}
}

func TestApplyRemaps_emptyFromSystemDoesNotMatchSystemedCoding(t *testing.T) {
	r := map[string]any{"system": "http://snomed.info/sct", "code": "1255665007"}
	remaps := []Remap{
		{From: Coding{System: "", Code: "1255665007"}, To: Coding{System: "http://x", Code: "999"}},
	}
	if ApplyRemaps(r, remaps) {
		t.Fatal("empty from.system must not match a coding that has a system")
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
	if h1, h2 := SpecHash(s), SpecHash(s); h1 != h2 {
		t.Fatalf("hash must be stable, got %q and %q", h1, h2)
	}
	s2 := s
	s2.Search = "questionnaire=Y"
	if SpecHash(s) == SpecHash(s2) {
		t.Fatal("hash must change when spec changes")
	}
}
