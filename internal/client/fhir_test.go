package client

import (
	"context"
	"encoding/json"
	"io"
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

func TestFHIRUpdate_RoundTrip(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "bad method", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/fhir/R4/ValueSet/123" {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		_, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/fhir+json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"resourceType":"ValueSet","id":"123","meta":{"versionId":"2"}}`))
	})
	defer srv.Close()

	out, err := c.FHIRUpdate(context.Background(), "ValueSet", "123", []byte(`{"resourceType":"ValueSet","id":"123"}`))
	if err != nil {
		t.Fatalf("FHIRUpdate: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["id"] != "123" {
		t.Fatalf("expected id 123, got %v", got["id"])
	}
}

func TestFHIRRead_EmptyID(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	defer srv.Close()

	_, err := c.FHIRRead(context.Background(), "ValueSet", "")
	if err == nil {
		t.Fatal("expected error for empty id, got nil")
	}
}

func TestIsNotFound_TreatsGoneAsNotFound(t *testing.T) {
	if !IsNotFound(&APIError{StatusCode: 410, Diagnostics: "deleted"}) {
		t.Fatal("expected HTTP 410 (Gone) to be treated as not-found")
	}
	if !IsNotFound(&APIError{StatusCode: 404}) {
		t.Fatal("expected HTTP 404 to be not-found")
	}
	if IsNotFound(&APIError{StatusCode: 400}) {
		t.Fatal("400 must not be not-found")
	}
}
