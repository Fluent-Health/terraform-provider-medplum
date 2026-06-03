package fhirjson

import "testing"

func TestCanonicalize_KeyOrderStable(t *testing.T) {
	a, err := Canonicalize([]byte(`{"b":1,"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Canonicalize([]byte(`{"a":2,"b":1}`))
	if string(a) != string(b) {
		t.Fatalf("canonical forms differ: %s vs %s", a, b)
	}
}

func TestStripServerFields(t *testing.T) {
	in := []byte(`{"resourceType":"ValueSet","id":"123","meta":{"versionId":"5","lastUpdated":"2026-01-01","tag":[{"code":"x"}]},"status":"active"}`)
	out, err := StripServerFields(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"meta":{"tag":[{"code":"x"}]},"resourceType":"ValueSet","status":"active"}`
	if string(out) != want {
		t.Fatalf("got %s want %s", out, want)
	}
}

func TestStripServerFields_DropsEmptyMeta(t *testing.T) {
	in := []byte(`{"resourceType":"ValueSet","id":"1","meta":{"versionId":"5","lastUpdated":"2026"},"status":"active"}`)
	out, _ := StripServerFields(in)
	want := `{"resourceType":"ValueSet","status":"active"}`
	if string(out) != want {
		t.Fatalf("got %s want %s", out, want)
	}
}

func TestEqual_IgnoresServerFields(t *testing.T) {
	config := []byte(`{"resourceType":"ValueSet","status":"active"}`)
	server := []byte(`{"resourceType":"ValueSet","id":"123","meta":{"versionId":"2","lastUpdated":"2026"},"status":"active"}`)
	eq, err := Equal(config, server)
	if err != nil {
		t.Fatal(err)
	}
	if !eq {
		t.Fatal("expected config and server to be semantically equal")
	}
}
