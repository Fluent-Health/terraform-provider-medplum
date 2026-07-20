package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestSourceHashOf(t *testing.T) {
	// sha256("hello") — fixed vector.
	const want = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if got := sourceHashOf("hello"); got != want {
		t.Fatalf("sourceHashOf: got %s, want %s", got, want)
	}
}

func TestBot_resolveCode(t *testing.T) {
	inline := botModel{Code: types.StringValue("code-body"), SourcePath: types.StringNull()}
	code, ok, err := inline.resolveCode()
	if err != nil || !ok || code != "code-body" {
		t.Fatalf("inline: got (%q, %v, %v)", code, ok, err)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "bot.js")
	if err := os.WriteFile(p, []byte("file-body"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile := botModel{Code: types.StringNull(), SourcePath: types.StringValue(p)}
	code, ok, err = fromFile.resolveCode()
	if err != nil || !ok || code != "file-body" {
		t.Fatalf("file: got (%q, %v, %v)", code, ok, err)
	}

	missing := botModel{Code: types.StringNull(), SourcePath: types.StringValue(filepath.Join(dir, "nope.js"))}
	if _, _, err := missing.resolveCode(); err == nil {
		t.Fatal("expected error for missing file")
	}

	unknown := botModel{Code: types.StringUnknown(), SourcePath: types.StringNull()}
	if _, ok, err := unknown.resolveCode(); ok || err != nil {
		t.Fatalf("unknown code: expected (ok=false, err=nil), got (%v, %v)", ok, err)
	}
}

func TestBinaryIDFromURL(t *testing.T) {
	cases := map[string]string{
		"Binary/abc-123": "abc-123",
		"https://api.example.com/fhir/R4/Binary/f47ac10b-58cc-4372-a567-0e02b2c3d479":                                                        "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"https://api.example.com/fhir/R4/Binary/f47ac10b-58cc-4372-a567-0e02b2c3d479/_history/2":                                             "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"http://localhost:8103/storage/f47ac10b-58cc-4372-a567-0e02b2c3d479/9c858901-8a57-4791-81fe-4c455b099bc9?Signature=x":                "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"https://bucket.s3.amazonaws.com/binary/f47ac10b-58cc-4372-a567-0e02b2c3d479/9c858901-8a57-4791-81fe-4c455b099bc9?X-Amz-Signature=x": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		"https://example.com/not/a/binary": "",
		"":                                 "",
	}
	for in, want := range cases {
		if got := binaryIDFromURL(in); got != want {
			t.Errorf("binaryIDFromURL(%q): got %q, want %q", in, got, want)
		}
	}
}

func TestBot_adminCreateBody(t *testing.T) {
	m := botModel{
		Name:           types.StringValue("my-bot"),
		Description:    types.StringNull(),
		RuntimeVersion: types.StringValue("vmcontext"),
		AccessPolicy:   types.StringValue("AccessPolicy/ap-1"),
	}
	b, err := m.adminCreateBody()
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["name"] != "my-bot" || doc["runtimeVersion"] != "vmcontext" {
		t.Fatalf("unexpected body: %v", doc)
	}
	ap, _ := doc["accessPolicy"].(map[string]any)
	if ap["reference"] != "AccessPolicy/ap-1" {
		t.Fatalf("accessPolicy: %v", doc["accessPolicy"])
	}
	if _, present := doc["description"]; present {
		t.Fatal("null description must be omitted")
	}
}

func TestBot_applyBotFields_PreservesUnmodeledFields(t *testing.T) {
	doc := map[string]any{
		"resourceType":   "Bot",
		"id":             "bot-1",
		"name":           "old-name",
		"description":    "old-desc",
		"timeout":        float64(10),
		"sourceCode":     map[string]any{"url": "Binary/src-1"},
		"executableCode": map[string]any{"url": "Binary/exe-1"},
		"meta":           map[string]any{"project": "proj-1"},
	}
	m := botModel{
		Name:           types.StringValue("new-name"),
		Description:    types.StringNull(),
		RuntimeVersion: types.StringValue("vmcontext"),
		Timeout:        types.Int64Null(),
		RunAsUser:      types.BoolValue(true),
	}
	m.applyBotFields(doc)
	if doc["name"] != "new-name" || doc["runtimeVersion"] != "vmcontext" || doc["runAsUser"] != true {
		t.Fatalf("fields not applied: %v", doc)
	}
	if _, present := doc["description"]; present {
		t.Fatal("null description must be removed")
	}
	if _, present := doc["timeout"]; present {
		t.Fatal("null timeout must be removed")
	}
	if _, present := doc["executableCode"]; !present {
		t.Fatal("executableCode must be preserved")
	}
	if _, present := doc["sourceCode"]; !present {
		t.Fatal("sourceCode must be preserved")
	}
}

func TestBot_fromDoc(t *testing.T) {
	raw := []byte(`{
		"resourceType": "Bot",
		"id": "bot-1",
		"name": "my-bot",
		"runtimeVersion": "vmcontext",
		"timeout": 30,
		"meta": {"project": "proj-1"},
		"executableCode": {"url": "Binary/exe-1"}
	}`)
	var doc botDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	var m botModel
	m.fromDoc(doc)
	if m.ID.ValueString() != "bot-1" || m.Name.ValueString() != "my-bot" {
		t.Fatalf("identity: %v", m)
	}
	if m.RuntimeVersion.ValueString() != "vmcontext" {
		t.Fatalf("runtime: %v", m.RuntimeVersion)
	}
	if m.Timeout.ValueInt64() != 30 {
		t.Fatalf("timeout: %v", m.Timeout)
	}
	if !m.Description.IsNull() {
		t.Fatal("absent description must be null")
	}
	if !m.RunAsUser.IsNull() {
		t.Fatal("absent runAsUser must be null")
	}
	if m.ProjectID.ValueString() != "proj-1" {
		t.Fatalf("project: %v", m.ProjectID)
	}
	if doc.ExecutableCode.URL != "Binary/exe-1" {
		t.Fatalf("executableCode: %v", doc.ExecutableCode)
	}
}
