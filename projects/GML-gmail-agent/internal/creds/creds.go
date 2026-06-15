// Package creds manages OAuth2 credentials in memory — never written to disk.
package creds

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Creds holds OAuth2 credentials exclusively in heap memory.
type Creds struct {
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	expiry       time.Time
	mu           sync.Mutex
}

// rawCreds is the JSON shape from `gws auth export --unmasked`.
type rawCreds struct {
	ClientID     string          `json:"client_id"`
	ClientSecret string          `json:"client_secret"`
	RefreshToken string          `json:"refresh_token"`
	AccessToken  string          `json:"access_token"`
	// gws may use any of these field names for token expiry
	Expiry      json.RawMessage `json:"expiry"`
	TokenExpiry json.RawMessage `json:"token_expiry"`
	ExpiresAt   json.RawMessage `json:"expires_at"`
	ExpiryTime  json.RawMessage `json:"expiry_time"`
}

// Load reads a credentials JSON from r (typically os.Stdin) and returns a Creds.
// If the access token expiry is missing or unparseable, Token() will refresh immediately.
func Load(r io.Reader) (*Creds, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("reading credentials: %w", err)
	}
	var raw rawCreds
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing credentials JSON: %w", err)
	}
	if raw.ClientID == "" {
		return nil, fmt.Errorf("credentials missing client_id")
	}
	if raw.RefreshToken == "" {
		return nil, fmt.Errorf("credentials missing refresh_token")
	}

	c := &Creds{
		clientID:     raw.ClientID,
		clientSecret: raw.ClientSecret,
		refreshToken: raw.RefreshToken,
		accessToken:  raw.AccessToken,
	}
	for _, field := range []json.RawMessage{raw.Expiry, raw.TokenExpiry, raw.ExpiresAt, raw.ExpiryTime} {
		if t := parseExpiryField(field); !t.IsZero() {
			c.expiry = t
			break
		}
	}
	return c, nil
}

// Token returns a valid access token, refreshing via Google OAuth2 if it expires within 5 minutes.
func (c *Creds) Token() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken != "" && time.Now().Add(5*time.Minute).Before(c.expiry) {
		return c.accessToken, nil
	}
	return c.refresh()
}

func (c *Creds) refresh() (string, error) {
	resp, err := http.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {c.refreshToken},
		"grant_type":    {"refresh_token"},
	})
	if err != nil {
		return "", fmt.Errorf("token refresh request: %w", err)
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parsing token response: %w", err)
	}
	if result.Error != "" {
		return "", fmt.Errorf("token refresh failed: %s: %s", result.Error, result.ErrorDesc)
	}
	if result.AccessToken == "" {
		return "", fmt.Errorf("token refresh returned empty access token")
	}

	c.accessToken = result.AccessToken
	if result.ExpiresIn > 0 {
		c.expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	}
	return c.accessToken, nil
}

// parseExpiryField tries to parse a JSON expiry field as RFC3339 string or Unix timestamp.
func parseExpiryField(raw json.RawMessage) time.Time {
	if len(raw) == 0 {
		return time.Time{}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			if t, err := time.Parse(layout, s); err == nil {
				return t
			}
		}
	}
	var ts float64
	if json.Unmarshal(raw, &ts) == nil && ts > 0 {
		return time.Unix(int64(ts), 0)
	}
	return time.Time{}
}
