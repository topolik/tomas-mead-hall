package main

// Real-data acceptance for Threads (iteration 026, GML processed-tracking).
//
// Runs ONLY when DSH_ACCEPT_DB points at a COPY of the live dsh.db:
//
//	DSH_ACCEPT_DB=/tmp/dsh-accept.db go test ./cmd/dsh -run TestAcceptance_GMLProcessedTracking -v
//
// It opens the copy (migrations apply, incl. 011_threads), picks a real
// dismissed GML insight notification, and exercises the full GML flow over
// HTTP with a real OAuth2 token: create thread → resolve → query
// ?ref_type=notification&ref_id=N&status=resolved. The copy is mutated; the
// live DB is never touched.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"dsh/internal/auth"
	"dsh/internal/config"
	"dsh/internal/db"
)

func TestAcceptance_GMLProcessedTracking(t *testing.T) {
	dbPath := os.Getenv("DSH_ACCEPT_DB")
	if dbPath == "" {
		t.Skip("set DSH_ACCEPT_DB to a copy of the live dsh.db to run")
	}

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open %s: %v", dbPath, err)
	}
	defer database.Close()

	// A real dismissed GML insight from production data.
	var nid int64
	var message string
	err = database.QueryRow(
		`SELECT id, message FROM notifications
		 WHERE dismissed_at IS NOT NULL AND project_code='GML'
		 ORDER BY id DESC LIMIT 1`,
	).Scan(&nid, &message)
	if err != nil {
		t.Fatalf("no dismissed GML insight in the DB copy: %v", err)
	}
	t.Logf("real dismissed insight #%d: %.80q", nid, message)

	// Real server stack: mux + JWT auth, same as production.
	cfg := &config.Config{Port: "0", DBPath: dbPath, Origin: "http://localhost"}
	waMap, err := auth.NewWebAuthnMap(cfg.Origins())
	if err != nil {
		t.Fatal(err)
	}
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatal(err)
	}
	jwtSecret := "acceptance-secret"
	srv := httptest.NewServer(buildMux(database, tmpls, waMap, jwtSecret, "", "", cfg, nil))
	defer srv.Close()

	clientID, secret, err := auth.CreateOAuth2Client(database, "gml-acceptance")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.PostForm(srv.URL+"/oauth/token", url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {clientID},
		"client_secret": {secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if tok.AccessToken == "" {
		t.Fatal("no access token")
	}

	api := func(method, path, body string) (*http.Response, []byte) {
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		return r, buf.Bytes()
	}

	// Step 0: GML's skip-check on an unprocessed insight → empty.
	probe := fmt.Sprintf("/api/v1/threads?ref_type=notification&ref_id=%d&status=resolved", nid)
	r, body := api("GET", probe, "")
	if r.StatusCode != 200 {
		t.Fatalf("probe: %d %s", r.StatusCode, body)
	}
	var threads []map[string]any
	json.Unmarshal(body, &threads)
	if len(threads) != 0 {
		t.Logf("note: insight #%d already has %d resolved thread(s) — continuing", nid, len(threads))
	}

	// Step 1: GML posts a processed-thread for the insight.
	r, body = api("POST", "/api/v1/threads", fmt.Sprintf(
		`{"subject":"processed: insight #%d","body":"distilled into knowledge.yaml (acceptance run)","ref_type":"notification","ref_id":"%d"}`, nid, nid))
	if r.StatusCode != 200 {
		t.Fatalf("create thread: %d %s", r.StatusCode, body)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	json.Unmarshal(body, &created)

	// Step 2: resolve it.
	r, body = api("PATCH", fmt.Sprintf("/api/v1/threads/%d", created.ID), `{"status":"resolved"}`)
	if r.StatusCode != 200 {
		t.Fatalf("resolve: %d %s", r.StatusCode, body)
	}

	// Step 3: the GML skip-check now finds it.
	r, body = api("GET", probe, "")
	threads = nil
	json.Unmarshal(body, &threads)
	if len(threads) == 0 {
		t.Fatalf("GML contract failed: resolved thread not found for insight #%d", nid)
	}

	// Authorship must be the OAuth client name, not anything payload-supplied.
	var createdBy string
	database.QueryRow(`SELECT created_by FROM threads WHERE id=?`, created.ID).Scan(&createdBy)
	if createdBy != "gml-acceptance" {
		t.Errorf("created_by = %q, want the OAuth client name", createdBy)
	}

	t.Logf("OK: insight #%d processed-tracking round-trip on real data (thread #%d)", nid, created.ID)
}
