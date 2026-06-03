package fhirprofile

import "testing"

// helper: an SD with the given snapshot elements (as a raw JSON array string).
func sdWith(elementsJSON string) []byte {
	return []byte(`{"resourceType":"StructureDefinition","url":"http://x/p","snapshot":{"element":` + elementsJSON + `}}`)
}

func TestAnalyze_EmptySnapshot_Rejects(t *testing.T) {
	r, err := Analyze([]byte(`{"resourceType":"StructureDefinition","url":"http://x"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Rejects()) != 1 {
		t.Fatalf("want 1 reject, got %d (%+v)", len(r.Rejects()), r.Findings)
	}
}

func TestAnalyze_BadDiscriminatorType_Rejects(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.identifier","path":"Patient.identifier","slicing":{"rules":"open","discriminator":[{"type":"exists","path":"system"}]}}
	]`))
	if len(r.Rejects()) != 1 {
		t.Fatalf("want reject for 'exists' discriminator, got %+v", r.Findings)
	}
}

func TestAnalyze_FHIRPathDiscriminatorPath_Rejects(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.extension","path":"Patient.extension","slicing":{"rules":"open","discriminator":[{"type":"value","path":"resolve()"}]}}
	]`))
	if len(r.Rejects()) != 1 {
		t.Fatalf("want reject for FHIRPath discriminator path, got %+v", r.Findings)
	}
}

func TestAnalyze_ExtensionSliceMissingFixedURL_Rejects(t *testing.T) {
	// extension slice with NO `.url` fixed child
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.extension:race","path":"Patient.extension","sliceName":"race","min":1,"max":"1"}
	]`))
	if len(r.Rejects()) != 1 {
		t.Fatalf("want reject for extension slice missing fixed url, got %+v", r.Findings)
	}
}

func TestAnalyze_ExtensionSliceWithFixedURL_Enforced(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.extension:race","path":"Patient.extension","sliceName":"race","min":1,"max":"1"},
	  {"id":"Patient.extension:race.url","path":"Patient.extension.url","fixedUri":"http://x/race"}
	]`))
	if len(r.Rejects()) != 0 {
		t.Fatalf("want no rejects, got %+v", r.Findings)
	}
	if r.EnforcedCount != 1 {
		t.Fatalf("want extension slice counted exactly once (presence+cardinality), got %d", r.EnforcedCount)
	}
}

func TestAnalyze_ClosedSlicing_Warns(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.identifier","path":"Patient.identifier","slicing":{"rules":"closed","discriminator":[{"type":"value","path":"system"}]}}
	]`))
	if len(r.Warns()) == 0 {
		t.Fatalf("want warn for closed slicing, got %+v", r.Findings)
	}
}

func TestAnalyze_EnforcedCardinalityAndFixed(t *testing.T) {
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.active","path":"Patient.active","min":1,"max":"1"},
	  {"id":"Patient.gender","path":"Patient.gender","fixedCode":"female"}
	]`))
	if r.EnforcedCount != 2 {
		t.Fatalf("want exactly 2 enforced (cardinality once-per-element + fixed), got %d", r.EnforcedCount)
	}
	if len(r.Rejects()) != 0 {
		t.Fatalf("unexpected rejects: %+v", r.Findings)
	}
}

func TestAnalyze_DecorativeOnly_NoEnforced(t *testing.T) {
	// mustSupport + required binding only → zero enforced
	r, _ := Analyze(sdWith(`[
	  {"id":"Patient","path":"Patient"},
	  {"id":"Patient.maritalStatus","path":"Patient.maritalStatus","mustSupport":true,"binding":{"strength":"required","valueSet":"http://x/vs"}}
	]`))
	if r.EnforcedCount != 0 {
		t.Fatalf("want 0 enforced, got %d", r.EnforcedCount)
	}
	if r.DecorativeCount == 0 {
		t.Fatal("want decorative signals counted")
	}
}

func TestAnalyze_InvalidJSON(t *testing.T) {
	if _, err := Analyze([]byte(`{bad`)); err == nil {
		t.Fatal("want error on invalid json")
	}
}
