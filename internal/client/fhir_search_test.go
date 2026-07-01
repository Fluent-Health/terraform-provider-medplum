package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFHIRSearch_sendsRawQuery(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.RequestURI()
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"searchset"}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, fhirPath: "/fhir/R4", httpClient: srv.Client()}
	out, err := c.FHIRSearch(context.Background(), "QuestionnaireResponse", "questionnaire=X&_count=50")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "/fhir/R4/QuestionnaireResponse?questionnaire=X&_count=50") {
		t.Fatalf("unexpected request URI: %s", gotURL)
	}
	if !strings.Contains(string(out), "searchset") {
		t.Fatalf("unexpected body: %s", out)
	}
}

func TestFHIRBundle_postsToBase(t *testing.T) {
	var gotPath, gotMethod, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = w.Write([]byte(`{"resourceType":"Bundle","type":"batch-response"}`))
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, fhirPath: "/fhir/R4", httpClient: srv.Client()}
	_, err := c.FHIRBundle(context.Background(), []byte(`{"resourceType":"Bundle","type":"batch"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/fhir/R4" {
		t.Fatalf("expected POST /fhir/R4, got %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"type":"batch"`) {
		t.Fatalf("body not forwarded: %s", gotBody)
	}
}
