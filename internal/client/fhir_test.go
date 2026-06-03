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
