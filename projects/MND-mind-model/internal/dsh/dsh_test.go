package dsh

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// T29: token caching, post, dismissed query params, error surfacing.
func TestClient(t *testing.T) {
	tokenCalls := 0
	var gotNotif Notification
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/oauth/token":
			tokenCalls++
			json.NewEncoder(w).Encode(map[string]any{"access_token": "tok123", "expires_in": 3600})
		case r.URL.Path == "/api/v1/notifications" && r.Method == "POST":
			if r.Header.Get("Authorization") != "Bearer tok123" {
				w.WriteHeader(401)
				return
			}
			json.NewDecoder(r.Body).Decode(&gotNotif)
			w.Write([]byte(`{}`))
		case r.URL.Path == "/api/v1/notifications" && r.Method == "GET":
			gotQuery = r.URL.RawQuery
			json.NewEncoder(w).Encode([]Previous{{ID: 7, Message: "[MND ask abc] q", Comment: "use flat files, and stay on task"}})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := NewClient(Config{URL: srv.URL, ClientID: "id", ClientSecret: "sec"})

	if err := c.PostNotification(Notification{ProjectCode: "MND", Message: "m", Type: "action_needed", Priority: "Q1"}); err != nil {
		t.Fatal(err)
	}
	if gotNotif.ProjectCode != "MND" || gotNotif.Type != "action_needed" {
		t.Fatalf("payload wrong: %+v", gotNotif)
	}

	notifs, err := c.GetDismissedWithComments("MND", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifs) != 1 || notifs[0].Comment == "" {
		t.Fatalf("dismissed fetch wrong: %+v", notifs)
	}
	for _, want := range []string{"project_code=MND", "include_dismissed=true", "has_comment=true"} {
		if !strings.Contains(gotQuery, want) {
			t.Fatalf("query missing %q: %s", want, gotQuery)
		}
	}
	if tokenCalls != 1 {
		t.Fatalf("token not cached: %d calls", tokenCalls)
	}
}

// T29: config validation.
func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsh.yaml")
	os.WriteFile(path, []byte("url: http://x\nclient_id: a\nclient_secret: b\n"), 0o600)
	if _, err := LoadConfig(path); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, []byte("url: http://x\nclient_id: \nclient_secret: b\n"), 0o600)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("empty client_id must error")
	}
	if _, err := LoadConfig(filepath.Join(dir, "missing.yaml")); err == nil || !strings.Contains(err.Error(), "dsh.yaml.example") {
		t.Fatalf("missing config must point at the example: %v", err)
	}
}
