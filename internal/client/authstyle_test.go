package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNew_ClientCredentials_UsesBasicAuth ensures the client-credentials token
// request can authenticate against a token endpoint that requires
// client_secret_basic (Authorization: Basic ...) and rejects credentials passed
// as body params — the behavior of the Gravitee AM endpoint that fronts Medplum.
// With a hardcoded AuthStyleInParams this returned "invalid_client".
func TestNew_ClientCredentials_UsesBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth2/token":
			user, pass, ok := r.BasicAuth()
			if !ok || user != "id" || pass != "secret" {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":             "invalid_client",
					"error_description": "missing or unsupported authentication method",
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "token_type": "Bearer", "expires_in": 3600,
			})
		case "/fhir/R4/ValueSet/123":
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(`{"resourceType":"ValueSet","id":"123"}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := New(context.Background(), Config{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.FHIRRead(context.Background(), "ValueSet", "123"); err != nil {
		t.Fatalf("FHIRRead against a Basic-auth token endpoint: %v", err)
	}
}
