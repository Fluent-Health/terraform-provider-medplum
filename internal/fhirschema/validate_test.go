package fhirschema

import (
	"strings"
	"testing"
)

func TestValidate_Valid(t *testing.T) {
	v, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	body := []byte(`{"resourceType":"ValueSet","status":"active"}`)
	if err := v.Validate("ValueSet", body); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidate_WrongType_BadEnum(t *testing.T) {
	v, _ := New()
	// status has a fixed value set; "bogus" is not a valid code.
	body := []byte(`{"resourceType":"ValueSet","status":"bogus"}`)
	if err := v.Validate("ValueSet", body); err == nil {
		t.Fatal("expected validation error for bad status")
	}
}

func TestValidate_UnknownResourceType(t *testing.T) {
	v, _ := New()
	if err := v.Validate("NotARealType", []byte(`{"resourceType":"NotARealType"}`)); err == nil {
		t.Fatal("expected error for unknown resource type")
	} else if !strings.Contains(err.Error(), "unknown FHIR resource type") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_InvalidJSON(t *testing.T) {
	v, _ := New()
	if err := v.Validate("ValueSet", []byte(`{not json`)); err == nil {
		t.Fatal("expected error for invalid json")
	}
}
