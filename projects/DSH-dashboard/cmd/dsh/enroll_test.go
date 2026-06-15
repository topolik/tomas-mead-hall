package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"dsh/internal/auth"
	"dsh/internal/config"
	"dsh/internal/db"
	"dsh/internal/handler"
)

const tsOrigin = "https://dsh-1.your-tailnet.ts.net"
const tsOrigin = "https://dsh-1.your-tailnet.ts.net"

// setupEnrollServer builds a server that mirrors the real multi-origin
// (localhost + Tailscale) deployment, with a setup token wired in and an
// authenticated session ready — the exact context in which a user enrolls a
// new device.
func setupEnrollServer(t *testing.T) (srv *httptest.Server, sessCookie *http.Cookie, csrf string) {
	t.Helper()

	f, err := os.CreateTemp("", "dsh_enroll_*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	database, err := db.Open(f.Name())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	cfg := &config.Config{
		Port:   "0",
		DBPath: f.Name(),
		Origin: "http://localhost:9090," + tsOrigin,
	}
	if err := bootstrap(database); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	// An existing passkey makes /setup require a token (the real-world state),
	// and gives us a user to authenticate as.
	if _, err := database.Exec(
		`INSERT INTO passkey_credentials(user_id, credential_id, public_key, sign_count, flags, name)
		 VALUES(1,'existing-cred',?,0,0,'laptop')`, []byte{0x01}); err != nil {
		t.Fatalf("seed passkey: %v", err)
	}

	csrf = "csrf-test-token"
	sessID, err := auth.CreateSession(database, 1, auth.SessionData{CSRFToken: csrf})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	sessCookie = &http.Cookie{Name: auth.SessionCookieName, Value: sessID}

	waMap, err := auth.NewWebAuthnMap(cfg.Origins())
	if err != nil {
		t.Fatalf("webauthn: %v", err)
	}
	tmpls, err := loadTemplates()
	if err != nil {
		t.Fatalf("loadTemplates: %v", err)
	}

	setupToken := handler.NewSetupToken("boot-value", f.Name()+".token")
	t.Cleanup(func() { os.Remove(f.Name() + ".token") })

	mux := buildMux(database, tmpls, waMap, "jwt-secret", "", "", cfg, setupToken)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, sessCookie, csrf
}

func rpIDFor(t *testing.T, srv *httptest.Server, path, host string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Registration (CredentialCreation) carries the RP id under publicKey.rp.id;
	// authentication (CredentialAssertion) carries it under publicKey.rpId.
	var body struct {
		PublicKey struct {
			RPID string `json:"rpId"`
			RP   struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if body.PublicKey.RPID != "" {
		return resp.StatusCode, body.PublicKey.RPID
	}
	return resp.StatusCode, body.PublicKey.RP.ID
}

func TestEnrollDevice_RequiresSession(t *testing.T) {
	srv, _, _ := setupEnrollServer(t)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("POST", srv.URL+"/settings/passkeys/enroll", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// No session → RequireSession redirects to /login.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302 redirect", resp.StatusCode)
	}
	if loc := resp.Header.Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, want /login", loc)
	}
}

func TestEnrollDevice_RequiresCSRF(t *testing.T) {
	srv, cookie, _ := setupEnrollServer(t)
	req, _ := http.NewRequest("POST", srv.URL+"/settings/passkeys/enroll", nil)
	req.AddCookie(cookie) // session but no CSRF header
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (CSRF)", resp.StatusCode)
	}
}

func TestEnrollDevice_FullFlow(t *testing.T) {
	srv, cookie, csrf := setupEnrollServer(t)

	// 1. Mint an enrollment link.
	req, _ := http.NewRequest("POST", srv.URL+"/settings/passkeys/enroll", nil)
	req.AddCookie(cookie)
	req.Header.Set("X-CSRF-Token", csrf)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enroll status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		URL       string `json:"url"`
		QR        string `json:"qr"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The link must point at the EXTERNAL origin (a phone can't reach localhost).
	if !strings.HasPrefix(out.URL, tsOrigin+"/setup?token=") {
		t.Fatalf("url = %q, want prefix %q/setup?token=", out.URL, tsOrigin)
	}
	if !strings.HasPrefix(out.QR, "data:image/png;base64,") || len(out.QR) < 100 {
		t.Errorf("qr looks wrong: %.40q...", out.QR)
	}
	if out.ExpiresIn != 600 {
		t.Errorf("expires_in = %d, want 600", out.ExpiresIn)
	}

	tok := out.URL[strings.Index(out.URL, "token=")+len("token="):]

	// 2. The fresh token unlocks /setup even though a passkey already exists.
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	sresp, err := client.Get(srv.URL + "/setup?token=" + tok)
	if err != nil {
		t.Fatal(err)
	}
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("/setup?token=fresh status = %d, want 200 (not redirect)", sresp.StatusCode)
	}

	// 3. Registration begun via the Tailscale host binds to the ts.net RP.
	code, rpid := rpIDFor(t, srv, "/setup/passkey/begin?token="+tok, "dsh-1.your-tailnet.ts.net")
	if code != http.StatusOK {
		t.Fatalf("begin status = %d, want 200", code)
	}
	if rpid != "dsh-1.your-tailnet.ts.net" {
		t.Fatalf("rpId = %q, want dsh-1.your-tailnet.ts.net", rpid)
	code, rpid := rpIDFor(t, srv, "/setup/passkey/begin?token="+tok, "dsh-1.your-tailnet.ts.net")
	if code != http.StatusOK {
		t.Fatalf("begin status = %d, want 200", code)
	}
	if rpid != "dsh-1.your-tailnet.ts.net" {
		t.Fatalf("rpId = %q, want dsh-1.your-tailnet.ts.net", rpid)
	}

	// 4. A wrong token is rejected.
	wresp, err := client.Get(srv.URL + "/setup/passkey/begin?token=wrong")
	if err != nil {
		t.Fatal(err)
	}
	wresp.Body.Close()
	if wresp.StatusCode != http.StatusForbidden {
		t.Errorf("begin with wrong token = %d, want 403", wresp.StatusCode)
	}
}

// TestWAForRequestDeterministicFallback proves an unknown Host always resolves to
// the same (default) RP, instead of a random map entry that could flip the rpId
// between requests and break the WebAuthn ceremony.
func TestWAForRequestDeterministicFallback(t *testing.T) {
	srv, _, _ := setupEnrollServer(t)
	for i := 0; i < 10; i++ {
		code, rpid := rpIDFor(t, srv, "/auth/passkey/login/begin", "totally-unknown-host.example")
		if code != http.StatusOK {
			t.Fatalf("login begin status = %d", code)
		}
		if rpid != "localhost" {
			t.Fatalf("fallback rpId = %q, want localhost (first configured origin), iter %d", rpid, i)
		}
	}
}

// TestWAForRequest_KnownHostsExact confirms exact host matches still win.
func TestWAForRequest_KnownHostsExact(t *testing.T) {
	srv, _, _ := setupEnrollServer(t)
	if _, rpid := rpIDFor(t, srv, "/auth/passkey/login/begin", "dsh-1.your-tailnet.ts.net"); rpid != "dsh-1.your-tailnet.ts.net" {
	if _, rpid := rpIDFor(t, srv, "/auth/passkey/login/begin", "dsh-1.your-tailnet.ts.net"); rpid != "dsh-1.your-tailnet.ts.net" {
		t.Errorf("ts.net host rpId = %q", rpid)
	}
	if _, rpid := rpIDFor(t, srv, "/auth/passkey/login/begin", "localhost"); rpid != "localhost" {
		t.Errorf("localhost host rpId = %q", rpid)
	}
}
