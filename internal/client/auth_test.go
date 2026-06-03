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
