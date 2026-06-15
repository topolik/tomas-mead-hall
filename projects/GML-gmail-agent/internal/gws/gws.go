// Package gws wraps the gws CLI as a subprocess, passing credentials via token env var.
package gws

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/topolik/gml-gmail-agent/internal/creds"
)

// Run executes a gws command with the given credentials and returns raw JSON output.
// The access token is injected via GOOGLE_WORKSPACE_CLI_TOKEN — never written to disk.
func Run(cr *creds.Creds, args ...string) ([]byte, error) {
	token, err := cr.Token()
	if err != nil {
		return nil, fmt.Errorf("gws token: %w", err)
	}

	cmd := exec.Command("gws", args...)
	cmd.Env = append(os.Environ(),
		"GOOGLE_WORKSPACE_CLI_TOKEN="+token,
		"GOOGLE_WORKSPACE_CLI_KEYRING_BACKEND=file",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gws %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}

// RunJSON executes a gws command and unmarshals the JSON output into v.
func RunJSON(cr *creds.Creds, v any, args ...string) error {
	out, err := Run(cr, args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

// Message is a Gmail message envelope (headers + optional body).
type Message struct {
	ID      string   `json:"id"`
	Snippet string   `json:"snippet"`
	Payload Payload  `json:"payload"`
	Labels  []string `json:"labelIds"`
	// InternalDate is milliseconds since epoch as a string
	InternalDate string `json:"internalDate"`
}

type Payload struct {
	MimeType string   `json:"mimeType"`
	Headers  []Header `json:"headers"`
	Body     Body     `json:"body"`
	Parts    []Part   `json:"parts"`
}

type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Body struct {
	Size int    `json:"size"`
	Data string `json:"data"`
}

type Part struct {
	MimeType string `json:"mimeType"`
	Body     Body   `json:"body"`
	Parts    []Part `json:"parts"`
}

func (m *Message) Header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func (m *Message) Subject() string { return m.Header("subject") }
func (m *Message) From() string    { return m.Header("from") }

func (m *Message) HasLabel(label string) bool {
	for _, l := range m.Labels {
		if l == label {
			return true
		}
	}
	return false
}

// ListResult is the response from gmail.users.messages.list.
type ListResult struct {
	Messages           []MessageRef `json:"messages"`
	NextPageToken      string       `json:"nextPageToken"`
	ResultSizeEstimate int          `json:"resultSizeEstimate"`
}

type MessageRef struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// Profile is the response from gmail.users.getProfile.
type Profile struct {
	EmailAddress  string `json:"emailAddress"`
	MessagesTotal int    `json:"messagesTotal"`
	ThreadsTotal  int    `json:"threadsTotal"`
	HistoryID     string `json:"historyId"`
}

// GetProfile returns the authenticated user's Gmail profile.
func GetProfile(cr *creds.Creds) (*Profile, error) {
	var p Profile
	err := RunJSON(cr, &p, "gmail", "users", "getProfile",
		"--params", `{"userId":"me"}`)
	return &p, err
}

// CountMessages returns the estimated message count for a query (single API call, no pagination).
func CountMessages(cr *creds.Creds, query string) (int, error) {
	params := fmt.Sprintf(`{"userId":"me","q":%s,"maxResults":500}`, jsonStr(query))
	var r ListResult
	err := RunJSON(cr, &r, "gmail", "users", "messages", "list", "--params", params)
	if err != nil {
		return 0, err
	}
	return r.ResultSizeEstimate, nil
}

// ListMessages returns message refs for the given Gmail query, up to maxPages pages.
func ListMessages(cr *creds.Creds, query string, maxPages int) ([]MessageRef, error) {
	params := fmt.Sprintf(`{"userId":"me","q":%s,"maxResults":500}`, jsonStr(query))
	limit := maxPages
	if limit <= 0 {
		limit = 10000
	}
	args := []string{"gmail", "users", "messages", "list",
		"--params", params,
		"--page-all",
		"--page-limit", fmt.Sprintf("%d", limit)}

	// gws --page-all / --page-limit outputs NDJSON (one JSON object per line)
	out, err := Run(cr, args...)
	if err != nil {
		return nil, err
	}

	var all []MessageRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var r ListResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			// Try single-object (no pagination)
			var sr ListResult
			if err2 := json.Unmarshal(out, &sr); err2 == nil {
				return sr.Messages, nil
			}
			return nil, fmt.Errorf("parsing list response: %w", err)
		}
		all = append(all, r.Messages...)
	}
	return all, nil
}

// GetMessage fetches full message metadata (headers + labels) for one ID.
func GetMessage(cr *creds.Creds, id string) (*Message, error) {
	params := fmt.Sprintf(`{"userId":"me","id":%s,"format":"metadata","metadataHeaders":["From","Subject","Date"]}`, jsonStr(id))
	var m Message
	err := RunJSON(cr, &m, "gmail", "users", "messages", "get",
		"--params", params)
	return &m, err
}

// GetMessageFull fetches a message with full body content.
func GetMessageFull(cr *creds.Creds, id string) (*Message, error) {
	params := fmt.Sprintf(`{"userId":"me","id":%s,"format":"full"}`, jsonStr(id))
	var m Message
	err := RunJSON(cr, &m, "gmail", "users", "messages", "get",
		"--params", params)
	return &m, err
}

// ExtractBody returns the plain text body of a message.
// It prefers text/plain; falls back to text/html if no plain part exists.
// The second return value is true if the content is HTML (needs conversion).
func (m *Message) ExtractBody() (string, bool) {
	plain, html := extractParts(m.Payload.Parts)
	if m.Payload.Body.Data != "" {
		decoded := decodeBase64URL(m.Payload.Body.Data)
		if strings.HasPrefix(m.Payload.MimeType, "text/plain") {
			plain = decoded
		} else if strings.HasPrefix(m.Payload.MimeType, "text/html") {
			html = decoded
		}
	}
	if plain != "" {
		return plain, false
	}
	if html != "" {
		return html, true
	}
	return m.Snippet, false
}

func extractParts(parts []Part) (plain, html string) {
	for _, p := range parts {
		if len(p.Parts) > 0 {
			pp, hh := extractParts(p.Parts)
			if pp != "" {
				plain = pp
			}
			if hh != "" {
				html = hh
			}
			continue
		}
		decoded := decodeBase64URL(p.Body.Data)
		if decoded == "" {
			continue
		}
		switch {
		case strings.HasPrefix(p.MimeType, "text/plain"):
			plain = decoded
		case strings.HasPrefix(p.MimeType, "text/html"):
			html = decoded
		}
	}
	return
}

func decodeBase64URL(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.URLEncoding.DecodeString(s)
	if err != nil {
		b, err = base64.RawURLEncoding.DecodeString(s)
		if err != nil {
			return ""
		}
	}
	return string(b)
}

// Archive removes the INBOX label from a message (Gmail archive = remove INBOX label only).
func Archive(cr *creds.Creds, id string) error {
	body := `{"removeLabelIds":["INBOX"]}`
	params := fmt.Sprintf(`{"userId":"me","id":%s}`, jsonStr(id))
	_, err := Run(cr, "gmail", "users", "messages", "modify",
		"--params", params,
		"--json", body)
	return err
}

// ArchiveWithLabel atomically removes INBOX and adds a tracing label in a single modify call.
// The tracingLabelID must be obtained via EnsureTracingLabel.
func ArchiveWithLabel(cr *creds.Creds, id string, tracingLabelID string) error {
	body := fmt.Sprintf(`{"removeLabelIds":["INBOX"],"addLabelIds":[%s]}`, jsonStr(tracingLabelID))
	params := fmt.Sprintf(`{"userId":"me","id":%s}`, jsonStr(id))
	_, err := Run(cr, "gmail", "users", "messages", "modify",
		"--params", params,
		"--json", body)
	return err
}

// Label is a Gmail label.
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// ListLabelsResult is the response from gmail.users.labels.list.
type ListLabelsResult struct {
	Labels []Label `json:"labels"`
}

// ListLabels returns all labels in the user's mailbox.
func ListLabels(cr *creds.Creds) ([]Label, error) {
	var r ListLabelsResult
	err := RunJSON(cr, &r, "gmail", "users", "labels", "list",
		"--params", `{"userId":"me"}`)
	return r.Labels, err
}

// CreateLabel creates a new Gmail label and returns it.
func CreateLabel(cr *creds.Creds, name string) (*Label, error) {
	body := fmt.Sprintf(`{"name":%s,"labelListVisibility":"labelShow","messageListVisibility":"show"}`, jsonStr(name))
	var l Label
	err := RunJSON(cr, &l, "gmail", "users", "labels", "create",
		"--params", `{"userId":"me"}`,
		"--json", body)
	return &l, err
}

// EnsureTracingLabel returns the label ID for a GML tracing label (e.g. "GML/archived"),
// creating it if it doesn't exist. Only labels under "GML/" are allowed.
func EnsureTracingLabel(cr *creds.Creds, name string) (string, error) {
	if !strings.HasPrefix(name, "GML/") {
		return "", fmt.Errorf("tracing label must start with GML/, got %q", name)
	}
	labels, err := ListLabels(cr)
	if err != nil {
		return "", fmt.Errorf("listing labels: %w", err)
	}
	for _, l := range labels {
		if l.Name == name {
			return l.ID, nil
		}
	}
	l, err := CreateLabel(cr, name)
	if err != nil {
		return "", fmt.Errorf("creating label %q: %w", name, err)
	}
	return l.ID, nil
}

// ThreadListResult is the response from gmail.users.threads.list.
type ThreadListResult struct {
	Threads            []ThreadRef `json:"threads"`
	NextPageToken      string      `json:"nextPageToken"`
	ResultSizeEstimate int         `json:"resultSizeEstimate"`
}

type ThreadRef struct {
	ID      string `json:"id"`
	Snippet string `json:"snippet"`
}

// Thread is a Gmail thread with all its messages.
type Thread struct {
	ID       string    `json:"id"`
	Messages []Message `json:"messages"`
}

// ListThreads returns thread refs for the given Gmail query, up to maxPages pages.
func ListThreads(cr *creds.Creds, query string, maxPages int) ([]ThreadRef, error) {
	params := fmt.Sprintf(`{"userId":"me","q":%s,"maxResults":100}`, jsonStr(query))
	limit := maxPages
	if limit <= 0 {
		limit = 10
	}
	args := []string{"gmail", "users", "threads", "list",
		"--params", params,
		"--page-all",
		"--page-limit", fmt.Sprintf("%d", limit)}

	out, err := Run(cr, args...)
	if err != nil {
		return nil, err
	}

	var all []ThreadRef
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		var r ThreadListResult
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			var sr ThreadListResult
			if err2 := json.Unmarshal(out, &sr); err2 == nil {
				return sr.Threads, nil
			}
			return nil, fmt.Errorf("parsing thread list response: %w", err)
		}
		all = append(all, r.Threads...)
	}
	return all, nil
}

// GetThread fetches a thread with metadata for all its messages (headers + labels).
func GetThread(cr *creds.Creds, id string) (*Thread, error) {
	params := fmt.Sprintf(`{"userId":"me","id":%s,"format":"metadata","metadataHeaders":["From","Subject","Date"]}`, jsonStr(id))
	var t Thread
	err := RunJSON(cr, &t, "gmail", "users", "threads", "get",
		"--params", params)
	return &t, err
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
