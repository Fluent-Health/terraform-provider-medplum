package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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
		return oauth2.ReuseTokenSource(nil, &loginTokenSource{cfg: c}), nil
	default:
		return nil, fmt.Errorf("no auth method configured")
	}
}

// loginTokenSource performs Medplum email+password login (with PKCE + token
// exchange) on demand. Wrapped in oauth2.ReuseTokenSource, it re-logs in when
// the previous token expires.
//
// Token() intentionally uses context.Background rather than the context that
// was active at Configure time: the Configure-time context is cancelled by
// Terraform before any CRUD operations run, so passing it to a lazy token
// fetch would cause every HTTP call to fail with "context canceled". Token
// fetches must outlive the request that triggered them.
type loginTokenSource struct{ cfg Config }

func (s *loginTokenSource) Token() (*oauth2.Token, error) {
	return s.cfg.login(context.Background())
}

// loginResponse is the shape returned by /auth/login and /auth/profile.
type loginResponse struct {
	Login             string `json:"login"`
	Code              string `json:"code"`
	AccessToken       string `json:"accessToken"`
	MFARequired       bool   `json:"mfaRequired"`
	MFAEnrollRequired bool   `json:"mfaEnrollRequired"`
	Memberships       []struct {
		ID string `json:"id"`
	} `json:"memberships"`
}

// httpClient returns the configured HTTP client or the default.
func (c Config) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// postJSON POSTs a JSON-encoded body to url and decodes the JSON response into dst.
// It returns an error on non-200 status (including up to 512 bytes of body).
func (c Config) postJSON(ctx context.Context, rawURL string, payload any, dst any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("POST %s failed: HTTP %d: %s", rawURL, resp.StatusCode, snippet)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// login performs Medplum email+password login and returns the access token.
// It implements the full Medplum login flow:
//  1. POST /auth/login → may return accessToken (fast path), code, or memberships.
//  2. If memberships are present, POST /auth/profile to select the first one and get a code.
//  3. Exchange the code at /oauth2/token for an access token.
func (c Config) login(ctx context.Context) (*oauth2.Token, error) {
	base := strings.TrimRight(c.BaseURL, "/")

	// Generate PKCE verifier and challenge (S256) required by Medplum v5+ native login.
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		return nil, fmt.Errorf("generate pkce verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(verifierBytes)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	// Step 1: POST /auth/login
	var loginResp loginResponse
	if err := c.postJSON(ctx, base+"/auth/login", map[string]string{
		"email":               c.Email,
		"password":            c.Password,
		"codeChallenge":       challenge,
		"codeChallengeMethod": "S256",
	}, &loginResp); err != nil {
		return nil, fmt.Errorf("login failed: %w", err)
	}

	// Step 3 (fast path): older Medplum versions return accessToken directly.
	if loginResp.AccessToken != "" {
		return &oauth2.Token{AccessToken: loginResp.AccessToken, TokenType: "Bearer"}, nil
	}

	// MFA is not supported for automated/provider login.
	if loginResp.MFARequired || loginResp.MFAEnrollRequired {
		return nil, fmt.Errorf("MFA-enabled accounts are not supported for automated login")
	}

	// Step 2: determine the auth code.
	code := loginResp.Code
	if code == "" {
		if len(loginResp.Memberships) == 0 {
			return nil, fmt.Errorf("login did not return an authorization code or memberships")
		}
		// Select the first membership to obtain a code.
		var profileResp loginResponse
		if err := c.postJSON(ctx, base+"/auth/profile", map[string]string{
			"login":   loginResp.Login,
			"profile": loginResp.Memberships[0].ID,
		}, &profileResp); err != nil {
			return nil, fmt.Errorf("profile selection failed: %w", err)
		}
		if profileResp.MFARequired || profileResp.MFAEnrollRequired {
			return nil, fmt.Errorf("MFA-enabled accounts are not supported for automated login")
		}
		if profileResp.Code == "" {
			return nil, fmt.Errorf("profile selection did not return an authorization code")
		}
		code = profileResp.Code
	}

	// Step 3: exchange the code for an access token at /oauth2/token.
	tokenURL := base + "/oauth2/token"
	formData := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"code_verifier": {verifier},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("token exchange failed: HTTP %d: %s", resp.StatusCode, snippet)
	}
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, err
	}
	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token exchange response missing access_token")
	}
	tok := &oauth2.Token{AccessToken: tokenResp.AccessToken, TokenType: "Bearer"}
	if tokenResp.ExpiresIn > 0 {
		tok.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	return tok, nil
}
