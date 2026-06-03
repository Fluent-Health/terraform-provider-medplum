package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// Operation invokes a FHIR operation (e.g. "$init", "$rotate-secret"). If id is
// empty, the operation is type-level (/{type}/$op); otherwise instance-level
// (/{type}/{id}/$op). body is the JSON Parameters payload (may be nil).
func (c *Client) Operation(ctx context.Context, resourceType, id, op string, body []byte) ([]byte, error) {
	var url string
	if id == "" {
		url = c.fhirURL(resourceType, op)
	} else {
		url = c.fhirURL(resourceType, id, op)
	}
	return c.do(ctx, http.MethodPost, url, body)
}

// SetPassword sets a user's password via the project-admin endpoint (no email sent).
func (c *Client) SetPassword(ctx context.Context, projectID, email, password string) error {
	body, err := json.Marshal(map[string]string{"email": email, "password": password})
	if err != nil {
		return err
	}
	url := c.baseURL + fmt.Sprintf("/admin/projects/%s/setpassword", projectID)
	_, err = c.do(ctx, http.MethodPost, url, body)
	return err
}
