package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
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

// ensureBotsFeature enables the "bots" feature on the session project
// (Project.features gate, server bots/utils.ts isBotEnabled).
func ensureBotsFeature(t *testing.T, c *client.Client) {
	t.Helper()
	ctx := context.Background()
	pid, err := c.CurrentProjectID(ctx)
	if err != nil {
		t.Fatalf("current project: %v", err)
	}
	out, err := c.FHIRRead(ctx, "Project", pid)
	if err != nil {
		t.Fatalf("read project: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	feats, _ := doc["features"].([]any)
	for _, f := range feats {
		if f == "bots" {
			return
		}
	}
	doc["features"] = append(feats, "bots")
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FHIRUpdate(ctx, "Project", pid, body); err != nil {
		t.Fatalf("enable bots feature: %v", err)
	}
}

// checkBotExecute runs POST Bot/{id}/$execute and asserts the deployed code's
// return value — proving the bundle is actually live, not just attached.
func checkBotExecute(c *client.Client, resourceName, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		out, err := c.Operation(context.Background(), "Bot", rs.Primary.ID, "$execute", []byte(`{}`))
		if err != nil {
			return fmt.Errorf("$execute: %w", err)
		}
		if !strings.Contains(string(out), want) {
			return fmt.Errorf("$execute returned %q, want it to contain %q", out, want)
		}
		return nil
	}
}

func TestAccBot_lifecycle(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)
	ensureBotsFeature(t, c)

	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := func(ret string) string {
		// Build the JS separately and quote the whole thing with %q — the code
		// contains double quotes, which cannot be nested in an HCL string.
		botCode := fmt.Sprintf(`exports.handler = async () => %q;`, ret)
		return fmt.Sprintf(`
resource "medplum_access_policy" "bot" {
  name = "tf-acc-bot-policy-%s"
  resource {
    resource_type = "Patient"
    readonly      = true
  }
}

resource "medplum_bot" "test" {
  name          = "tf-acc-bot-%s"
  description   = "acc test bot"
  code          = %q
  timeout       = 30
  access_policy = "AccessPolicy/${medplum_access_policy.bot.id}"
}`, suffix, suffix, botCode)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create: bot + membership + deployed code, live via $execute
				Config: cfg("hello-v1"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_bot.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_bot.test", "membership_id"),
					resource.TestCheckResourceAttrSet("medplum_bot.test", "project_id"),
					resource.TestCheckResourceAttrSet("medplum_bot.test", "source_hash"),
					resource.TestCheckResourceAttr("medplum_bot.test", "runtime_version", "vmcontext"),
					checkBotExecute(c, "medplum_bot.test", "hello-v1"),
				),
			},
			{Config: cfg("hello-v1"), PlanOnly: true}, // no-op plan
			{ // edit code -> apply -> new behaviour live (no restart)
				Config: cfg("hello-v2"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkBotExecute(c, "medplum_bot.test", "hello-v2"),
				),
			},
			{ // import: membership discovered, hash recomputed from deployed Binary
				ResourceName:            "medplum_bot.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"code", "source_path"},
			},
		},
	})
}

func TestAccBot_fissionRejectedAtPlanTime(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	cfg := `
resource "medplum_bot" "fission" {
  name            = "tf-acc-bot-fission"
  code            = "exports.handler = async () => true;"
  runtime_version = "fission"
}`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`not supported by this environment|supported_bot_runtimes`),
			},
		},
	})
}
