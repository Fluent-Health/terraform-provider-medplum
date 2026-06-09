package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newFastRetryClient builds a client whose retry transport uses millisecond
// delays so retry-path tests stay fast.
func newFastRetryClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	c, err := New(context.Background(), Config{
		BaseURL:     srv.URL,
		AccessToken: "tok",
		HTTPClient: &http.Client{Transport: &retryTransport{
			base: http.DefaultTransport, maxRetries: 3, baseDelay: time.Millisecond, maxDelay: 5 * time.Millisecond,
		}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, srv
}

const errOutcome = `{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"not-found","details":{"text":"Not found"}}]}`

// A read that first gets HTTP 200 + error OperationOutcome (gateway flake) then
// the real resource must transparently retry and succeed.
func TestFHIRRead_RetriesTransientOperationOutcome(t *testing.T) {
	var calls int32
	c, srv := newFastRetryClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		if atomic.AddInt32(&calls, 1) == 1 {
			_, _ = w.Write([]byte(errOutcome)) // HTTP 200 + error OperationOutcome
			return
		}
		_, _ = w.Write([]byte(`{"resourceType":"ValueSet","id":"123"}`))
	})
	defer srv.Close()

	out, err := c.FHIRRead(context.Background(), "ValueSet", "123")
	if err != nil {
		t.Fatalf("expected transparent retry to succeed, got: %v", err)
	}
	if !strings.Contains(string(out), `"ValueSet"`) {
		t.Fatalf("expected the real ValueSet after retry, got: %s", out)
	}
	if atomic.LoadInt32(&calls) < 2 {
		t.Fatalf("expected at least one retry, got %d calls", calls)
	}
}

// A persistent HTTP 200 + error OperationOutcome must surface as an error rather
// than being stored as a resource body.
func TestFHIRRead_PersistentOperationOutcomeErrors(t *testing.T) {
	c, srv := newFastRetryClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(errOutcome))
	})
	defer srv.Close()

	if _, err := c.FHIRRead(context.Background(), "ValueSet", "404"); err == nil {
		t.Fatal("expected an error for a persistent 200 + error OperationOutcome")
	}
}

// A success/information OperationOutcome (e.g. a delete response) must NOT be
// treated as an error.
func TestDelete_InformationOutcomeOK(t *testing.T) {
	c, srv := newFastRetryClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/fhir+json")
		_, _ = w.Write([]byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"information","code":"informational"}]}`))
	})
	defer srv.Close()

	if err := c.FHIRDelete(context.Background(), "ValueSet", "123"); err != nil {
		t.Fatalf("information OperationOutcome should not be an error: %v", err)
	}
}
