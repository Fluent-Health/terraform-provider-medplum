package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestNew_ClientCredentials_SurvivesConfigContextCancel reproduces the bug where
// the client-credentials token source was bound to the Configure-time context.
// Terraform cancels that context once Configure returns, so lazy token fetches
// during CRUD failed with "context canceled". New must detach the token source
// from the caller's context.
func TestNew_ClientCredentials_SurvivesConfigContextCancel(t *testing.T) {
	var tokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth2/token":
			tokenCalls++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok", "token_type": "Bearer", "expires_in": 3600,
			})
		case r.URL.Path == "/fhir/R4/ValueSet/123":
			w.Header().Set("Content-Type", "application/fhir+json")
			_, _ = w.Write([]byte(`{"resourceType":"ValueSet","id":"123"}`))
		default:
			http.Error(w, "unexpected "+r.URL.Path, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	// New is called with a Configure-time context that we cancel immediately
	// afterwards — exactly what the Terraform plugin framework does.
	ctx, cancel := context.WithCancel(context.Background())
	c, err := New(ctx, Config{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cancel()

	// The token is fetched lazily on this first call. If the token source were
	// bound to the canceled ctx above, this would fail with "context canceled".
	if _, err := c.FHIRRead(context.Background(), "ValueSet", "123"); err != nil {
		t.Fatalf("FHIRRead after Configure-context cancel: %v", err)
	}
	if tokenCalls == 0 {
		t.Fatal("expected the token endpoint to be called")
	}
}
