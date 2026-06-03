package fhirjson

import "testing"

func TestCanonicalize_KeyOrderStable(t *testing.T) {
	a, err := Canonicalize([]byte(`{"b":1,"a":2}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonicalize([]byte(`{"a":2,"b":1}`))
	if err != nil {
		t.Fatal(err)
	}
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
	out, err := StripServerFields(in)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"resourceType":"ValueSet","status":"active"}`
	if string(out) != want {
		t.Fatalf("got %s want %s", out, want)
	}
}

func TestNormalize_InvalidJSON(t *testing.T) {
	bad := []byte("{not json")

	if _, err := Canonicalize(bad); err == nil {
		t.Fatal("Canonicalize: expected error for invalid JSON, got nil")
	}

	if _, err := StripServerFields(bad); err == nil {
		t.Fatal("StripServerFields: expected error for invalid JSON, got nil")
	}

	if _, err := Equal(bad, []byte("{}")); err == nil {
		t.Fatal("Equal: expected error for invalid JSON, got nil")
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

func TestContains_ServerSupersetIsContained(t *testing.T) {
	config := []byte(`{"resourceType":"ValueSet","status":"active"}`)
	server := []byte(`{"resourceType":"ValueSet","status":"active","id":"1","meta":{"project":"p","author":"a","versionId":"3"},"text":{"status":"generated"}}`)
	ok, err := Contains(config, server)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected server superset to be contained")
	}
}

func TestContains_UserFieldDiffersNotContained(t *testing.T) {
	config := []byte(`{"resourceType":"ValueSet","status":"active"}`)
	server := []byte(`{"resourceType":"ValueSet","status":"draft","meta":{"project":"p"}}`)
	ok, err := Contains(config, server)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected differing field to not be contained")
	}
}

func TestContains_MissingUserFieldNotContained(t *testing.T) {
	config := []byte(`{"resourceType":"ValueSet","url":"http://x"}`)
	server := []byte(`{"resourceType":"ValueSet"}`)
	ok, err := Contains(config, server)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected missing user field to not be contained")
	}
}

func TestContains_ArrayLengthDiffers(t *testing.T) {
	config := []byte(`{"resourceType":"ValueSet","contact":[{"name":"a"}]}`)
	server := []byte(`{"resourceType":"ValueSet","contact":[{"name":"a"},{"name":"b"}]}`)
	ok, err := Contains(config, server)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected array length mismatch to not be contained")
	}
}

func TestContains_NestedObjectContained(t *testing.T) {
	config := []byte(`{"resourceType":"ValueSet","compose":{"inactive":true}}`)
	server := []byte(`{"resourceType":"ValueSet","compose":{"inactive":true,"lockedDate":"2026"}}`)
	ok, err := Contains(config, server)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected nested object with extra server field to be contained")
	}
}
