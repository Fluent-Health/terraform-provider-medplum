# Medplum Terraform Provider — Plan 1: Foundation + Generic FHIR Resource

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up a Go Terraform provider (`terraform-plugin-framework`) for Medplum with gateway-agnostic multi-method auth, a plan-time R4 JSON-schema validator, a generic `medplum_fhir_resource` with stable drift handling, and live-Medplum acceptance tests in GitHub Actions.

**Architecture:** A thin `client` package owns auth (three methods, all reduced to an `oauth2.TokenSource`) and HTTP calls to Medplum; a `fhirschema` package embeds the R4 schema and validates a JSON document against `#/definitions/<resourceType>`; the `provider` package wires Terraform config to the client; `medplum_fhir_resource` maps a JSON `body` string to FHIR CRUD, validating at plan time and normalizing server-managed fields to avoid perpetual diffs.

**Tech Stack:** Go 1.23, `github.com/hashicorp/terraform-plugin-framework`, `github.com/hashicorp/terraform-plugin-testing`, `github.com/santhosh-tekuri/jsonschema/v6`, `golang.org/x/oauth2`, Docker Compose Medplum, GoReleaser (later plan).

**Scope note:** This is Plan 1 of 3. Plan 2 = typed resources (`access_policy`, `client_application`, `project_membership`, `user`, `project`). Plan 3 = docs + GoReleaser + registry publish. See `docs/superpowers/specs/2026-06-03-terraform-provider-medplum-design.md`.

**Conventions for every task:** Run `gofmt -w` and `go vet ./...` before committing. Commit messages use Conventional Commits. Each commit footer ends with:
```
Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>
```

---

## File Structure

| Path | Responsibility |
| --- | --- |
| `go.mod`, `go.sum` | Module `github.com/Fluent-Health/terraform-provider-medplum`, Go 1.23, deps. |
| `main.go` | `providerserver.Serve` entrypoint. |
| `Makefile` | `build`, `test`, `testacc`, `fmt`, `vet`, `lint` targets. |
| `internal/client/client.go` | `Client` struct, `Config`, `New`, base HTTP request helper. |
| `internal/client/auth.go` | Builds an `oauth2.TokenSource` from the three auth methods. |
| `internal/client/fhir.go` | `FHIRCreate/Read/Update/Delete`, `OperationOutcome` error mapping. |
| `internal/client/*_test.go` | `httptest`-backed unit tests for auth + FHIR. |
| `internal/fhirschema/data/fhir.schema.json` | Embedded HL7 R4 schema (draft-06). |
| `internal/fhirschema/validate.go` | `Validator` with `Validate(resourceType, bodyJSON)`. |
| `internal/fhirschema/validate_test.go` | Validator unit tests. |
| `internal/fhirjson/normalize.go` | Canonicalize JSON + strip server-managed fields for diffing. |
| `internal/fhirjson/normalize_test.go` | Normalization unit tests. |
| `internal/provider/provider.go` | Provider definition, schema, `Configure`. |
| `internal/provider/provider_test.go` | Provider/config unit tests + `testAccProtoV6ProviderFactories`. |
| `internal/provider/fhir_resource.go` | `medplum_fhir_resource`. |
| `internal/provider/fhir_resource_test.go` | Acceptance test. |
| `.github/workflows/ci.yml` | lint + unit + acceptance (docker-compose Medplum). |
| `docker-compose.test.yml` | Postgres + Redis + Medplum server for CI/local acc tests. |

---

## Task 1: Repo scaffold and buildable empty provider

**Files:**
- Create: `go.mod`, `main.go`, `Makefile`, `.gitignore`, `internal/provider/provider.go`

- [ ] **Step 1: Initialize the module and add dependencies**

Run:
```bash
cd /home/ivan/Developer/terraform-provider-medplum
go mod init github.com/Fluent-Health/terraform-provider-medplum
go get github.com/hashicorp/terraform-plugin-framework@v1.13.0
go get github.com/hashicorp/terraform-plugin-go@v0.25.0
go get golang.org/x/oauth2@v0.24.0
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.1
go get github.com/hashicorp/terraform-plugin-testing@v1.10.0
```
Expected: `go.mod` lists Go 1.23 and these requires.

- [ ] **Step 2: Create `.gitignore`**

```gitignore
# Binaries
terraform-provider-medplum
dist/
# Go
*.test
coverage.out
# Terraform
.terraform/
*.tfstate
*.tfstate.*
.terraformrc
terraform.rc
```

- [ ] **Step 3: Create the minimal provider in `internal/provider/provider.go`**

```go
package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &medplumProvider{version: version}
	}
}

type medplumProvider struct {
	version string
}

func (p *medplumProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "medplum"
	resp.Version = p.version
}

func (p *medplumProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{}
}

func (p *medplumProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *medplumProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}

func (p *medplumProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
```

- [ ] **Step 4: Create `main.go`**

```go
package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/provider"
)

// version is set by the release build via -ldflags.
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "set to run the provider with debugger support")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/Fluent-Health/medplum",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
```

- [ ] **Step 5: Create `Makefile`**

```makefile
default: build

build:
	go build -o terraform-provider-medplum

fmt:
	gofmt -w .

vet:
	go vet ./...

test:
	go test ./... -count=1

testacc:
	TF_ACC=1 go test ./... -count=1 -v -timeout 30m

.PHONY: default build fmt vet test testacc
```

- [ ] **Step 6: Verify it builds and vets**

Run: `go mod tidy && go build ./... && go vet ./...`
Expected: no errors; `terraform-provider-medplum` is buildable.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum main.go Makefile .gitignore internal/provider/provider.go
git commit -m "feat: scaffold terraform-plugin-framework provider skeleton"
```

---

## Task 2: Client config + auth token sources

**Files:**
- Create: `internal/client/client.go`, `internal/client/auth.go`, `internal/client/auth_test.go`

- [ ] **Step 1: Write the failing auth test in `internal/client/auth_test.go`**

```go
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenSource_ClientCredentials(t *testing.T) {
	var gotGrant string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotGrant = r.Form.Get("grant_type")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "abc123", "token_type": "Bearer", "expires_in": 3600})
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, TokenURL: srv.URL + "/oauth2/token", ClientID: "id", ClientSecret: "secret"}
	ts, err := cfg.tokenSource(context.Background())
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "abc123" {
		t.Fatalf("got token %q", tok.AccessToken)
	}
	if gotGrant != "client_credentials" {
		t.Fatalf("got grant %q", gotGrant)
	}
}

func TestTokenSource_StaticToken(t *testing.T) {
	cfg := Config{BaseURL: "https://example.com", AccessToken: "static-tok"}
	ts, err := cfg.tokenSource(context.Background())
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	tok, _ := ts.Token()
	if tok.AccessToken != "static-tok" {
		t.Fatalf("got token %q", tok.AccessToken)
	}
}

func TestTokenSource_Login(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/login" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"accessToken": "login-tok"})
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Email: "a@b.com", Password: "pw"}
	ts, err := cfg.tokenSource(context.Background())
	if err != nil {
		t.Fatalf("tokenSource: %v", err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "login-tok" {
		t.Fatalf("got token %q", tok.AccessToken)
	}
}

func TestConfig_Validate_ExactlyOneMethod(t *testing.T) {
	if err := (Config{BaseURL: "x"}).Validate(); err == nil {
		t.Fatal("expected error for no auth method")
	}
	if err := (Config{BaseURL: "x", AccessToken: "t", ClientID: "c", ClientSecret: "s"}).Validate(); err == nil {
		t.Fatal("expected error for multiple auth methods")
	}
	if err := (Config{BaseURL: "x", AccessToken: "t"}).Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/client/... -run TestTokenSource -v`
Expected: FAIL — `Config`, `tokenSource`, `Validate` undefined.

- [ ] **Step 3: Create `internal/client/client.go`**

```go
package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Config holds connection + auth settings. Exactly one auth method must be set:
// (client_id+client_secret) OR access_token OR (email+password).
type Config struct {
	BaseURL      string
	FHIRPath     string // default "/fhir/R4"
	TokenURL     string // default BaseURL + "/oauth2/token"
	ClientID     string
	ClientSecret string
	AccessToken  string
	Email        string
	Password     string

	HTTPClient *http.Client // optional; defaults to http.DefaultClient
}

func (c Config) hasClientCreds() bool { return c.ClientID != "" && c.ClientSecret != "" }
func (c Config) hasToken() bool       { return c.AccessToken != "" }
func (c Config) hasLogin() bool       { return c.Email != "" && c.Password != "" }

// Validate ensures exactly one auth method is configured.
func (c Config) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	n := 0
	if c.hasClientCreds() {
		n++
	}
	if c.hasToken() {
		n++
	}
	if c.hasLogin() {
		n++
	}
	if n == 0 {
		return fmt.Errorf("one auth method is required: client_id+client_secret, access_token, or email+password")
	}
	if n > 1 {
		return fmt.Errorf("exactly one auth method may be set, got %d", n)
	}
	return nil
}

func (c Config) fhirPath() string {
	if c.FHIRPath == "" {
		return "/fhir/R4"
	}
	return "/" + strings.Trim(c.FHIRPath, "/")
}

func (c Config) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return strings.TrimRight(c.BaseURL, "/") + "/oauth2/token"
}

// Client is an authenticated Medplum HTTP client.
type Client struct {
	baseURL  string
	fhirPath string
	httpClient *http.Client
}

// New validates the config and returns a Client whose underlying transport
// injects a bearer token from the configured auth method.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	// Inject the base HTTP client into ctx BEFORE building the token source:
	// clientcredentials.TokenSource captures ctx, so the custom client must be
	// present for token refresh to use it (not just for FHIR requests).
	base := cfg.HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	ts, err := cfg.tokenSource(ctx)
	if err != nil {
		return nil, err
	}
	return &Client{
		baseURL:    strings.TrimRight(cfg.BaseURL, "/"),
		fhirPath:   cfg.fhirPath(),
		httpClient: oauth2.NewClient(ctx, ts),
	}, nil
}
```

- [ ] **Step 4: Create `internal/client/auth.go`**

```go
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// tokenSource reduces all three auth methods to an oauth2.TokenSource.
func (c Config) tokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	switch {
	case c.hasToken():
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: c.AccessToken, TokenType: "Bearer"}), nil
	case c.hasClientCreds():
		cc := &clientcredentials.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			TokenURL:     c.tokenURL(),
			AuthStyle:    oauth2.AuthStyleInParams,
		}
		return cc.TokenSource(ctx), nil
	case c.hasLogin():
		tok, err := c.login(ctx)
		if err != nil {
			return nil, err
		}
		return oauth2.StaticTokenSource(tok), nil
	default:
		return nil, fmt.Errorf("no auth method configured")
	}
}

// login performs Medplum email+password login and returns the access token.
func (c Config) login(ctx context.Context) (*oauth2.Token, error) {
	body, _ := json.Marshal(map[string]string{"email": c.Email, "password": c.Password})
	url := strings.TrimRight(c.BaseURL, "/") + "/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login failed: HTTP %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("login response missing accessToken")
	}
	return &oauth2.Token{AccessToken: out.AccessToken, TokenType: "Bearer"}, nil
}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/client/... -v`
Expected: PASS for all four tests.

- [ ] **Step 6: Commit**

```bash
git add internal/client/client.go internal/client/auth.go internal/client/auth_test.go
git commit -m "feat(client): config validation and three auth token sources"
```

---

## Task 3: FHIR CRUD helpers + OperationOutcome errors

**Files:**
- Create: `internal/client/fhir.go`, `internal/client/fhir_test.go`

- [ ] **Step 1: Write the failing test in `internal/client/fhir_test.go`**

```go
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	c, err := New(context.Background(), Config{BaseURL: srv.URL, AccessToken: "tok"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

func TestFHIRCreate_ReturnsServerBody(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fhir/R4/ValueSet" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"resourceType":"ValueSet","id":"123","meta":{"versionId":"1"}}`))
	})
	defer srv.Close()

	out, err := c.FHIRCreate(context.Background(), "ValueSet", []byte(`{"resourceType":"ValueSet"}`))
	if err != nil {
		t.Fatalf("FHIRCreate: %v", err)
	}
	var got map[string]any
	_ = json.Unmarshal(out, &got)
	if got["id"] != "123" {
		t.Fatalf("expected id 123, got %v", got["id"])
	}
}

func TestFHIRRead_NotFound(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"not-found","diagnostics":"gone"}]}`))
	})
	defer srv.Close()

	_, err := c.FHIRRead(context.Background(), "ValueSet", "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Fatalf("expected IsNotFound, got %v", err)
	}
}

func TestFHIRDelete_OK(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	if err := c.FHIRDelete(context.Background(), "ValueSet", "123"); err != nil {
		t.Fatalf("FHIRDelete: %v", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/client/... -run TestFHIR -v`
Expected: FAIL — `FHIRCreate`, `FHIRRead`, `FHIRDelete`, `IsNotFound` undefined.

- [ ] **Step 3: Create `internal/client/fhir.go`**

```go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// APIError carries an HTTP status and parsed FHIR OperationOutcome diagnostics.
type APIError struct {
	StatusCode  int
	Diagnostics string
	Body        string
}

func (e *APIError) Error() string {
	if e.Diagnostics != "" {
		return fmt.Sprintf("medplum API error (HTTP %d): %s", e.StatusCode, e.Diagnostics)
	}
	return fmt.Sprintf("medplum API error (HTTP %d): %s", e.StatusCode, e.Body)
}

// IsNotFound reports whether err is an APIError with HTTP 404.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound
}

func (c *Client) fhirURL(parts ...string) string {
	u := c.baseURL + c.fhirPath
	for _, p := range parts {
		u += "/" + p
	}
	return u
}

func (c *Client) do(ctx context.Context, method, url string, body []byte) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/fhir+json")
	}
	req.Header.Set("Accept", "application/fhir+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: resp.StatusCode, Diagnostics: parseOutcome(respBody), Body: string(respBody)}
	}
	return respBody, nil
}

func parseOutcome(b []byte) string {
	var oo struct {
		ResourceType string `json:"resourceType"`
		Issue        []struct {
			Diagnostics string `json:"diagnostics"`
			Code        string `json:"code"`
		} `json:"issue"`
	}
	if json.Unmarshal(b, &oo) == nil && oo.ResourceType == "OperationOutcome" && len(oo.Issue) > 0 {
		if oo.Issue[0].Diagnostics != "" {
			return oo.Issue[0].Diagnostics
		}
		return oo.Issue[0].Code
	}
	return ""
}

// FHIRCreate POSTs a resource and returns the server representation.
func (c *Client) FHIRCreate(ctx context.Context, resourceType string, body []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPost, c.fhirURL(resourceType), body)
}

// FHIRRead GETs a resource by id.
func (c *Client) FHIRRead(ctx context.Context, resourceType, id string) ([]byte, error) {
	return c.do(ctx, http.MethodGet, c.fhirURL(resourceType, id), nil)
}

// FHIRUpdate PUTs a resource by id and returns the server representation.
func (c *Client) FHIRUpdate(ctx context.Context, resourceType, id string, body []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPut, c.fhirURL(resourceType, id), body)
}

// FHIRDelete DELETEs a resource by id.
func (c *Client) FHIRDelete(ctx context.Context, resourceType, id string) error {
	_, err := c.do(ctx, http.MethodDelete, c.fhirURL(resourceType, id), nil)
	return err
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/client/... -v`
Expected: PASS for all client tests.

- [ ] **Step 5: Commit**

```bash
git add internal/client/fhir.go internal/client/fhir_test.go
git commit -m "feat(client): FHIR CRUD helpers with OperationOutcome error mapping"
```

---

## Task 4: Embed the R4 schema and build the validator

**Files:**
- Create: `internal/fhirschema/data/fhir.schema.json`, `internal/fhirschema/validate.go`, `internal/fhirschema/validate_test.go`

- [ ] **Step 1: Vendor the R4 schema file**

Copy the official HL7 R4 JSON schema (draft-06, top-level `discriminator`, per-type `#/definitions/<Type>`) into the package:
```bash
mkdir -p internal/fhirschema/data
cp /home/ivan/Developer/fhir-static-data/fhir.schema.json internal/fhirschema/data/fhir.schema.json
```
(If that source is unavailable, download from `https://hl7.org/fhir/R4/fhir.schema.json`.)
Expected: file exists and is ~2 MB.

- [ ] **Step 2: Write the failing test in `internal/fhirschema/validate_test.go`**

```go
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
```

- [ ] **Step 3: Run to verify failure**

Run: `go test ./internal/fhirschema/... -v`
Expected: FAIL — `New`, `Validate` undefined.

- [ ] **Step 4: Create `internal/fhirschema/validate.go`**

```go
package fhirschema

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed data/fhir.schema.json
var schemaBytes []byte

// Validator validates FHIR R4 resources against the embedded JSON schema.
type Validator struct {
	mu       sync.Mutex
	compiler *jsonschema.Compiler
	cache    map[string]*jsonschema.Schema
	defs     map[string]json.RawMessage // resourceType -> presence check
}

const schemaURL = "fhir.schema.json"

// New compiles the embedded schema and returns a reusable Validator.
func New() (*Validator, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaBytes))
	if err != nil {
		return nil, fmt.Errorf("parse embedded schema: %w", err)
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft6)
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("add schema resource: %w", err)
	}

	// Capture the set of known resource definitions for fast unknown-type errors.
	var raw struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal(schemaBytes, &raw); err != nil {
		return nil, fmt.Errorf("read definitions: %w", err)
	}
	return &Validator{compiler: c, cache: map[string]*jsonschema.Schema{}, defs: raw.Definitions}, nil
}

func (v *Validator) schemaFor(resourceType string) (*jsonschema.Schema, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if s, ok := v.cache[resourceType]; ok {
		return s, nil
	}
	if _, ok := v.defs[resourceType]; !ok {
		return nil, fmt.Errorf("unknown FHIR resource type %q", resourceType)
	}
	s, err := v.compiler.Compile(schemaURL + "#/definitions/" + resourceType)
	if err != nil {
		return nil, fmt.Errorf("compile schema for %s: %w", resourceType, err)
	}
	v.cache[resourceType] = s
	return s, nil
}

// Validate checks that bodyJSON conforms to the schema for resourceType.
func (v *Validator) Validate(resourceType string, bodyJSON []byte) error {
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(bodyJSON))
	if err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	s, err := v.schemaFor(resourceType)
	if err != nil {
		return err
	}
	if err := s.Validate(inst); err != nil {
		return fmt.Errorf("FHIR schema validation failed: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests to verify pass**

Run: `go test ./internal/fhirschema/... -v`
Expected: PASS. (If `TestValidate_WrongType_BadEnum` fails because the R4 schema models `status` as a bare string rather than an enum, change the assertion to a structurally-invalid case instead: `{"resourceType":"ValueSet","status":123}` — a number where a string is required — and keep the test name semantics.)

- [ ] **Step 6: Commit**

```bash
git add internal/fhirschema
git commit -m "feat(fhirschema): embed R4 schema and validate resources by definition"
```

---

## Task 5: JSON normalization for stable drift detection

**Files:**
- Create: `internal/fhirjson/normalize.go`, `internal/fhirjson/normalize_test.go`

- [ ] **Step 1: Write the failing test in `internal/fhirjson/normalize_test.go`**

```go
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
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/fhirjson/... -v`
Expected: FAIL — undefined `Canonicalize`, `StripServerFields`, `Equal`.

- [ ] **Step 3: Create `internal/fhirjson/normalize.go`**

```go
package fhirjson

import (
	"bytes"
	"encoding/json"
)

// Canonicalize re-encodes JSON with sorted keys (encoding/json sorts map keys),
// producing a byte-stable form for comparison.
func Canonicalize(in []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// StripServerFields removes server-managed fields (top-level id, meta.versionId,
// meta.lastUpdated) and drops meta entirely if it becomes empty, then canonicalizes.
func StripServerFields(in []byte) ([]byte, error) {
	var m map[string]any
	if err := json.Unmarshal(in, &m); err != nil {
		return nil, err
	}
	delete(m, "id")
	if meta, ok := m["meta"].(map[string]any); ok {
		delete(meta, "versionId")
		delete(meta, "lastUpdated")
		if len(meta) == 0 {
			delete(m, "meta")
		}
	}
	out, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return Canonicalize(out)
}

// Equal reports whether two FHIR documents are equal after stripping
// server-managed fields and canonicalizing.
func Equal(a, b []byte) (bool, error) {
	na, err := StripServerFields(a)
	if err != nil {
		return false, err
	}
	nb, err := StripServerFields(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(na, nb), nil
}
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/fhirjson/... -v`
Expected: PASS. (`encoding/json` marshals map keys in sorted order, so the `want` strings above hold.)

- [ ] **Step 5: Commit**

```bash
git add internal/fhirjson
git commit -m "feat(fhirjson): canonicalize and strip server-managed fields for diffing"
```

---

## Task 6: Wire the client + validator into provider Configure

**Files:**
- Modify: `internal/provider/provider.go`
- Create: `internal/provider/provider_test.go`

- [ ] **Step 1: Replace `internal/provider/provider.go` with the configured version**

```go
package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Fluent-Health/terraform-provider-medplum/internal/client"
	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirschema"
)

// providerData is passed to resources via Configure.
type providerData struct {
	Client    *client.Client
	Validator *fhirschema.Validator
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &medplumProvider{version: version} }
}

type medplumProvider struct {
	version string
}

type medplumProviderModel struct {
	BaseURL      types.String `tfsdk:"base_url"`
	FHIRPath     types.String `tfsdk:"fhir_path"`
	TokenURL     types.String `tfsdk:"token_url"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	AccessToken  types.String `tfsdk:"access_token"`
	Email        types.String `tfsdk:"email"`
	Password     types.String `tfsdk:"password"`
}

func (p *medplumProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "medplum"
	resp.Version = p.version
}

func (p *medplumProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage Medplum FHIR resources and project configuration.",
		Attributes: map[string]schema.Attribute{
			"base_url":      schema.StringAttribute{Optional: true, MarkdownDescription: "Medplum (or gateway) base URL. Env: MEDPLUM_BASE_URL."},
			"fhir_path":     schema.StringAttribute{Optional: true, MarkdownDescription: "FHIR base path. Default /fhir/R4. Env: MEDPLUM_FHIR_PATH."},
			"token_url":     schema.StringAttribute{Optional: true, MarkdownDescription: "OAuth token endpoint. Default base_url + /oauth2/token. Env: MEDPLUM_TOKEN_URL."},
			"client_id":     schema.StringAttribute{Optional: true, MarkdownDescription: "OAuth client id. Env: MEDPLUM_CLIENT_ID."},
			"client_secret": schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "OAuth client secret. Env: MEDPLUM_CLIENT_SECRET."},
			"access_token":  schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Pre-obtained bearer token. Env: MEDPLUM_ACCESS_TOKEN."},
			"email":         schema.StringAttribute{Optional: true, MarkdownDescription: "Super-admin email. Env: MEDPLUM_EMAIL."},
			"password":      schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Super-admin password. Env: MEDPLUM_PASSWORD."},
		},
	}
}

func firstNonEmpty(configured types.String, envKey string) string {
	if !configured.IsNull() && configured.ValueString() != "" {
		return configured.ValueString()
	}
	return os.Getenv(envKey)
}

func (p *medplumProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var m medplumProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}

	cfg := client.Config{
		BaseURL:      firstNonEmpty(m.BaseURL, "MEDPLUM_BASE_URL"),
		FHIRPath:     firstNonEmpty(m.FHIRPath, "MEDPLUM_FHIR_PATH"),
		TokenURL:     firstNonEmpty(m.TokenURL, "MEDPLUM_TOKEN_URL"),
		ClientID:     firstNonEmpty(m.ClientID, "MEDPLUM_CLIENT_ID"),
		ClientSecret: firstNonEmpty(m.ClientSecret, "MEDPLUM_CLIENT_SECRET"),
		AccessToken:  firstNonEmpty(m.AccessToken, "MEDPLUM_ACCESS_TOKEN"),
		Email:        firstNonEmpty(m.Email, "MEDPLUM_EMAIL"),
		Password:     firstNonEmpty(m.Password, "MEDPLUM_PASSWORD"),
	}

	c, err := client.New(ctx, cfg)
	if err != nil {
		resp.Diagnostics.AddError("Invalid Medplum provider configuration", err.Error())
		return
	}
	v, err := fhirschema.New()
	if err != nil {
		resp.Diagnostics.AddError("Failed to load FHIR schema", err.Error())
		return
	}

	data := &providerData{Client: c, Validator: v}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *medplumProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewFHIRResource,
	}
}

func (p *medplumProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
```

- [ ] **Step 2: Create `internal/provider/provider_test.go` with the acceptance harness + a config unit test**

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories is used by acceptance tests.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"medplum": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProvider_ImplementsInterface(t *testing.T) {
	var _ = New("test")()
}
```

- [ ] **Step 3: Verify build (resource referenced in Step 1 lands in Task 7; comment it out to compile first)**

To keep the repo compiling before Task 7, temporarily return `nil` from `Resources` OR implement Task 7 next without committing Step 1's `Resources` change separately. Recommended: proceed directly to Task 7, then build/commit together.

Run: `go build ./... 2>&1 | head` — Expected: error `undefined: NewFHIRResource` until Task 7 is implemented. This is expected; do not commit yet.

- [ ] **Step 4: (Deferred commit)** — commit at the end of Task 7 so the tree builds.

---

## Task 7: `medplum_fhir_resource` with plan-time validation, CRUD, drift, import

> **Drift model updated during implementation (commit `010e902`).** Code review found that
> Medplum stamps `meta.project`/`author`/`compartment` onto every resource, which the
> `StripServerFields`+`Equal` approach below would treat as perpetual drift. The committed
> implementation instead uses a **subset/containment** model: `fhirjson.Contains(config, server)`
> ignores server-only fields, `Read` keeps the user's `body` unless the server no longer satisfies
> it (genuine drift), and a `semanticJSONBody()` plan modifier suppresses cosmetic diffs. The
> CRUD/validation/import structure below is otherwise as implemented.

**Files:**
- Create: `internal/provider/fhir_resource.go`
- Modify: commit alongside Task 6.

- [ ] **Step 1: Create `internal/provider/fhir_resource.go`**

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
	"github.com/Fluent-Health/terraform-provider-medplum/internal/fhirjson"
)

func NewFHIRResource() resource.Resource { return &fhirResource{} }

type fhirResource struct {
	data *providerData
}

type fhirResourceModel struct {
	ResourceType types.String `tfsdk:"resource_type"`
	Body         types.String `tfsdk:"body"`
	ID           types.String `tfsdk:"id"`
	VersionID    types.String `tfsdk:"version_id"`
	LastUpdated  types.String `tfsdk:"last_updated"`
}

func (r *fhirResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_fhir_resource"
}

func (r *fhirResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A generic FHIR R4 resource, validated against the R4 JSON schema at plan time.",
		Attributes: map[string]schema.Attribute{
			"resource_type": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "FHIR resourceType, e.g. ValueSet. Must match body.resourceType.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"body": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The FHIR resource as JSON. Do not set 'id'; it is server-assigned.",
			},
			"id":           schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"version_id":   schema.StringAttribute{Computed: true},
			"last_updated": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *fhirResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(*providerData)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	r.data = data
}

// ValidateConfig performs config-only structural checks. It must NOT depend on
// provider data (r.data): the framework runs the validation RPC without invoking
// the resource's Configure, so r.data is nil here. Schema validation that needs
// the compiled validator lives in ModifyPlan (where Configure has run).
func (r *fhirResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m fhirResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Body.IsUnknown() || m.Body.IsNull() || m.ResourceType.IsUnknown() {
		return
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(m.Body.ValueString()), &doc); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "Invalid JSON", err.Error())
		return
	}
	if doc["resourceType"] != m.ResourceType.ValueString() {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "resourceType mismatch",
			fmt.Sprintf("body.resourceType (%v) must equal resource_type (%s)", doc["resourceType"], m.ResourceType.ValueString()))
	}
	if _, ok := doc["id"]; ok {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "id must not be set",
			"The 'id' field is assigned by the server; remove it from body.")
	}
}

// ModifyPlan runs full R4 schema validation at plan time. Configure has reliably
// run before this hook, so r.data (and the compiled Validator) is available.
func (r *fhirResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() { // resource is being destroyed; nothing to validate
		return
	}
	var m fhirResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() || m.Body.IsUnknown() || m.Body.IsNull() || m.ResourceType.IsUnknown() {
		return
	}
	if r.data == nil || r.data.Validator == nil {
		return
	}
	if err := r.data.Validator.Validate(m.ResourceType.ValueString(), []byte(m.Body.ValueString())); err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("body"), "FHIR schema validation failed", err.Error())
	}
}

func extractMeta(serverBody []byte) (id, versionID, lastUpdated string) {
	var doc struct {
		ID   string `json:"id"`
		Meta struct {
			VersionID   string `json:"versionId"`
			LastUpdated string `json:"lastUpdated"`
		} `json:"meta"`
	}
	_ = json.Unmarshal(serverBody, &doc)
	return doc.ID, doc.Meta.VersionID, doc.Meta.LastUpdated
}

func (r *fhirResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m fhirResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRCreate(ctx, m.ResourceType.ValueString(), []byte(m.Body.ValueString()))
	if err != nil {
		resp.Diagnostics.AddError("Create failed", err.Error())
		return
	}
	id, ver, upd := extractMeta(out)
	m.ID = types.StringValue(id)
	m.VersionID = types.StringValue(ver)
	m.LastUpdated = types.StringValue(upd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *fhirResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m fhirResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	out, err := r.data.Client.FHIRRead(ctx, m.ResourceType.ValueString(), m.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Read failed", err.Error())
		return
	}
	// Only overwrite body if it semantically differs from what we have in state,
	// so server-managed fields and key ordering do not cause perpetual diffs.
	eq, eqErr := fhirjson.Equal([]byte(m.Body.ValueString()), out)
	if eqErr == nil && !eq {
		m.Body = types.StringValue(string(out))
	}
	id, ver, upd := extractMeta(out)
	m.ID = types.StringValue(id)
	m.VersionID = types.StringValue(ver)
	m.LastUpdated = types.StringValue(upd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

func (r *fhirResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state fhirResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Re-inject the server id into the body for the PUT.
	var doc map[string]any
	if err := json.Unmarshal([]byte(plan.Body.ValueString()), &doc); err != nil {
		resp.Diagnostics.AddError("Invalid body JSON", err.Error())
		return
	}
	doc["id"] = state.ID.ValueString()
	putBody, _ := json.Marshal(doc)

	out, err := r.data.Client.FHIRUpdate(ctx, plan.ResourceType.ValueString(), state.ID.ValueString(), putBody)
	if err != nil {
		resp.Diagnostics.AddError("Update failed", err.Error())
		return
	}
	id, ver, upd := extractMeta(out)
	plan.ID = types.StringValue(id)
	plan.VersionID = types.StringValue(ver)
	plan.LastUpdated = types.StringValue(upd)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *fhirResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m fhirResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.Client.FHIRDelete(ctx, m.ResourceType.ValueString(), m.ID.ValueString()); err != nil {
		if client.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Delete failed", err.Error())
	}
}

// ImportState accepts "ResourceType/id" and populates resource_type + id.
func (r *fhirResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	rt, id, ok := splitRef(req.ID)
	if !ok {
		resp.Diagnostics.AddError("Invalid import ID", "expected format ResourceType/id, e.g. ValueSet/abc123")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("resource_type"), rt)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	// body is populated by the subsequent Read.
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("body"), "{}")...)
}

func splitRef(s string) (resourceType, id string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			return s[:i], s[i+1:], i > 0 && i < len(s)-1
		}
	}
	return "", "", false
}

// interface assertions
var (
	_ resource.Resource                   = (*fhirResource)(nil)
	_ resource.ResourceWithConfigure      = (*fhirResource)(nil)
	_ resource.ResourceWithImportState    = (*fhirResource)(nil)
	_ resource.ResourceWithValidateConfig = (*fhirResource)(nil)
	_ resource.ResourceWithModifyPlan     = (*fhirResource)(nil)
)
```

- [ ] **Step 2: Build the whole tree**

Run: `go mod tidy && go build ./... && go vet ./...`
Expected: compiles cleanly (this resolves the `NewFHIRResource` reference from Task 6).

- [ ] **Step 3: Run unit tests**

Run: `go test ./... -count=1`
Expected: PASS (no acceptance tests run without `TF_ACC`).

- [ ] **Step 4: Commit Tasks 6 + 7 together**

```bash
git add internal/provider
git commit -m "feat(provider): configure client+validator and add medplum_fhir_resource"
```

---

## Task 8: Acceptance test for `medplum_fhir_resource`

**Files:**
- Create: `internal/provider/fhir_resource_test.go`

- [ ] **Step 1: Write the acceptance test**

```go
package provider

import (
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func testAccPreCheck(t *testing.T) {
	if os.Getenv("MEDPLUM_BASE_URL") == "" {
		t.Fatal("MEDPLUM_BASE_URL must be set for acceptance tests")
	}
	hasCreds := os.Getenv("MEDPLUM_CLIENT_ID") != "" || os.Getenv("MEDPLUM_ACCESS_TOKEN") != "" || os.Getenv("MEDPLUM_EMAIL") != ""
	if !hasCreds {
		t.Fatal("a Medplum auth method env var must be set for acceptance tests")
	}
}

func TestAccFHIRResource_basic(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccFHIRResourceConfig("active"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "id"),
					resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "version_id"),
					resource.TestCheckResourceAttr("medplum_fhir_resource.test", "resource_type", "ValueSet"),
				),
			},
			{
				// No-op re-apply must produce an empty plan (drift stability).
				Config:   testAccFHIRResourceConfig("active"),
				PlanOnly: true,
			},
			{
				Config: testAccFHIRResourceConfig("draft"),
				Check: resource.TestCheckResourceAttrSet("medplum_fhir_resource.test", "version_id"),
			},
			{
				ResourceName:      "medplum_fhir_resource.test",
				ImportState:       true,
				ImportStateIdFunc: importIDFunc("medplum_fhir_resource.test"),
				ImportStateVerify: true,
				// body is re-read from server; ignore exact string match on import verify.
				ImportStateVerifyIgnore: []string{"body"},
			},
		},
	})
}

func testAccFHIRResourceConfig(status string) string {
	return fmt.Sprintf(`
resource "medplum_fhir_resource" "test" {
  resource_type = "ValueSet"
  body = jsonencode({
    resourceType = "ValueSet"
    status       = %q
    url          = "http://example.com/fhir/ValueSet/tf-acc-test"
  })
}
`, status)
}

func importIDFunc(name string) resource.ImportStateIdFunc {
	return func(s *resource.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("resource %s not found", name)
		}
		return "ValueSet/" + rs.Primary.Attributes["id"], nil
	}
}
```

- [ ] **Step 2: Verify it compiles and is skipped without `TF_ACC`**

Run: `go test ./internal/provider/... -run TestAccFHIRResource -v`
Expected: test is skipped (no `TF_ACC`), package compiles.

- [ ] **Step 3: Commit**

```bash
git add internal/provider/fhir_resource_test.go
git commit -m "test(provider): acceptance test for medplum_fhir_resource"
```

---

## Task 9: Docker Compose Medplum for local + CI acceptance

**Files:**
- Create: `docker-compose.test.yml`, `scripts/wait-for-medplum.sh`

- [ ] **Step 1: Create `docker-compose.test.yml`**

Pin the Medplum server image to a known version (see spec — confirm against the version under test).

```yaml
services:
  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: medplum
      POSTGRES_PASSWORD: medplum
      POSTGRES_DB: medplum
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U medplum"]
      interval: 5s
      timeout: 5s
      retries: 20
  redis:
    image: redis:7
    command: ["redis-server", "--requirepass", "medplum"]
    healthcheck:
      test: ["CMD", "redis-cli", "-a", "medplum", "ping"]
      interval: 5s
      timeout: 5s
      retries: 20
  medplum:
    image: medplum/medplum-server:5.1.14
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    ports:
      - "8103:8103"
    environment:
      MEDPLUM_DATABASE_HOST: postgres
      MEDPLUM_DATABASE_PORT: "5432"
      MEDPLUM_DATABASE_DBNAME: medplum
      MEDPLUM_DATABASE_USERNAME: medplum
      MEDPLUM_DATABASE_PASSWORD: medplum
      MEDPLUM_REDIS_HOST: redis
      MEDPLUM_REDIS_PORT: "6379"
      MEDPLUM_REDIS_PASSWORD: medplum
      MEDPLUM_BASE_URL: "http://localhost:8103/"
      MEDPLUM_PORT: "8103"
```

- [ ] **Step 2: Create `scripts/wait-for-medplum.sh`**

```bash
#!/usr/bin/env bash
# Waits for the Medplum server healthcheck endpoint to respond.
set -euo pipefail

URL="${1:-http://localhost:8103/healthcheck}"
for i in $(seq 1 60); do
  if curl -fsS "$URL" >/dev/null 2>&1; then
    echo "Medplum is up"
    exit 0
  fi
  echo "waiting for Medplum ($i)..."
  sleep 5
done
echo "Medplum did not become healthy in time" >&2
exit 1
```
Then: `chmod +x scripts/wait-for-medplum.sh`

- [ ] **Step 3: Document the bootstrap requirement**

The fresh Medplum server auto-creates a default project and super-admin on first boot via
`MEDPLUM_ADMIN_*` / project init. Add a `scripts/bootstrap-medplum.md` note describing how the CI
job obtains credentials (the server logs the default admin, or use the documented default
`admin@example.com` / `medplum_admin` for the dev image). **Acceptance-test task:** confirm the
exact default-admin mechanism for the pinned image and capture it here.

- [ ] **Step 4: Commit**

```bash
git add docker-compose.test.yml scripts/wait-for-medplum.sh scripts/bootstrap-medplum.md
git commit -m "test: docker-compose Medplum stack for acceptance tests"
```

---

## Task 10: GitHub Actions CI (lint + unit + acceptance)

**Files:**
- Create: `.github/workflows/ci.yml`, `.golangci.yml`

- [ ] **Step 1: Create `.golangci.yml`**

```yaml
run:
  timeout: 5m
linters:
  enable:
    - gofmt
    - govet
    - errcheck
    - staticcheck
    - ineffassign
    - unused
```

- [ ] **Step 2: Create `.github/workflows/ci.yml`**

```yaml
name: CI
on:
  pull_request:
  push:
    branches: [main]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: v1.61

  unit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - run: go test ./... -count=1

  acceptance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.23"
      - name: Start Medplum
        run: docker compose -f docker-compose.test.yml up -d
      - name: Wait for Medplum
        run: ./scripts/wait-for-medplum.sh
      - name: Acceptance tests
        env:
          TF_ACC: "1"
          MEDPLUM_BASE_URL: "http://localhost:8103"
          MEDPLUM_EMAIL: "admin@example.com"
          MEDPLUM_PASSWORD: "medplum_admin"
        run: go test ./internal/provider/... -count=1 -v -timeout 30m
      - name: Medplum logs on failure
        if: failure()
        run: docker compose -f docker-compose.test.yml logs medplum
```

- [ ] **Step 3: Validate the workflow YAML locally**

Run: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/ci.yml'))" && echo OK`
Expected: `OK`.

- [ ] **Step 4: Commit and push to open a PR**

```bash
git add .github/workflows/ci.yml .golangci.yml
git commit -m "ci: lint, unit, and live-Medplum acceptance workflow"
```
Then push the branch and open a PR; confirm all three jobs run. The `acceptance` job is the one to watch — if the default-admin credentials differ for the pinned image, fix them per Task 9 Step 3 and re-run.

---

## Self-Review (completed during plan authoring)

**Spec coverage (Plan 1 scope):**
- Provider scaffold + multi-method gateway-agnostic auth → Tasks 1, 2, 6. ✓
- Generic `medplum_fhir_resource` with plan-time R4 validation + drift handling → Tasks 4, 5, 7. ✓
- Acceptance tests against live Medplum in GitHub Actions → Tasks 8, 9, 10. ✓
- Env-var config fallback → Task 6 (`firstNonEmpty`). ✓
- Import support → Task 7 (`ImportState`). ✓
- Deferred to Plan 2/3 (typed resources, docs, GoReleaser/registry) → explicitly out of scope. ✓

**Type consistency:** `client.Config`, `client.New`, `client.Client.FHIR{Create,Read,Update,Delete}`, `client.IsNotFound`, `fhirschema.New`/`Validator.Validate`, `fhirjson.{Canonicalize,StripServerFields,Equal}`, `providerData{Client,Validator}`, `NewFHIRResource` are defined once and referenced consistently.

**Open items intentionally surfaced for execution (not placeholders):**
- Task 4 Step 5: fallback assertion if R4 schema models `status` as a bare string.
- Task 9 Step 3 / Task 10: confirm the pinned Medplum image's default-admin credentials; this is the most likely point of first-run friction.
