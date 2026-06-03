package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
