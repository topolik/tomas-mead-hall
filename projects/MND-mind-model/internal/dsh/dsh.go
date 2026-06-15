// Package dsh is a trimmed DSH dashboard client (GML pattern): OAuth
// client-credentials auth, notification post/update, dismissed-with-comment
// fetch. This is the human-in-the-loop channel — low-confidence escalations
// go out, Tomas's corrective comments come back.
package dsh

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config comes from data/dsh.yaml (gitignored; see dsh.yaml.example).
type Config struct {
	URL          string `yaml:"url"`
	ClientID     string `yaml:"client_id"`
	ClientSecret string `yaml:"client_secret"`
}

// LoadConfig reads and sanity-checks the client config.
func LoadConfig(path string) (Config, error) {
	var c Config
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("dsh config: %w (copy dsh.yaml.example and fill creds from DSH /admin/clients)", err)
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("dsh config parse: %w", err)
	}
	if c.URL == "" || c.ClientID == "" || c.ClientSecret == "" {
		return c, fmt.Errorf("dsh config %s: url, client_id, client_secret are all required", path)
	}
	return c, nil
}

type Client struct {
	cfg         Config
	accessToken string
	expiry      time.Time
	http        *http.Client
}

func NewClient(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

func (c *Client) token() (string, error) {
	if c.accessToken != "" && time.Now().Add(time.Minute).Before(c.expiry) {
		return c.accessToken, nil
	}
	resp, err := c.http.PostForm(c.cfg.URL+"/oauth/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.cfg.ClientID},
		"client_secret": {c.cfg.ClientSecret},
	})
	if err != nil {
		return "", fmt.Errorf("DSH token request: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("DSH token parse: %w", err)
	}
	if result.Error != "" || result.AccessToken == "" {
		return "", fmt.Errorf("DSH token failed: %s (HTTP %d)", result.Error, resp.StatusCode)
	}
	c.accessToken = result.AccessToken
	if result.ExpiresIn > 0 {
		c.expiry = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second)
	}
	return c.accessToken, nil
}

func (c *Client) do(method, path string, payload any) (*http.Response, error) {
	tok, err := c.token()
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.cfg.URL+path, body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	return c.http.Do(req)
}

// Notification is the POST /api/v1/notifications payload.
type Notification struct {
	ProjectCode string `json:"project_code"`
	Message     string `json:"message"`
	Type        string `json:"type"`     // action_needed | info
	Priority    string `json:"priority"` // Q1..Q4
	Link        string `json:"link,omitempty"`
}

func (c *Client) PostNotification(n Notification) error {
	resp, err := c.do("POST", "/api/v1/notifications", n)
	if err != nil {
		return fmt.Errorf("DSH notification post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DSH notification failed (HTTP %d): %s", resp.StatusCode, b)
	}
	return nil
}

// UpdateNotification refreshes an ACTIVE notification in place; DSH returns
// 404 for dismissed ones (never resurrects — GML lesson).
func (c *Client) UpdateNotification(id int64, message, priority string) error {
	payload := struct {
		Message  string `json:"message"`
		Priority string `json:"priority,omitempty"`
	}{message, priority}
	resp, err := c.do("PATCH", fmt.Sprintf("/api/v1/notifications/%d", id), payload)
	if err != nil {
		return fmt.Errorf("DSH notification update: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DSH notification update failed (HTTP %d): %s", resp.StatusCode, b)
	}
	return nil
}

// Previous is one notification as returned by GET /api/v1/notifications.
type Previous struct {
	ID          int64  `json:"id"`
	ProjectCode string `json:"project_code"`
	Message     string `json:"message"`
	Comment     string `json:"comment"`
	CreatedAt   string `json:"created_at"`
	DismissedAt string `json:"dismissed_at,omitempty"`
}

// GetNotifications lists active notifications for a project.
func (c *Client) GetNotifications(projectCode string, limit int) ([]Previous, error) {
	return c.getNotifications(fmt.Sprintf("/api/v1/notifications?project_code=%s&limit=%d",
		url.QueryEscape(projectCode), limit))
}

// GetDismissedWithComments lists dismissed notifications carrying a comment —
// each comment is a direction from Tomas.
func (c *Client) GetDismissedWithComments(projectCode string, limit int) ([]Previous, error) {
	return c.getNotifications(fmt.Sprintf(
		"/api/v1/notifications?project_code=%s&limit=%d&include_dismissed=true&has_comment=true",
		url.QueryEscape(projectCode), limit))
}

func (c *Client) getNotifications(path string) ([]Previous, error) {
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, fmt.Errorf("DSH get notifications: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DSH get notifications (HTTP %d): %s", resp.StatusCode, b)
	}
	var out []Previous
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("DSH parse notifications: %w", err)
	}
	return out, nil
}
