# Medplum Terraform Provider — Plan 2: Typed Resources + Auth Refresh

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a refreshing super-admin login token source and five typed Medplum resources (`access_policy`, `client_application`, `project_membership`, `user`, `project`), each a 1:1 FHIR-CRUD wrapper with live-Medplum acceptance tests.

**Architecture:** Each typed resource maps a fixed set of attributes to/from a Medplum FHIR resource via the existing `internal/client`. Unlike the generic `medplum_fhir_resource` (opaque JSON body + `Contains` drift), typed resources read back **only their modeled fields**, so server-managed fields (`meta.project`/`author`/`compartment`, etc.) are ignored automatically — no semantic-diff machinery needed. Computed identity fields (`id`, `version_id`, `last_updated`) come from the server response.

**Tech Stack:** Go 1.22+, terraform-plugin-framework, terraform-plugin-testing, the Plan-1 `internal/client` + `internal/fhirschema`.

**Builds on:** merged Plan 1 (`main`). Spec: `docs/superpowers/specs/2026-06-03-plan2-typed-resources-design.md`.

**Conventions for every task:**
- Work on branch `feat/plan-2-typed-resources` (create it in Task 0).
- TDD: failing test → see it fail → implement → see it pass → commit.
- Before each commit: `gofmt -w . && go vet ./... && go test ./... -count=1`.
- Commit messages use Conventional Commits, footer:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
  Use `git -c commit.gpgsign=false commit`.
- Acceptance tests run only under `TF_ACC=1` (skipped otherwise); they reuse the existing
  `testAccProtoV6ProviderFactories` and `testAccPreCheck` in `internal/provider/provider_test.go`.

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `internal/client/auth.go` | (modify) login flow → refreshing `oauth2.TokenSource` via `ReuseTokenSource` |
| `internal/client/admin.go` | (new) `SetPassword`; `Operation` helper for FHIR `$operation` POSTs |
| `internal/provider/shared.go` | (new) helpers shared by typed resources: `refString`, `stringOrNull`, `setComputedMeta` |
| `internal/provider/access_policy_resource.go` | (new) `medplum_access_policy` |
| `internal/provider/client_application_resource.go` | (new) `medplum_client_application` |
| `internal/provider/project_membership_resource.go` | (new) `medplum_project_membership` |
| `internal/provider/user_resource.go` | (new) `medplum_user` |
| `internal/provider/project_resource.go` | (new) `medplum_project` |
| `internal/provider/provider.go` | (modify) register the five resources |
| `internal/provider/*_test.go` | per-resource acceptance + targeted unit tests |

`extractMeta` (already in `internal/provider/fhir_resource.go`, package `provider`) is reused by typed resources.

---

## Task 0: Branch

- [ ] **Step 1: Create the feature branch**

Run:
```bash
cd /home/ivan/Developer/terraform-provider-medplum
git checkout main && git pull --ff-only 2>/dev/null; git checkout -b feat/plan-2-typed-resources
go test ./... -count=1
```
Expected: branch created; baseline tests pass.

---

## Task 1: Refreshing login token source

**Files:**
- Modify: `internal/client/auth.go`
- Test: `internal/client/auth_test.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/client/auth_test.go`:

```go
func TestLogin_RefreshesAfterExpiry(t *testing.T) {
	var loginCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/auth/login":
			loginCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "L1", "code": "C1"})
		case "/oauth2/token":
			// expires_in=1 second so the token expires almost immediately
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("tok-%d", loginCount), "expires_in": 1})
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Email: "a@b.com", Password: "pw"}
	ts, err := cfg.tokenSource(context.Background())
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	t1, err := ts.Token()
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	// Force expiry and request again — should re-login.
	time.Sleep(1500 * time.Millisecond)
	t2, err := ts.Token()
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if loginCount < 2 {
		t.Fatalf("expected re-login after expiry, loginCount=%d", loginCount)
	}
	if t1.AccessToken == t2.AccessToken {
		t.Fatalf("expected a refreshed token, got same %q", t1.AccessToken)
	}
}
```

Add `"time"` to the test imports if missing.

- [ ] **Step 2: Run it to verify failure**

Run: `go test ./internal/client/... -run TestLogin_RefreshesAfterExpiry -v`
Expected: FAIL (current login returns a non-expiring StaticTokenSource → only one login, tokens equal).

- [ ] **Step 3: Implement the refreshing source**

In `internal/client/auth.go`:

1. In `tokenSource()`, replace the login branch:
```go
	case c.hasLogin():
		src := &loginTokenSource{cfg: c, ctx: ctx}
		return oauth2.ReuseTokenSource(nil, src), nil
```
2. Add the source type (its `Token()` runs the existing login flow):
```go
// loginTokenSource performs Medplum email+password login (with PKCE + token
// exchange) on demand. Wrapped in oauth2.ReuseTokenSource, it re-logs in when
// the previous token expires.
type loginTokenSource struct {
	cfg Config
	ctx context.Context
}

func (s *loginTokenSource) Token() (*oauth2.Token, error) {
	return s.cfg.login(s.ctx)
}
```
3. In `login()`, when decoding the `/oauth2/token` response, also read `expires_in` and set the token expiry. Change the token-response struct and return:
```go
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	// ... decode into tokenResp; error if AccessToken == "" ...
	tok := &oauth2.Token{AccessToken: tokenResp.AccessToken, TokenType: "Bearer"}
	if tokenResp.ExpiresIn > 0 {
		// Subtract a small skew so we refresh slightly early.
		tok.Expiry = timeNow().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return tok, nil
```
4. Add a `timeNow = time.Now` package var (so it's overridable in tests if needed) OR just call `time.Now()` directly. Add imports `"time"`. (Direct `time.Now()` is fine; no need for the indirection unless a test requires it — the test above uses real sleep.)

Keep the `accessToken`-direct fast path and MFA/profile-selection logic unchanged; only the token-exchange return and the login branch wiring change.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/client/... -v`
Expected: PASS (new test + all existing login tests). Note `TestTokenSource_Login` (accessToken fast-path) still passes because it returns before token exchange.

- [ ] **Step 5: Commit**

```bash
git add internal/client/auth.go internal/client/auth_test.go
git -c commit.gpgsign=false commit -m "feat(client): refreshing login token source via ReuseTokenSource"
```

---

## Task 2: Client admin/operation helpers

**Files:**
- Create: `internal/client/admin.go`, `internal/client/admin_test.go`

- [ ] **Step 1: Write the failing test**

`internal/client/admin_test.go`:

```go
package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetPassword(t *testing.T) {
	var gotBody map[string]string
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/admin/projects/proj1/setpassword" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","id":"ok"}`))
	})
	defer srv.Close()

	if err := c.SetPassword(context.Background(), "proj1", "a@b.com", "supersecret"); err != nil {
		t.Fatalf("SetPassword: %v", err)
	}
	if gotBody["email"] != "a@b.com" || gotBody["password"] != "supersecret" {
		t.Fatalf("unexpected body: %v", gotBody)
	}
}

func TestOperation_PostsToOperationPath(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fhir/R4/Project/$init" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "Parameters") {
			http.Error(w, "want parameters", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"resourceType":"Parameters"}`))
	})
	defer srv.Close()

	_, err := c.Operation(context.Background(), "Project", "", "$init", []byte(`{"resourceType":"Parameters"}`))
	if err != nil {
		t.Fatalf("Operation: %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify failure**

Run: `go test ./internal/client/... -run 'TestSetPassword|TestOperation' -v`
Expected: FAIL — `SetPassword`, `Operation` undefined.

- [ ] **Step 3: Implement `internal/client/admin.go`**

```go
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// Operation invokes a FHIR operation (e.g. "$init", "$rotate-secret"). If id is
// empty, the operation is type-level (/{type}/$op); otherwise instance-level
// (/{type}/{id}/$op). body is the JSON Parameters payload (may be nil).
func (c *Client) Operation(ctx context.Context, resourceType, id, op string, body []byte) ([]byte, error) {
	var url string
	if id == "" {
		url = c.fhirURL(resourceType, op)
	} else {
		url = c.fhirURL(resourceType, id, op)
	}
	return c.do(ctx, http.MethodPost, url, body)
}

// SetPassword sets a user's password via the project-admin endpoint (no email sent).
func (c *Client) SetPassword(ctx context.Context, projectID, email, password string) error {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return err
	}
	url := c.baseURL + "/admin/projects/" + projectID + "/setpassword"
	_, err = c.do(ctx, http.MethodPost, url, body)
	return err
}

// note: c.do sets Content-Type application/fhir+json; the admin endpoint accepts JSON.
var _ = strings.TrimSpace // keep strings import if unused after edits
```

(If `strings` ends up unused, remove the import and the `var _` line.)

- [ ] **Step 4: Run tests**

Run: `go test ./internal/client/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/client/admin.go internal/client/admin_test.go
git -c commit.gpgsign=false commit -m "feat(client): SetPassword and FHIR Operation helpers"
```

---

## Task 3: Shared provider helpers

**Files:**
- Create: `internal/provider/shared.go`

- [ ] **Step 1: Create the helpers**

```go
package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// strOrEmpty returns "" for null/unknown, else the string value.
func strOrEmpty(s types.String) string {
	if s.IsNull() || s.IsUnknown() {
		return ""
	}
	return s.ValueString()
}

// optString returns a types.String that is null when v is empty, else the value.
// Used in Read so absent server fields become null (not "") and don't churn the plan.
func optString(v string) types.String {
	if v == "" {
		return types.StringNull()
	}
	return types.StringValue(v)
}
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean. (No test needed; pure helpers exercised by the resources below.)

- [ ] **Step 3: Commit**

```bash
git add internal/provider/shared.go
git -c commit.gpgsign=false commit -m "feat(provider): shared string helpers for typed resources"
```

---

## Task 4: `medplum_access_policy`

**Files:**
- Create: `internal/provider/access_policy_resource.go`, `internal/provider/access_policy_resource_test.go`

Maps to FHIR `AccessPolicy` (fields confirmed from source): `name`, `resource[] { resourceType, criteria, readonly, hiddenFields[], readonlyFields[], compartment }`, `ipAccessRule[] { name, value, action }`.

- [ ] **Step 1: Create the resource**

```go
package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewAccessPolicyResource() resource.Resource { return &accessPolicyResource{} }

type accessPolicyResource struct{ data *providerData }

type accessPolicyModel struct {
	ID       types.String              `tfsdk:"id"`
	Name     types.String              `tfsdk:"name"`
	Resource []accessPolicyResourceRow `tfsdk:"resource"`
}

type accessPolicyResourceRow struct {
	ResourceType  types.String `tfsdk:"resource_type"`
	Criteria      types.String `tfsdk:"criteria"`
	Readonly      types.Bool   `tfsdk:"readonly"`
	HiddenFields  []string     `tfsdk:"hidden_fields"`
	ReadonlyField []string     `tfsdk:"readonly_fields"`
}

func (r *accessPolicyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_access_policy"
}

func (r *accessPolicyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum AccessPolicy.",
		Attributes: map[string]schema.Attribute{
			"id":   schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name": schema.StringAttribute{Required: true},
		},
		Blocks: map[string]schema.Block{
			"resource": schema.ListNestedBlock{
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"resource_type":   schema.StringAttribute{Required: true},
						"criteria":        schema.StringAttribute{Optional: true},
						"readonly":        schema.BoolAttribute{Optional: true},
						"hidden_fields":   schema.ListAttribute{Optional: true, ElementType: types.StringType},
						"readonly_fields": schema.ListAttribute{Optional: true, ElementType: types.StringType},
					},
				},
			},
		},
	}
}

func (r *accessPolicyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

// toFHIR builds the AccessPolicy JSON from the model. id is included when non-empty (for PUT).
func (m accessPolicyModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{"resourceType": "AccessPolicy", "name": m.Name.ValueString()}
	if id != "" {
		doc["id"] = id
	}
	rows := make([]map[string]any, 0, len(m.Resource))
	for _, row := range m.Resource {
		entry := map[string]any{"resourceType": row.ResourceType.ValueString()}
		if v := strOrEmpty(row.Criteria); v != "" {
			entry["criteria"] = v
		}
		if !row.Readonly.IsNull() && !row.Readonly.IsUnknown() {
			entry["readonly"] = row.Readonly.ValueBool()
		}
		if len(row.HiddenFields) > 0 {
			entry["hiddenFields"] = row.HiddenFields
		}
		if len(row.ReadonlyField) > 0 {
			entry["readonlyFields"] = row.ReadonlyField
		}
		rows = append(rows, entry)
	}
	doc["resource"] = rows
	return json.Marshal(doc)
}

// fromFHIR populates the model's server-derived fields. Only modeled fields are
// read back, so server-managed fields (meta.*) never cause drift.
func (m *accessPolicyModel) fromFHIR(body []byte) error {
	var doc struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Resource []struct {
			ResourceType   string   `json:"resourceType"`
			Criteria       string   `json:"criteria"`
			Readonly       *bool    `json:"readonly"`
			HiddenFields   []string `json:"hiddenFields"`
			ReadonlyFields []string `json:"readonlyFields"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Name = types.StringValue(doc.Name)
	rows := make([]accessPolicyResourceRow, 0, len(doc.Resource))
	for _, row := range doc.Resource {
		rr := accessPolicyResourceRow{
			ResourceType:  types.StringValue(row.ResourceType),
			Criteria:      optString(row.Criteria),
			HiddenFields:  row.HiddenFields,
			ReadonlyField: row.ReadonlyFields,
		}
		if row.Readonly != nil {
			rr.Readonly = types.BoolValue(*row.Readonly)
		} else {
			rr.Readonly = types.BoolNull()
		}
		rows = append(rows, rr)
	}
	m.Resource = rows
	return nil
}

func (r *accessPolicyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m accessPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := m.toFHIR("")
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "AccessPolicy", body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *accessPolicyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m accessPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "AccessPolicy", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *accessPolicyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state accessPolicyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := plan.toFHIR(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "AccessPolicy", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *accessPolicyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m accessPolicyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "AccessPolicy", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *accessPolicyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*accessPolicyResource)(nil)
	_ resource.ResourceWithConfigure   = (*accessPolicyResource)(nil)
	_ resource.ResourceWithImportState = (*accessPolicyResource)(nil)
)
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean (resource not yet registered; that's Task 9). If the framework requires registration to build, it does not — standalone types compile.

- [ ] **Step 3: Write the acceptance test**

`internal/provider/access_policy_resource_test.go`:

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAccessPolicy_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "medplum_access_policy" "test" {
  name = "tf-acc-policy"
  resource {
    resource_type = "Patient"
    criteria      = "Patient?_id=%patient.id"
    readonly      = true
    hidden_fields = ["telecom"]
  }
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_access_policy.test", "id"),
					resource.TestCheckResourceAttr("medplum_access_policy.test", "resource.0.resource_type", "Patient"),
					resource.TestCheckResourceAttr("medplum_access_policy.test", "resource.0.readonly", "true"),
				),
			},
			{Config: `
resource "medplum_access_policy" "test" {
  name = "tf-acc-policy"
  resource {
    resource_type = "Patient"
    criteria      = "Patient?_id=%patient.id"
    readonly      = true
    hidden_fields = ["telecom"]
  }
}`, PlanOnly: true},
			{
				ResourceName:      "medplum_access_policy.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
		},
	})
}
```

- [ ] **Step 4: Verify it compiles + skips**

Run: `go test ./internal/provider/... -run TestAccAccessPolicy -v`
Expected: SKIP (no TF_ACC), package compiles.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/access_policy_resource.go internal/provider/access_policy_resource_test.go
git -c commit.gpgsign=false commit -m "feat(provider): medplum_access_policy resource"
```

---

## Task 5: `medplum_client_application`

**Files:**
- Create: `internal/provider/client_application_resource.go`, `internal/provider/client_application_resource_test.go`

**Secret handling (important):** Medplum's `$rotate-secret` cannot mint an initial secret (it requires the current secret to match), and the admin `/client` endpoint is intentionally avoided. So the provider **generates** the secret on create (crypto/rand) unless the user supplies one. `secret` is `Optional + Computed + Sensitive`.

- [ ] **Step 1: Create the resource**

```go
package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewClientApplicationResource() resource.Resource { return &clientApplicationResource{} }

type clientApplicationResource struct{ data *providerData }

type clientApplicationModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	RedirectURI types.String `tfsdk:"redirect_uri"`
	Secret      types.String `tfsdk:"secret"`
}

func (r *clientApplicationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_client_application"
}

func (r *clientApplicationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum ClientApplication. Access is granted separately via medplum_project_membership (accessPolicy lives on the membership).",
		Attributes: map[string]schema.Attribute{
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":         schema.StringAttribute{Required: true},
			"description":  schema.StringAttribute{Optional: true},
			"redirect_uri": schema.StringAttribute{Optional: true},
			"secret": schema.StringAttribute{
				Optional: true, Computed: true, Sensitive: true,
				MarkdownDescription: "Client secret. Generated by the provider if not set.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *clientApplicationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

func generateClientSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (m clientApplicationModel) toFHIR(id, secret string) ([]byte, error) {
	doc := map[string]any{"resourceType": "ClientApplication", "name": m.Name.ValueString(), "secret": secret}
	if id != "" {
		doc["id"] = id
	}
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	}
	if v := strOrEmpty(m.RedirectURI); v != "" {
		doc["redirectUri"] = v
	}
	return json.Marshal(doc)
}

func (m *clientApplicationModel) fromFHIR(body []byte) error {
	var doc struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		RedirectURI string `json:"redirectUri"`
		Secret      string `json:"secret"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Name = types.StringValue(doc.Name)
	m.Description = optString(doc.Description)
	m.RedirectURI = optString(doc.RedirectURI)
	m.Secret = types.StringValue(doc.Secret)
	return nil
}

func (r *clientApplicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m clientApplicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	secret := strOrEmpty(m.Secret)
	if secret == "" {
		var err error
		secret, err = generateClientSecret()
		if err != nil {
			resp.Diagnostics.AddError("Secret generation failed", err.Error())
			return
		}
	}
	body, err := m.toFHIR("", secret)
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "ClientApplication", body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *clientApplicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m clientApplicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "ClientApplication", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *clientApplicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state clientApplicationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Preserve the existing secret unless the user explicitly set a new one.
	secret := strOrEmpty(plan.Secret)
	if secret == "" {
		secret = state.Secret.ValueString()
	}
	body, err := plan.toFHIR(state.ID.ValueString(), secret)
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "ClientApplication", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *clientApplicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m clientApplicationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "ClientApplication", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *clientApplicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*clientApplicationResource)(nil)
	_ resource.ResourceWithConfigure   = (*clientApplicationResource)(nil)
	_ resource.ResourceWithImportState = (*clientApplicationResource)(nil)
)
```

- [ ] **Step 2: Write a unit test for secret generation**

`internal/provider/client_application_resource_test.go`:

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestGenerateClientSecret_UniqueAndLong(t *testing.T) {
	a, err := generateClientSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := generateClientSecret()
	if a == b {
		t.Fatal("expected unique secrets")
	}
	if len(a) < 40 {
		t.Fatalf("secret too short: %d", len(a))
	}
}

func TestAccClientApplication_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "medplum_client_application" "test" {
  name        = "tf-acc-client"
  description = "acc test"
}`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_client_application.test", "secret"),
				),
			},
			{Config: `
resource "medplum_client_application" "test" {
  name        = "tf-acc-client"
  description = "acc test"
}`, PlanOnly: true},
			{
				ResourceName:            "medplum_client_application.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"secret"},
			},
		},
	})
}
```

- [ ] **Step 3: Run unit test + verify acc skips**

Run: `go test ./internal/provider/... -run 'TestGenerateClientSecret|TestAccClientApplication' -v`
Expected: `TestGenerateClientSecret_UniqueAndLong` PASS; acc test SKIP.

- [ ] **Step 4: Commit**

```bash
git add internal/provider/client_application_resource.go internal/provider/client_application_resource_test.go
git -c commit.gpgsign=false commit -m "feat(provider): medplum_client_application with provider-generated secret"
```

---

## Task 6: `medplum_project_membership`

**Files:**
- Create: `internal/provider/project_membership_resource.go`, `internal/provider/project_membership_resource_test.go`

Generic binder. `project`/`user`/`profile` are ForceNew; `access_policy`/`admin` update in place. Plain FHIR delete + project-owner guard (read the project, refuse if the membership's user is the owner).

- [ ] **Step 1: Create the resource**

```go
package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewProjectMembershipResource() resource.Resource { return &projectMembershipResource{} }

type projectMembershipResource struct{ data *providerData }

type projectMembershipModel struct {
	ID           types.String `tfsdk:"id"`
	Project      types.String `tfsdk:"project"`
	User         types.String `tfsdk:"user"`
	Profile      types.String `tfsdk:"profile"`
	AccessPolicy types.String `tfsdk:"access_policy"`
	Admin        types.Bool   `tfsdk:"admin"`
}

func (r *projectMembershipResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project_membership"
}

func (r *projectMembershipResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	rr := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Binds a profile (ClientApplication, User, Practitioner, ...) to a Medplum Project with an access policy.",
		Attributes: map[string]schema.Attribute{
			"id":            schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"project":       schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "Project reference, e.g. Project/abc."},
			"user":          schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "User/ClientApplication/Bot reference."},
			"profile":       schema.StringAttribute{Required: true, PlanModifiers: rr, MarkdownDescription: "Profile reference (Practitioner/Patient/ClientApplication/...)."},
			"access_policy": schema.StringAttribute{Optional: true, MarkdownDescription: "AccessPolicy reference."},
			"admin":         schema.BoolAttribute{Optional: true},
		},
	}
}

func (r *projectMembershipResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

func refObj(ref string) map[string]any { return map[string]any{"reference": ref} }

func (m projectMembershipModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{
		"resourceType": "ProjectMembership",
		"project":      refObj(m.Project.ValueString()),
		"user":         refObj(m.User.ValueString()),
		"profile":      refObj(m.Profile.ValueString()),
	}
	if id != "" {
		doc["id"] = id
	}
	if v := strOrEmpty(m.AccessPolicy); v != "" {
		doc["accessPolicy"] = refObj(v)
	}
	if !m.Admin.IsNull() && !m.Admin.IsUnknown() {
		doc["admin"] = m.Admin.ValueBool()
	}
	return json.Marshal(doc)
}

func (m *projectMembershipModel) fromFHIR(body []byte) error {
	var doc struct {
		ID      string `json:"id"`
		Project struct {
			Reference string `json:"reference"`
		} `json:"project"`
		User struct {
			Reference string `json:"reference"`
		} `json:"user"`
		Profile struct {
			Reference string `json:"reference"`
		} `json:"profile"`
		AccessPolicy struct {
			Reference string `json:"reference"`
		} `json:"accessPolicy"`
		Admin *bool `json:"admin"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Project = types.StringValue(doc.Project.Reference)
	m.User = types.StringValue(doc.User.Reference)
	m.Profile = types.StringValue(doc.Profile.Reference)
	m.AccessPolicy = optString(doc.AccessPolicy.Reference)
	if doc.Admin != nil {
		m.Admin = types.BoolValue(*doc.Admin)
	} else {
		m.Admin = types.BoolNull()
	}
	return nil
}

// isProjectOwner reads the project and reports whether userRef is its owner.
func (r *projectMembershipResource) isProjectOwner(ctx context.Context, projectRef, userRef string) (bool, error) {
	// projectRef is like "Project/abc"; FHIRRead wants the bare id.
	id := projectRef
	for i := 0; i < len(projectRef); i++ {
		if projectRef[i] == '/' {
			id = projectRef[i+1:]
			break
		}
	}
	out, err := r.data.Client.FHIRRead(ctx, "Project", id)
	if err != nil {
		return false, err
	}
	var proj struct {
		Owner struct {
			Reference string `json:"reference"`
		} `json:"owner"`
	}
	if err := json.Unmarshal(out, &proj); err != nil {
		return false, err
	}
	return proj.Owner.Reference != "" && proj.Owner.Reference == userRef, nil
}

func (r *projectMembershipResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := m.toFHIR("")
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "ProjectMembership", body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectMembershipResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "ProjectMembership", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectMembershipResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectMembershipModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := plan.toFHIR(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "ProjectMembership", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectMembershipResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectMembershipModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Owner guard: refuse to delete the project owner's membership (would brick the project).
	owner, err := r.isProjectOwner(ctx, m.Project.ValueString(), m.User.ValueString())
	if err == nil && owner {
		resp.Diagnostics.AddError("Refusing to delete project owner membership",
			"This membership belongs to the project owner; deleting it can render the project unusable. Remove the project instead.")
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "ProjectMembership", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *projectMembershipResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*projectMembershipResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectMembershipResource)(nil)
	_ resource.ResourceWithImportState = (*projectMembershipResource)(nil)
)
```

- [ ] **Step 2: Build**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 3: Write the acceptance test (composition: policy + client + membership)**

`internal/provider/project_membership_resource_test.go`:

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const testAccMembershipConfig = `
resource "medplum_access_policy" "p" {
  name = "tf-acc-mbr-policy"
  resource { resource_type = "Patient" }
}

resource "medplum_client_application" "c" {
  name = "tf-acc-mbr-client"
}

resource "medplum_project_membership" "m" {
  project       = "Project/${var_project_id}"
  user          = medplum_client_application.c.id
  profile       = medplum_client_application.c.id
  access_policy = medplum_access_policy.p.id
}
`

func TestAccProjectMembership_bindsClient(t *testing.T) {
	t.Skip("requires a known project id; enable once project bootstrap exposes MEDPLUM_TEST_PROJECT_ID")
	// Implementation note for the executor: replace ${var_project_id} with the
	// CI project id (from MEDPLUM_TEST_PROJECT_ID) and unskip. medplum_client_application.c.id
	// is "ClientApplication/<uuid>"; user and profile both reference the client.
	_ = testAccMembershipConfig
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps:                    []resource.TestStep{},
	})
}
```

(The membership acceptance test is skipped pending a known project id in CI — see Task 9 Step 4 / the bootstrap follow-up. Unit-level coverage of `toFHIR`/`fromFHIR` and the owner guard is added next.)

- [ ] **Step 4: Add unit tests for encoding + owner-ref parsing**

Append to `internal/provider/project_membership_resource_test.go`:

```go
import "encoding/json"

func TestProjectMembership_toFHIR(t *testing.T) {
	m := projectMembershipModel{
		Project:      typesStr("Project/p1"),
		User:         typesStr("ClientApplication/c1"),
		Profile:      typesStr("ClientApplication/c1"),
		AccessPolicy: typesStr("AccessPolicy/a1"),
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	if doc["resourceType"] != "ProjectMembership" {
		t.Fatalf("bad resourceType: %v", doc["resourceType"])
	}
	ap, _ := doc["accessPolicy"].(map[string]any)
	if ap["reference"] != "AccessPolicy/a1" {
		t.Fatalf("bad accessPolicy ref: %v", doc["accessPolicy"])
	}
}
```

Add a tiny helper at the bottom of the test file:
```go
func typesStr(s string) types.String { return types.StringValue(s) }
```
and import `"github.com/hashicorp/terraform-plugin-framework/types"`.

- [ ] **Step 5: Run + commit**

Run: `go test ./internal/provider/... -run 'TestProjectMembership|TestAccProjectMembership' -v`
Expected: `TestProjectMembership_toFHIR` PASS; acc test SKIP.

```bash
git add internal/provider/project_membership_resource.go internal/provider/project_membership_resource_test.go
git -c commit.gpgsign=false commit -m "feat(provider): medplum_project_membership with owner guard"
```

---

## Task 7: `medplum_user`

**Files:**
- Create: `internal/provider/user_resource.go`, `internal/provider/user_resource_test.go`

`User` FHIR CRUD + optional write-only `password` via `/setpassword`. `password` requires `email` + project scope (validated at plan).

- [ ] **Step 1: Create the resource**

```go
package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewUserResource() resource.Resource { return &userResource{} }

type userResource struct{ data *providerData }

type userModel struct {
	ID          types.String `tfsdk:"id"`
	FirstName   types.String `tfsdk:"first_name"`
	LastName    types.String `tfsdk:"last_name"`
	Email       types.String `tfsdk:"email"`
	ExternalID  types.String `tfsdk:"external_id"`
	ProjectID   types.String `tfsdk:"project_id"`
	Admin       types.Bool   `tfsdk:"admin"`
	Password    types.String `tfsdk:"password"`
}

func (r *userResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *userResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum User. Project-scoped when project_id is set (else server-scoped).",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"first_name":  schema.StringAttribute{Required: true},
			"last_name":   schema.StringAttribute{Required: true},
			"email":       schema.StringAttribute{Optional: true},
			"external_id": schema.StringAttribute{Optional: true},
			"project_id":  schema.StringAttribute{Optional: true, MarkdownDescription: "Project id (bare uuid). Sets project scope and is required when password is set.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
			"admin":       schema.BoolAttribute{Optional: true},
			"password":    schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Write-only. Applied via /setpassword (no email). Requires email + project_id. Not read back."},
		},
	}
}

func (r *userResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

func (r *userResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m userModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !m.Password.IsNull() && !m.Password.IsUnknown() && m.Password.ValueString() != "" {
		if strOrEmpty(m.Email) == "" || strOrEmpty(m.ProjectID) == "" {
			resp.Diagnostics.AddAttributeError(path.Root("password"), "password requires email + project_id",
				"Medplum /setpassword is project-scoped and keyed by email; set both email and project_id when using password.")
		}
	}
}

func (m userModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{
		"resourceType": "User",
		"firstName":    m.FirstName.ValueString(),
		"lastName":     m.LastName.ValueString(),
	}
	if id != "" {
		doc["id"] = id
	}
	if v := strOrEmpty(m.Email); v != "" {
		doc["email"] = v
	}
	if v := strOrEmpty(m.ExternalID); v != "" {
		doc["externalId"] = v
	}
	if v := strOrEmpty(m.ProjectID); v != "" {
		doc["project"] = refObj("Project/" + v)
	}
	if !m.Admin.IsNull() && !m.Admin.IsUnknown() {
		doc["admin"] = m.Admin.ValueBool()
	}
	return json.Marshal(doc)
}

// fromFHIR populates server-derived fields. Password is never read back.
func (m *userModel) fromFHIR(body []byte) error {
	var doc struct {
		ID         string `json:"id"`
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
		Email      string `json:"email"`
		ExternalID string `json:"externalId"`
		Project    struct {
			Reference string `json:"reference"`
		} `json:"project"`
		Admin *bool `json:"admin"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.FirstName = types.StringValue(doc.FirstName)
	m.LastName = types.StringValue(doc.LastName)
	m.Email = optString(doc.Email)
	m.ExternalID = optString(doc.ExternalID)
	// project reference "Project/<id>" → bare id; null when server-scoped.
	if ref := doc.Project.Reference; len(ref) > len("Project/") {
		m.ProjectID = types.StringValue(ref[len("Project/"):])
	} else {
		m.ProjectID = types.StringNull()
	}
	if doc.Admin != nil {
		m.Admin = types.BoolValue(*doc.Admin)
	} else {
		m.Admin = types.BoolNull()
	}
	return nil
}

// applyPassword sets the password via /setpassword when configured.
func (r *userResource) applyPassword(ctx context.Context, m userModel) error {
	pw := strOrEmpty(m.Password)
	if pw == "" {
		return nil
	}
	return r.data.Client.SetPassword(ctx, m.ProjectID.ValueString(), m.Email.ValueString(), pw)
}

func (r *userResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pwPlan := m.Password // preserve write-only value for state
	body, err := m.toFHIR("")
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, "User", body)
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	m.Password = pwPlan
	if err := r.applyPassword(ctx, m); err != nil {
		resp.Diagnostics.AddError("setpassword failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *userResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pw := m.Password
	out, err := r.data.Client.FHIRRead(ctx, "User", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	m.Password = pw // keep write-only value from prior state (never read from server)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *userResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state userModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	pwPlan := plan.Password
	body, err := plan.toFHIR(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "User", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	plan.Password = pwPlan
	if err := r.applyPassword(ctx, plan); err != nil {
		resp.Diagnostics.AddError("setpassword failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *userResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m userModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "User", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *userResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                   = (*userResource)(nil)
	_ resource.ResourceWithConfigure      = (*userResource)(nil)
	_ resource.ResourceWithImportState    = (*userResource)(nil)
	_ resource.ResourceWithValidateConfig = (*userResource)(nil)
)
```

- [ ] **Step 2: Unit test for the password precondition encoding**

`internal/provider/user_resource_test.go`:

```go
package provider

import (
	"encoding/json"
	"testing"
)

func TestUser_toFHIR_ProjectScope(t *testing.T) {
	m := userModel{
		FirstName: typesStr("Jane"),
		LastName:  typesStr("Doe"),
		Email:     typesStr("jane@example.com"),
		ProjectID: typesStr("p1"),
	}
	b, err := m.toFHIR("")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	_ = json.Unmarshal(b, &doc)
	proj, _ := doc["project"].(map[string]any)
	if proj["reference"] != "Project/p1" {
		t.Fatalf("bad project ref: %v", doc["project"])
	}
}
```

(`typesStr` is defined in `project_membership_resource_test.go`, same package.)

- [ ] **Step 3: Run + commit**

Run: `go test ./internal/provider/... -run TestUser -v`
Expected: PASS.

```bash
git add internal/provider/user_resource.go internal/provider/user_resource_test.go
git -c commit.gpgsign=false commit -m "feat(provider): medplum_user with optional setpassword"
```

---

## Task 8: `medplum_project`

**Files:**
- Create: `internal/provider/project_resource.go`, `internal/provider/project_resource_test.go`

Create via `POST /Project/$init` (Parameters: `name`), then PUT the full resource if extra fields are set. Requires super-admin auth.

- [ ] **Step 1: Create the resource**

```go
package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
)

func NewProjectResource() resource.Resource { return &projectResource{} }

type projectResource struct{ data *providerData }

type projectModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Features    []string     `tfsdk:"features"`
}

func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Medplum Project. Creation requires super-admin credentials.",
		Attributes: map[string]schema.Attribute{
			"id":          schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true},
			"features":    schema.ListAttribute{Optional: true, ElementType: types.StringType},
		},
	}
}

func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	d, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = d
}

func (m projectModel) toFHIR(id string) ([]byte, error) {
	doc := map[string]any{"resourceType": "Project", "name": m.Name.ValueString(), "id": id}
	if v := strOrEmpty(m.Description); v != "" {
		doc["description"] = v
	}
	if len(m.Features) > 0 {
		doc["features"] = m.Features
	}
	return json.Marshal(doc)
}

func (m *projectModel) fromFHIR(body []byte) error {
	var doc struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Features    []string `json:"features"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	m.ID = types.StringValue(doc.ID)
	m.Name = types.StringValue(doc.Name)
	m.Description = optString(doc.Description)
	m.Features = doc.Features
	return nil
}

// initProject calls Project/$init and returns the new project's bare id.
func (r *projectResource) initProject(ctx context.Context, name string) (string, error) {
	params := map[string]any{
		"resourceType": "Parameters",
		"parameter":    []map[string]any{{"name": "name", "valueString": name}},
	}
	body, err := json.Marshal(params)
	if err != nil {
		return "", err
	}
	out, err := r.data.Client.Operation(ctx, "Project", "", "$init", body)
	if err != nil {
		return "", err
	}
	// $init returns a Parameters resource wrapping the created Project, OR the
	// Project directly depending on version. Handle both: look for an id at the
	// top level, else dig into parameter[].resource.
	var top struct {
		ResourceType string `json:"resourceType"`
		ID           string `json:"id"`
		Parameter    []struct {
			Name     string          `json:"name"`
			Resource json.RawMessage `json:"resource"`
		} `json:"parameter"`
	}
	if err := json.Unmarshal(out, &top); err != nil {
		return "", err
	}
	if top.ResourceType == "Project" && top.ID != "" {
		return top.ID, nil
	}
	for _, p := range top.Parameter {
		if len(p.Resource) > 0 {
			var res struct {
				ResourceType string `json:"resourceType"`
				ID           string `json:"id"`
			}
			if json.Unmarshal(p.Resource, &res) == nil && res.ResourceType == "Project" && res.ID != "" {
				return res.ID, nil
			}
		}
	}
	return "", fmt.Errorf("Project/$init response did not contain a Project id: %s", string(out))
}

func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id, err := r.initProject(ctx, m.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Project init failed", err.Error()+"\n(Project creation requires super-admin credentials.)")
		return
	}
	// $init only sets name; if extra fields are configured, PUT the full resource.
	if strOrEmpty(m.Description) != "" || len(m.Features) > 0 {
		body, encErr := m.toFHIR(id)
		if encErr != nil {
			resp.Diagnostics.AddError("Encoding failed", encErr.Error())
			return
		}
		out, updErr := r.data.Client.FHIRUpdate(ctx, "Project", id, body)
		if updErr != nil {
			resp.Diagnostics.AddError("Project configure failed", updErr.Error())
			return
		}
		if err := m.fromFHIR(out); err != nil {
			resp.Diagnostics.AddError("Decoding failed", err.Error())
			return
		}
	} else {
		m.ID = types.StringValue(id)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, "Project", m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	if err := m.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := plan.toFHIR(state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Encoding failed", err.Error())
		return
	}
	out, err := r.data.Client.FHIRUpdate(ctx, "Project", state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	if err := plan.fromFHIR(out); err != nil {
		resp.Diagnostics.AddError("Decoding failed", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, "Project", m.ID.ValueString()); err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)
```

- [ ] **Step 2: Unit test for `$init` response parsing**

`internal/provider/project_resource_test.go`:

```go
package provider

import "testing"

func TestProject_toFHIR(t *testing.T) {
	m := projectModel{Name: typesStr("Acme"), Description: typesStr("d"), Features: []string{"bots"}}
	b, err := m.toFHIR("p1")
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(b), `"id":"p1"`) || !contains(string(b), `"bots"`) {
		t.Fatalf("unexpected body: %s", b)
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: Run + commit**

Run: `go test ./internal/provider/... -run TestProject_toFHIR -v`
Expected: PASS.

```bash
git add internal/provider/project_resource.go internal/provider/project_resource_test.go
git -c commit.gpgsign=false commit -m "feat(provider): medplum_project via Project/\$init"
```

---

## Task 9: Register resources + provider acceptance wiring

**Files:**
- Modify: `internal/provider/provider.go`

- [ ] **Step 1: Register all five resources**

In `provider.go`, replace the `Resources` method body:

```go
func (p *medplumProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewFHIRResource,
		NewAccessPolicyResource,
		NewClientApplicationResource,
		NewProjectMembershipResource,
		NewUserResource,
		NewProjectResource,
	}
}
```

- [ ] **Step 2: Build, vet, unit tests**

Run: `gofmt -w . && go vet ./... && go test ./... -count=1`
Expected: build clean; all unit tests pass; acceptance tests skip.

- [ ] **Step 3: Commit**

```bash
git add internal/provider/provider.go
git -c commit.gpgsign=false commit -m "feat(provider): register typed resources"
```

- [ ] **Step 4: (Executor note) project id for membership acceptance**

The membership acceptance test (`TestAccProjectMembership_bindsClient`) is skipped pending a known
project id. When wiring it: export `MEDPLUM_TEST_PROJECT_ID` in the CI `acceptance` job (the seeded
super-admin's default project id, obtainable via `GET /admin/projects` or the login membership),
read it with `os.Getenv` in the test, substitute into the config, and remove the `t.Skip`. This is
a small CI follow-up, not a code change to the resources.

---

## Self-Review (completed during plan authoring)

**Spec coverage:**
- Refreshing login token source → Task 1. ✓
- `access_policy` → Task 4; `client_application` (provider-generated secret) → Task 5;
  `project_membership` (owner guard, plain delete) → Task 6; `user` (+setpassword, plan-time
  precondition) → Task 7; `project` ($init + PUT, super-admin) → Task 8. ✓
- Typed-resource drift via reading only modeled fields (no `Contains` needed) → applied in every
  `fromFHIR`. ✓
- Registration → Task 9. ✓
- Bot, profile, invite excluded (non-goals). ✓

**Deviations from spec (intentional, with reason):**
- `client_application` secret is **provider-generated**, not via `$rotate-secret`: the operation
  cannot mint an initial secret (rotatesecret.ts requires the current secret to match). Documented
  in Task 5. `$rotate-secret`-based rotation can be a later enhancement.
- `project` create is **`$init` (name) + follow-up PUT** for other fields: `$init` only accepts
  `name`/owner. Documented in Task 8.

**Type consistency:** `providerData`, `client.Client.FHIR{Create,Read,Update,Delete}`,
`client.IsNotFound`, `client.SetPassword`, `client.Operation`, `strOrEmpty`, `optString`, `refObj`,
`typesStr` (test helper) are defined once and reused. Each resource's `toFHIR`/`fromFHIR` are method
pairs on its own model — no cross-resource name collisions (distinct receiver types).

**Open items surfaced (not placeholders):**
- Membership acceptance needs `MEDPLUM_TEST_PROJECT_ID` (Task 9 Step 4).
- `project` acceptance requires super-admin (the CI super-admin login provides it); verify `$init`
  succeeds against the pinned image on first run.
