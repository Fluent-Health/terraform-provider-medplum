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
	if e.Diagnostics != "" {
		return fmt.Sprintf("medplum API error (HTTP %d): %s", e.StatusCode, e.Diagnostics)
	}
	return fmt.Sprintf("medplum API error (HTTP %d): %s", e.StatusCode, e.Body)
}

// IsNotFound reports whether err is an APIError with HTTP 404.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound
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
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)

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
	return c.do(ctx, http.MethodGet, c.fhirURL(resourceType, id), nil)
}

// FHIRUpdate PUTs a resource by id and returns the server representation.
func (c *Client) FHIRUpdate(ctx context.Context, resourceType, id string, body []byte) ([]byte, error) {
	return c.do(ctx, http.MethodPut, c.fhirURL(resourceType, id), body)
}

// FHIRDelete DELETEs a resource by id.
func (c *Client) FHIRDelete(ctx context.Context, resourceType, id string) error {
	_, err := c.do(ctx, http.MethodDelete, c.fhirURL(resourceType, id), nil)
	return err
}
