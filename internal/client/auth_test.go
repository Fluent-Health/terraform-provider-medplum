package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLogin_CodeExchange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "L1", "code": "C1"})
		case "/oauth2/token":
			_ = r.ParseForm()
			if got := r.Form.Get("grant_type"); got != "authorization_code" {
				http.Error(w, fmt.Sprintf("bad grant_type: %q", got), http.StatusBadRequest)
				return
			}
			if got := r.Form.Get("code"); got != "C1" {
				http.Error(w, fmt.Sprintf("bad code: %q", got), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "final-tok"})
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
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "final-tok" {
		t.Fatalf("got token %q, want %q", tok.AccessToken, "final-tok")
	}
}

func TestLogin_ProfileSelection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/login":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"login":       "L1",
				"memberships": []map[string]any{{"id": "M1"}},
			})
		case "/auth/profile":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["login"] != "L1" {
				http.Error(w, fmt.Sprintf("bad login: %q", body["login"]), http.StatusBadRequest)
				return
			}
			if body["profile"] != "M1" {
				http.Error(w, fmt.Sprintf("bad profile: %q", body["profile"]), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "L1", "code": "C2"})
		case "/oauth2/token":
			_ = r.ParseForm()
			if got := r.Form.Get("code"); got != "C2" {
				http.Error(w, fmt.Sprintf("bad code: %q", got), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "tok2"})
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
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok.AccessToken != "tok2" {
		t.Fatalf("got token %q, want %q", tok.AccessToken, "tok2")
	}
}

func TestLogin_MFA_Unsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"login": "L1", "mfaRequired": true})
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	cfg := Config{BaseURL: srv.URL, Email: "a@b.com", Password: "pw"}
	_, err := cfg.tokenSource(context.Background())
	if err == nil {
		t.Fatal("expected error for MFA account, got nil")
	}
	if !strings.Contains(err.Error(), "MFA") {
		t.Fatalf("error does not mention MFA: %v", err)
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

func TestNew_ClientCredentials_UsesConfiguredHTTPClient(t *testing.T) {
	tokenHit := false
	fhirHit := false

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			tokenHit = true
			_ = r.ParseForm()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "abc",
				"token_type":   "Bearer",
				"expires_in":   3600,
			})
		case "/fhir/R4/metadata":
			fhirHit = true
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"resourceType":"CapabilityStatement"}`)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// srv.Client() already trusts the TLS certificate; pass it via Config.HTTPClient
	// so we can prove the custom client is used for token refresh.
	cfg := Config{
		BaseURL:      srv.URL,
		TokenURL:     srv.URL + "/oauth2/token",
		ClientID:     "id",
		ClientSecret: "secret",
		HTTPClient:   srv.Client(),
	}

	c, err := New(context.Background(), cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Make a request through the client — this triggers token acquisition.
	resp, err := c.httpClient.Get(srv.URL + "/fhir/R4/metadata")
	if err != nil {
		t.Fatalf("GET metadata: %v", err)
	}
	resp.Body.Close()

	if !tokenHit {
		t.Error("token endpoint was never reached: custom HTTPClient was not wired into the token source")
	}
	if !fhirHit {
		t.Error("FHIR metadata endpoint was never reached")
	}
}
