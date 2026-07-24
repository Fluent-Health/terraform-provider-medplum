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
	"time"

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
	if m.Ref.ValueString() != "Bot/bot-1" {
		t.Fatalf("ref: %v", m.Ref)
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
	ensureProjectFeature(t, c, "bots")
}

// ensureProjectFeature makes sure the session's Project has `feature` in its
// features array, enabling it if absent. Used to satisfy feature gates like
// "bots" (bot execution) and "cron" (bot scheduling).
func ensureProjectFeature(t *testing.T, c *client.Client, feature string) {
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
		if f == feature {
			return
		}
	}
	doc["features"] = append(feats, feature)
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.FHIRUpdate(ctx, "Project", pid, body); err != nil {
		t.Fatalf("enable %s feature: %v", feature, err)
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
  access_policy = medplum_access_policy.bot.ref
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
					resource.TestCheckResourceAttrSet("medplum_bot.test", "ref"),
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

// checkSubscriptionFiresBot creates a QuestionnaireResponse directly against
// the acc client (not through Terraform) and polls for the Observation the
// subscription-triggered bot writes. This proves the canonical trigger path:
// the server's subscription worker matches the rest-hook Subscription's
// criteria against the newly created resource (server
// packages/core/src/subscriptions/index.ts resourceMatchesSubscriptionCriteria
// — a bare `criteria = "QuestionnaireResponse"` with no
// subscription-supported-interaction extension matches any interaction,
// including "create") and, because channel.endpoint starts with "Bot/"
// (packages/server/src/workers/subscription.ts processSubscriptionResource,
// `subscription.channel?.endpoint?.startsWith('Bot/')`), executes the bot via
// execBot rather than sending an HTTP rest-hook — reading the endpoint as a
// FHIR reference string (`systemRepo.readReference({reference: url})`), not a
// full URL. Execution is async via the Redis-backed subscription queue, so
// this polls rather than asserting immediately.
func checkSubscriptionFiresBot(c *client.Client) resource.TestCheckFunc {
	return func(_ *terraform.State) error {
		ctx := context.Background()
		qrOut, err := c.FHIRCreate(ctx, "QuestionnaireResponse", []byte(`{"resourceType":"QuestionnaireResponse","status":"completed"}`))
		if err != nil {
			return fmt.Errorf("create QuestionnaireResponse: %w", err)
		}
		var qr struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(qrOut, &qr); err != nil || qr.ID == "" {
			return fmt.Errorf("create QuestionnaireResponse: no id in response %s", qrOut)
		}

		query := fmt.Sprintf("identifier=https://example.com/tf-acc-bot-e2e|%s", qr.ID)
		deadline := time.Now().Add(60 * time.Second)
		var last []byte
		for {
			out, err := c.FHIRSearch(ctx, "Observation", query)
			if err != nil {
				return fmt.Errorf("search Observation: %w", err)
			}
			last = out
			var bundle struct {
				Entry []json.RawMessage `json:"entry"`
			}
			if err := json.Unmarshal(out, &bundle); err == nil && len(bundle.Entry) > 0 {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("bot-authored Observation for QuestionnaireResponse/%s never appeared after 60s; last search response: %s", qr.ID, last)
			}
			time.Sleep(2 * time.Second)
		}
	}
}

func TestAccBot_subscriptionEndToEnd(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)
	ensureBotsFeature(t, c)

	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	// vmcontext is CommonJS. The triggering QuestionnaireResponse arrives as
	// event.input: server execBot (workers/subscription.ts) passes the resource
	// itself as the bot's input body for create/update interactions (only
	// "delete" wraps it as {deletedResource: resource}), and vmcontext.ts hands
	// that object straight through as event.input for FHIR_JSON content — no
	// JSON.parse needed in the bot.
	botCode := `exports.handler = async (medplum, event) => {
  const qr = event.input;
  await medplum.createResource({
    resourceType: "Observation",
    status: "final",
    code: { text: "tf-acc-bot-e2e" },
    identifier: [{ system: "https://example.com/tf-acc-bot-e2e", value: qr.id }]
  });
  return true;
};`
	// The JS body is built separately and injected with %q — it contains double
	// quotes, which cannot be nested in an HCL string.
	cfg := fmt.Sprintf(`
resource "medplum_bot" "e2e" {
  name = "tf-acc-bot-e2e-%s"
  code = %q
}

resource "medplum_fhir_resource" "qr_hook" {
  resource_type = "Subscription"
  body = jsonencode({
    resourceType = "Subscription"
    status       = "active"
    reason       = "bot e2e test"
    criteria     = "QuestionnaireResponse"
    channel = {
      type = "rest-hook"
      # Deliberately using medplum_bot.e2e.ref (a second consumer position for
      # the "ref" attribute, "Bot/<id>") rather than a hand-built string: the
      # server requires channel.endpoint to be exactly a "Bot/{id}" reference
      # string for bot-backed subscriptions (subscription.ts, see comment
      # above checkSubscriptionFiresBot), which is precisely what ref yields.
      endpoint = medplum_bot.e2e.ref
    }
  })
}`, suffix, botCode)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_bot.e2e", "id"),
					resource.TestCheckResourceAttrSet("medplum_bot.e2e", "ref"),
					resource.TestCheckResourceAttrSet("medplum_fhir_resource.qr_hook", "id"),
					checkSubscriptionFiresBot(c),
				),
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

// checkBotMembershipAdmin asserts the live ProjectMembership.admin flag of the
// bot's membership, straight from the server.
func checkBotMembershipAdmin(c *client.Client, resourceName string, want bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		mid := rs.Primary.Attributes["membership_id"]
		if mid == "" {
			return fmt.Errorf("%s has no membership_id", resourceName)
		}
		out, err := c.FHIRRead(context.Background(), "ProjectMembership", mid)
		if err != nil {
			return fmt.Errorf("read membership: %w", err)
		}
		var mem struct {
			Admin *bool `json:"admin"`
		}
		if err := json.Unmarshal(out, &mem); err != nil {
			return err
		}
		got := mem.Admin != nil && *mem.Admin
		if got != want {
			return fmt.Errorf("membership admin = %v, want %v", got, want)
		}
		return nil
	}
}

func TestAccBot_adminMembership(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)
	ensureBotsFeature(t, c)

	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := func(adminLine string) string {
		return fmt.Sprintf(`
resource "medplum_bot" "admin" {
  name = "tf-acc-bot-admin-%s"
  code = "exports.handler = async () => true;"
  %s
}`, suffix, adminLine)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create with admin membership
				Config: cfg("admin = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("medplum_bot.admin", "admin", "true"),
					checkBotMembershipAdmin(c, "medplum_bot.admin", true),
				),
			},
			{Config: cfg("admin = true"), PlanOnly: true}, // no-op plan
			{ // demote: true -> false updates the membership back
				Config: cfg("admin = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("medplum_bot.admin", "admin", "false"),
					checkBotMembershipAdmin(c, "medplum_bot.admin", false),
				),
			},
			{ // promote again via update path (default -> true drift direction)
				Config: cfg("admin = true"),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkBotMembershipAdmin(c, "medplum_bot.admin", true),
				),
			},
			{ // import must reflect the live membership.admin
				ResourceName:            "medplum_bot.admin",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"code", "source_path"},
			},
		},
	})
}

// checkBotCronString asserts the live Bot.cronString from the server. want ""
// means the field must be absent.
func checkBotCronString(c *client.Client, resourceName, want string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[resourceName]
		if !ok {
			return fmt.Errorf("resource %s not found in state", resourceName)
		}
		out, err := c.FHIRRead(context.Background(), "Bot", rs.Primary.ID)
		if err != nil {
			return fmt.Errorf("read bot: %w", err)
		}
		var bot struct {
			CronString string `json:"cronString"`
		}
		if err := json.Unmarshal(out, &bot); err != nil {
			return err
		}
		if bot.CronString != want {
			return fmt.Errorf("cronString = %q, want %q", bot.CronString, want)
		}
		return nil
	}
}

func TestAccBot_cronSchedule(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	c := newAccClient(t)
	ensureBotsFeature(t, c)
	ensureProjectFeature(t, c, "cron")

	suffix := acctest.RandStringFromCharSet(8, acctest.CharSetAlphaNum)
	cfg := func(cronLine string) string {
		return fmt.Sprintf(`
resource "medplum_bot" "cron" {
  name = "tf-acc-bot-cron-%s"
  code = "exports.handler = async () => true;"
  %s
}`, suffix, cronLine)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{ // create with a schedule
				Config: cfg(`cron_string = "0 2 * * *"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("medplum_bot.cron", "cron_string", "0 2 * * *"),
					checkBotCronString(c, "medplum_bot.cron", "0 2 * * *"),
				),
			},
			{Config: cfg(`cron_string = "0 2 * * *"`), PlanOnly: true}, // no-op plan
			{ // change the schedule
				Config: cfg(`cron_string = "*/15 9-17 * * 1-5"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					checkBotCronString(c, "medplum_bot.cron", "*/15 9-17 * * 1-5"),
				),
			},
			{ // remove the schedule -> field deleted server-side
				Config: cfg(``),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("medplum_bot.cron", "cron_string"),
					checkBotCronString(c, "medplum_bot.cron", ""),
				),
			},
		},
	})
}

func TestAccBot_invalidCronRejectedAtPlanTime(t *testing.T) {
	if os.Getenv("TF_ACC") == "" {
		t.Skip("TF_ACC not set")
	}
	cfg := `
resource "medplum_bot" "badcron" {
  name        = "tf-acc-bot-badcron"
  code        = "exports.handler = async () => true;"
  cron_string = "not a cron"
}`
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      cfg,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`Invalid cron_string`),
			},
		},
	})
}

func TestValidateCronString(t *testing.T) {
	valid := []string{
		"0 2 * * *",         // 02:00 daily
		"*/15 9-17 * * 1-5", // every 15m, 9-5, Mon-Fri
		"0 0 1 * *",         // 1st of month
		"* * * * *",         // every minute
		"0,30 * * * 0",      // :00 and :30 on Sundays
		"0 0 * * 6",         // Saturdays midnight
	}
	for _, expr := range valid {
		if err := validateCronString(expr); err != nil {
			t.Errorf("validateCronString(%q) = %v, want nil", expr, err)
		}
	}

	invalid := []string{
		"",            // empty
		"testing",     // not cron
		"0 2 * *",     // 4 fields
		"0 2 * * * *", // 6 fields (seconds not allowed)
		"60 * * * *",  // minute out of range
		"* 24 * * *",  // hour out of range
		"* * 0 * *",   // day-of-month below 1
		"* * 32 * *",  // day-of-month above 31
		"* * * 13 *",  // month out of range
		"* * * * 7",   // day-of-week above 6
		"*/0 * * * *", // zero step
		"1-a * * * *", // non-numeric range
	}
	for _, expr := range invalid {
		if err := validateCronString(expr); err == nil {
			t.Errorf("validateCronString(%q) = nil, want error", expr)
		}
	}
}

func TestApplyBotFields_CronString(t *testing.T) {
	// Setting cron_string writes cronString.
	m := botModel{
		Name:           types.StringValue("b"),
		RuntimeVersion: types.StringValue("vmcontext"),
		CronString:     types.StringValue("0 2 * * *"),
	}
	doc := map[string]any{}
	m.applyBotFields(doc)
	if doc["cronString"] != "0 2 * * *" {
		t.Fatalf("cronString = %v, want %q", doc["cronString"], "0 2 * * *")
	}

	// Clearing cron_string deletes the field from an existing doc.
	m.CronString = types.StringNull()
	doc = map[string]any{"cronString": "0 2 * * *"}
	m.applyBotFields(doc)
	if _, present := doc["cronString"]; present {
		t.Fatalf("cronString should be deleted when null, got %v", doc["cronString"])
	}
}
