package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// tokenSource reduces all three auth methods to an oauth2.TokenSource.
func (c Config) tokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	switch {
	case c.hasToken():
		return oauth2.StaticTokenSource(&oauth2.Token{AccessToken: c.AccessToken, TokenType: "Bearer"}), nil
	case c.hasClientCreds():
		cc := &clientcredentials.Config{
			ClientID:     c.ClientID,
			ClientSecret: c.ClientSecret,
			TokenURL:     c.tokenURL(),
			AuthStyle:    oauth2.AuthStyleInParams,
		}
		return cc.TokenSource(ctx), nil
	case c.hasLogin():
		tok, err := c.login(ctx)
		if err != nil {
			return nil, err
		}
		return oauth2.StaticTokenSource(tok), nil
	default:
		return nil, fmt.Errorf("no auth method configured")
	}
}

// login performs Medplum email+password login and returns the access token.
func (c Config) login(ctx context.Context) (*oauth2.Token, error) {
	body, err := json.Marshal(map[string]string{"email": c.Email, "password": c.Password})
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(c.BaseURL, "/") + "/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("login failed: HTTP %d: %s", resp.StatusCode, snippet)
	}
	var out struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("login response missing accessToken")
	}
	return &oauth2.Token{AccessToken: out.AccessToken, TokenType: "Bearer"}, nil
}
