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
	detail := e.Diagnostics
	if detail == "" {
		detail = e.Body
	}
	if detail == "" {
		detail = http.StatusText(e.StatusCode)
	}
	return fmt.Sprintf("medplum API error (HTTP %d): %s", e.StatusCode, detail)
}

// IsNotFound reports whether err is an APIError indicating the resource is not
// present: HTTP 404 (Not Found) or 410 (Gone). FHIR/Medplum returns 410 for a
// resource that has been deleted, so for delete-tolerance and read-removal both
// statuses mean "no longer there".
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && (ae.StatusCode == http.StatusNotFound || ae.StatusCode == http.StatusGone)
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
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: resp.StatusCode, Diagnostics: parseOutcome(respBody), Body: string(respBody)}
	}
	// Some gateways (e.g. Gravitee in front of Medplum) intermittently answer a
	// read with HTTP 200 but an error OperationOutcome body ("Not found") instead
	// of the resource. Never let that masquerade as a real resource: surface it
	// as an error so it is retried (see retryTransport) rather than stored.
	if isErrorOutcome(respBody) {
		return nil, &APIError{StatusCode: resp.StatusCode, Diagnostics: parseOutcome(respBody), Body: string(respBody)}
	}
	return respBody, nil
}

// isErrorOutcome reports whether b is an OperationOutcome carrying an error- or
// fatal-severity issue. A success/information OperationOutcome (e.g. the body of
// a delete) is not treated as an error.
func isErrorOutcome(b []byte) bool {
	var oo struct {
		ResourceType string `json:"resourceType"`
		Issue        []struct {
			Severity string `json:"severity"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(b, &oo); err != nil || oo.ResourceType != "OperationOutcome" {
		return false
	}
	for _, i := range oo.Issue {
		if i.Severity == "error" || i.Severity == "fatal" {
			return true
		}
	}
	return false
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
	if id == "" {
		return nil, fmt.Errorf("FHIRRead: id is required")
	}
	return c.do(ctx, http.MethodGet, c.fhirURL(resourceType, id), nil)
}

// FHIRUpdate PUTs a resource by id and returns the server representation.
func (c *Client) FHIRUpdate(ctx context.Context, resourceType, id string, body []byte) ([]byte, error) {
	if id == "" {
		return nil, fmt.Errorf("FHIRUpdate: id is required")
	}
	return c.do(ctx, http.MethodPut, c.fhirURL(resourceType, id), body)
}

// FHIRDelete DELETEs a resource by id.
func (c *Client) FHIRDelete(ctx context.Context, resourceType, id string) error {
	if id == "" {
		return fmt.Errorf("FHIRDelete: id is required")
	}
	_, err := c.do(ctx, http.MethodDelete, c.fhirURL(resourceType, id), nil)
	return err
}

// FHIRSearch GETs a search against a resource type with a raw query string
// (e.g. "questionnaire=X&_tag:not=...&_count=50") and returns the searchset Bundle.
func (c *Client) FHIRSearch(ctx context.Context, resourceType, rawQuery string) ([]byte, error) {
	u := c.fhirURL(resourceType)
	if rawQuery != "" {
		u += "?" + rawQuery
	}
	return c.do(ctx, http.MethodGet, u, nil)
}

// FHIRBundle POSTs a batch/transaction Bundle to the base FHIR endpoint and
// returns the response Bundle.
func (c *Client) FHIRBundle(ctx context.Context, bundle []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPost, c.baseURL+c.fhirPath, bundle)
}

// FHIRReadBinaryContent GETs the raw stored content of a Binary resource.
// Requesting a non-FHIR Accept type makes Medplum stream the binary content
// itself instead of the FHIR JSON envelope. The payload is arbitrary bytes
// (e.g. deployed bot code), so no OperationOutcome sniffing is applied to
// success responses.
func (c *Client) FHIRReadBinaryContent(ctx context.Context, id string) ([]byte, error) {
	if id == "" {
		return nil, fmt.Errorf("FHIRReadBinaryContent: id is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.fhirURL("Binary", id), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, &APIError{StatusCode: resp.StatusCode, Diagnostics: parseOutcome(body), Body: string(body)}
	}
	return body, nil
}
