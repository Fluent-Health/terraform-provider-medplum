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

// CurrentProjectID returns the project id of the authenticated session via GET /auth/me.
func (c *Client) CurrentProjectID(ctx context.Context) (string, error) {
	out, err := c.do(ctx, http.MethodGet, c.baseURL+"/auth/me", nil)
	if err != nil {
		return "", err
	}
	var me struct {
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
	}
	if err := json.Unmarshal(out, &me); err != nil {
		return "", err
	}
	if me.Project.ID == "" {
		return "", fmt.Errorf("/auth/me returned no project id")
	}
	return me.Project.ID, nil
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

// AdminCreateBot creates a Bot plus its ProjectMembership via the project-admin
// endpoint (POST /admin/projects/{id}/bot). body is the plain-JSON
// BotInitParameters payload (name, description, runtimeVersion,
// accessPolicy: {reference}), NOT FHIR Parameters; the response is the created
// Bot resource. NOTE: the server creates the bot in the AUTHENTICATED SESSION's
// project — the projectID URL segment is only permission-checked — so pass the
// session project id (see CurrentProjectID).
func (c *Client) AdminCreateBot(ctx context.Context, projectID string, body []byte) ([]byte, error) {
	if projectID == "" {
		return nil, fmt.Errorf("AdminCreateBot: projectID is required")
	}
	url := c.baseURL + fmt.Sprintf("/admin/projects/%s/bot", projectID)
	return c.do(ctx, http.MethodPost, url, body)
}
