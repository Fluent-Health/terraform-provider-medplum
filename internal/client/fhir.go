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
	return respBody, nil
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
