package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/topolik/gml-gmail-agent/internal/config"
)

type DSHClient struct {
	cfg         config.DSHConfig
	accessToken string
	expiry      time.Time
	http        *http.Client
}

func NewDSHClient(cfg config.DSHConfig) *DSHClient {
	return &DSHClient{
		cfg:  cfg,
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *DSHClient) token() (string, error) {
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

type Notification struct {
	ProjectCode string `json:"project_code"`
	Message     string `json:"message"`
	Type        string `json:"type"`
	Priority    string `json:"priority,omitempty"`
	Link        string `json:"link,omitempty"`
}

func (c *DSHClient) PostNotification(n Notification) error {
	tok, err := c.token()
	if err != nil {
		return err
	}
	body, _ := json.Marshal(n)
	req, _ := http.NewRequest("POST", c.cfg.URL+"/api/v1/notifications", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
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

// UpdateNotification refreshes an existing ACTIVE notification in place (PATCH).
// Used by the insight pipeline when learn re-derives an insight that matches an
// already-posted, not-yet-dismissed one (same identity key) — we update rather
// than post a duplicate. The DSH side guards on dismissed_at IS NULL, so this
// never resurrects a dismissed insight (it returns HTTP 404 for those).
func (c *DSHClient) UpdateNotification(id int64, message, link, priority string) error {
	tok, err := c.token()
	if err != nil {
		return err
	}
	payload := struct {
		Message  string `json:"message"`
		Link     string `json:"link"`
		Priority string `json:"priority,omitempty"`
	}{message, link, priority}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest("PATCH", fmt.Sprintf("%s/api/v1/notifications/%d", c.cfg.URL, id), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
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

type PreviousNotification struct {
	ID          int64  `json:"id"`
	ProjectCode string `json:"project_code"`
	Message     string `json:"message"`
	Type        string `json:"type"`
	Priority    string `json:"priority"`
	Link        string `json:"link"`
	Comment     string `json:"comment"`
	CreatedAt   string `json:"created_at"`
	DismissedAt string `json:"dismissed_at,omitempty"`
}

func (c *DSHClient) GetNotifications(projectCode string, limit int) ([]PreviousNotification, error) {
	tok, err := c.token()
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/api/v1/notifications?project_code=%s&limit=%d",
		c.cfg.URL, url.QueryEscape(projectCode), limit)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DSH get notifications: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DSH get notifications (HTTP %d): %s", resp.StatusCode, b)
	}
	var notifs []PreviousNotification
	if err := json.NewDecoder(resp.Body).Decode(&notifs); err != nil {
		return nil, fmt.Errorf("DSH parse notifications: %w", err)
	}
	return notifs, nil
}

func (c *DSHClient) GetDismissedNotifications(projectCode string, limit int, hasComment bool) ([]PreviousNotification, error) {
	tok, err := c.token()
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("%s/api/v1/notifications?project_code=%s&limit=%d&include_dismissed=true",
		c.cfg.URL, url.QueryEscape(projectCode), limit)
	if hasComment {
		u += "&has_comment=true"
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DSH get dismissed notifications: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DSH get dismissed notifications (HTTP %d): %s", resp.StatusCode, b)
	}
	var notifs []PreviousNotification
	if err := json.NewDecoder(resp.Body).Decode(&notifs); err != nil {
		return nil, fmt.Errorf("DSH parse dismissed notifications: %w", err)
	}
	return notifs, nil
}

func (c *DSHClient) PostTodo(text, priority, projectCode string) error {
	tok, err := c.token()
	if err != nil {
		return err
	}
	todo := struct {
		Text        string `json:"text"`
		Priority    string `json:"priority"`
		ProjectCode string `json:"project_code,omitempty"`
	}{text, priority, projectCode}
	body, _ := json.Marshal(todo)
	req, _ := http.NewRequest("POST", c.cfg.URL+"/api/v1/todos", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("DSH todo post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DSH todo failed (HTTP %d): %s", resp.StatusCode, b)
	}
	return nil
}

type Plan struct {
	ID          int64   `json:"id"`
	ProjectCode string  `json:"project_code"`
	Title       string  `json:"title"`
	Detail      string  `json:"detail"`
	Status      string  `json:"status"`
	Comment     string  `json:"comment"`
	CreatedAt   string  `json:"created_at"`
	DecidedAt   *string `json:"decided_at,omitempty"`
}

func (c *DSHClient) PostPlan(projectCode, title, detail string) (int64, error) {
	tok, err := c.token()
	if err != nil {
		return 0, err
	}
	plan := struct {
		ProjectCode string `json:"project_code"`
		Title       string `json:"title"`
		Detail      string `json:"detail"`
	}{projectCode, title, detail}
	body, _ := json.Marshal(plan)
	req, _ := http.NewRequest("POST", c.cfg.URL+"/api/v1/plans", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("DSH plan post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("DSH plan failed (HTTP %d): %s", resp.StatusCode, b)
	}
	var result struct {
		OK bool  `json:"ok"`
		ID int64 `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.ID, nil
}

func (c *DSHClient) GetPlans(status string) ([]Plan, error) {
	tok, err := c.token()
	if err != nil {
		return nil, err
	}
	u := c.cfg.URL + "/api/v1/plans"
	if status != "" {
		u += "?status=" + url.QueryEscape(status)
	}
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("DSH get plans: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("DSH get plans (HTTP %d): %s", resp.StatusCode, b)
	}
	var plans []Plan
	if err := json.NewDecoder(resp.Body).Decode(&plans); err != nil {
		return nil, fmt.Errorf("DSH parse plans: %w", err)
	}
	return plans, nil
}

func FormatPreviousNotifications(notifs []PreviousNotification) string {
	if len(notifs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, n := range notifs {
		ts := n.CreatedAt
		if len(ts) > 16 {
			ts = ts[:16]
		}
		fmt.Fprintf(&sb, "[%s] %s\n", ts, n.Message)
	}
	return sb.String()
}

func FormatDismissedNotifications(notifs []PreviousNotification) string {
	if len(notifs) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, n := range notifs {
		ts := n.CreatedAt
		if len(ts) > 16 {
			ts = ts[:16]
		}
		dismissed := n.DismissedAt
		if len(dismissed) > 16 {
			dismissed = dismissed[:16]
		}
		if dismissed != "" {
			fmt.Fprintf(&sb, "[insight #%d] [%s dismissed:%s] %s %s\n", n.ID, ts, dismissed, n.Priority, n.Message)
		} else {
			fmt.Fprintf(&sb, "[insight #%d] [%s] %s %s\n", n.ID, ts, n.Priority, n.Message)
		}
		if n.Link != "" {
			fmt.Fprintf(&sb, "  Link: %s\n", n.Link)
		}
		if n.Comment != "" {
			fmt.Fprintf(&sb, "  Comment: %q\n", n.Comment)
		}
	}
	return sb.String()
}
