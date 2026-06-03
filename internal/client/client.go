package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// Config holds connection + auth settings. Exactly one auth method must be set:
// (client_id+client_secret) OR access_token OR (email+password).
type Config struct {
	BaseURL      string
	FHIRPath     string // default "/fhir/R4"
	TokenURL     string // default BaseURL + "/oauth2/token"
	ClientID     string
	ClientSecret string
	AccessToken  string
	Email        string
	Password     string

	HTTPClient *http.Client // optional; defaults to http.DefaultClient
}

func (c Config) hasClientCreds() bool { return c.ClientID != "" && c.ClientSecret != "" }
func (c Config) hasToken() bool       { return c.AccessToken != "" }
func (c Config) hasLogin() bool       { return c.Email != "" && c.Password != "" }

// Validate ensures exactly one auth method is configured.
func (c Config) Validate() error {
	if c.BaseURL == "" {
		return fmt.Errorf("base_url is required")
	}
	n := 0
	if c.hasClientCreds() {
		n++
	}
	if c.hasToken() {
		n++
	}
	if c.hasLogin() {
		n++
	}
	if n == 0 {
		return fmt.Errorf("one auth method is required: client_id+client_secret, access_token, or email+password")
	}
	if n > 1 {
		return fmt.Errorf("exactly one auth method may be set, got %d", n)
	}
	return nil
}

func (c Config) fhirPath() string {
	if c.FHIRPath == "" {
		return "/fhir/R4"
	}
	return "/" + strings.Trim(c.FHIRPath, "/")
}

func (c Config) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return strings.TrimRight(c.BaseURL, "/") + "/oauth2/token"
}

// Client is an authenticated Medplum HTTP client.
type Client struct {
	baseURL  string
	fhirPath string
	http     *http.Client
}

// New validates the config and returns a Client whose underlying transport
// injects a bearer token from the configured auth method.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ts, err := cfg.tokenSource(ctx)
	if err != nil {
		return nil, err
	}
	base := cfg.HTTPClient
	if base == nil {
		base = http.DefaultClient
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	return &Client{
		baseURL:  strings.TrimRight(cfg.BaseURL, "/"),
		fhirPath: cfg.fhirPath(),
		http:     oauth2.NewClient(ctx, ts),
	}, nil
}
