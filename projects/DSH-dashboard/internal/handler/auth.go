package handler

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/skip2/go-qrcode"

	"dsh/internal/auth"
)

type AuthHandler struct {
	DB             *sql.DB
	Tmpls          *template.Template
	WAMap          map[string]*webauthn.WebAuthn
	JWTSec         string
	Issuer         string
	SetupToken     *SetupToken
	ExternalOrigin string // origin a phone/device uses for enrollment links (e.g. https://x.ts.net)
	DefaultRPID    string // deterministic fallback RP when Host matches no configured origin
}

func (h *AuthHandler) waForRequest(r *http.Request) *webauthn.WebAuthn {
	host := r.Host
	if i := indexOf(host, ':'); i >= 0 {
		host = host[:i]
	}
	if wa, ok := h.WAMap[host]; ok {
		return wa
	}
	// Deterministic fallback: the configured default RP. Map iteration order is
	// randomized, so falling back to `for range` could hand a phone a localhost
	// rpId one request and a ts.net rpId the next — breaking the ceremony.
	if wa, ok := h.WAMap[h.DefaultRPID]; ok {
		return wa
	}
	for _, wa := range h.WAMap {
		return wa
	}
	return nil
}

func indexOf(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// --- First-run setup (public, only when no passkeys exist) ---

func (h *AuthHandler) setupAllowed(r *http.Request) bool {
	if !hasPasskeys(h.DB) {
		return true
	}
	return h.SetupToken.Valid(r.URL.Query().Get("token"))
}

func (h *AuthHandler) SetupPage(w http.ResponseWriter, r *http.Request) {
	if !h.setupAllowed(r) {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	h.Tmpls.ExecuteTemplate(w, "setup.html", nil)
}

func (h *AuthHandler) SetupPasskeyBegin(w http.ResponseWriter, r *http.Request) {
	if !h.setupAllowed(r) {
		jsonError(w, http.StatusForbidden, "setup not allowed")
		return
	}
	userID, username := setupUser(h.DB)
	creation, err := auth.BeginPasskeyRegister(h.waForRequest(r), h.DB, userID, username)
	if err != nil {
		log.Printf("setup passkey begin: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	jsonOK(w, creation)
}

func (h *AuthHandler) SetupPasskeyFinish(w http.ResponseWriter, r *http.Request) {
	if !h.setupAllowed(r) {
		jsonError(w, http.StatusForbidden, "setup already complete")
		return
	}
	userID, username := setupUser(h.DB)

	parsed, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "parse error: "+err.Error())
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "Passkey"
	}
	if err := auth.FinishPasskeyRegister(h.waForRequest(r), h.DB, userID, username, parsed, name); err != nil {
		log.Printf("setup passkey finish: %v", err)
		jsonError(w, 500, err.Error())
		return
	}
	h.SetupToken.Consume()

	csrfToken, _ := auth.NewCSRFToken()
	sessID, err := auth.CreateSession(h.DB, userID, auth.SessionData{CSRFToken: csrfToken})
	if err != nil {
		jsonError(w, 500, "session error")
		return
	}
	auth.WriteAudit(h.DB, "passkey_registered", username, realIP(r), "first_run_setup")
	auth.SetSessionCookie(w, sessID)
	jsonOK(w, map[string]string{"status": "ok", "redirect": "/"})
}

// setupUser returns the single bootstrap user. Must only be called when no passkeys exist.
func setupUser(db *sql.DB) (int64, string) {
	var id int64
	var name string
	db.QueryRow(`SELECT id, username FROM users LIMIT 1`).Scan(&id, &name)
	return id, name
}

// --- Login (passkey only) ---

func (h *AuthHandler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if !hasPasskeys(h.DB) {
		http.Redirect(w, r, "/setup", http.StatusFound)
		return
	}
	h.Tmpls.ExecuteTemplate(w, "login.html", nil)
}

// --- Logout ---

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var actor string
	if sess, _, err := auth.SessionFromRequest(r, h.DB); err == nil {
		h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, sess.UserID).Scan(&actor)
	}
	c, err := r.Cookie(auth.SessionCookieName)
	if err == nil {
		_ = auth.DeleteSession(h.DB, c.Value)
	}
	auth.ClearSessionCookie(w)
	auth.WriteAudit(h.DB, "logout", actor, realIP(r), "")
	http.Redirect(w, r, "/login", http.StatusFound)
}

// --- Passkey settings (authenticated) ---

type passkeyRow struct {
	ID        int64
	Name      string
	CreatedAt string
}

func (h *AuthHandler) PasskeysPage(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromRequest(r)
	var username string
	h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&username)

	sessObj, data, _ := auth.SessionFromRequest(r, h.DB)
	data.CSRFToken, _ = auth.NewCSRFToken()
	_ = auth.UpdateSessionData(h.DB, sessObj.ID, *data)

	rows, err := h.DB.Query(
		`SELECT id, name, created_at FROM passkey_credentials WHERE user_id=? ORDER BY created_at`, userID,
	)
	var passkeys []passkeyRow
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var pk passkeyRow
			rows.Scan(&pk.ID, &pk.Name, &pk.CreatedAt)
			passkeys = append(passkeys, pk)
		}
	}

	notifBadge, planBadge, threadBadge := navBadges(h.DB)
	h.Tmpls.ExecuteTemplate(w, "settings_passkeys.html", map[string]any{
		"Passkeys":    passkeys,
		"CSRFToken":   data.CSRFToken,
		"Username":    username,
		"NotifBadge":  notifBadge,
		"PlanBadge":   planBadge,
		"ThreadBadge": threadBadge,
	})
}

// --- Device enrollment (authenticated: mint a fresh setup link for a new device) ---

// EnrollDevice mints a fresh, short-lived setup token and returns an enrollment
// URL (plus a QR code) pointing at the external origin. An already-authenticated
// user (e.g. logged in on their laptop) scans the QR with a new device — phone,
// tablet — and registers a passkey bound to the external RP. This replaces the
// boot-time token printed by run.sh, which expires 10 minutes after the container
// starts and so is useless for enrolling a device days later.
func (h *AuthHandler) EnrollDevice(w http.ResponseWriter, r *http.Request) {
	if h.SetupToken == nil {
		jsonError(w, http.StatusServiceUnavailable, "device enrollment not configured")
		return
	}

	origin := strings.TrimRight(h.ExternalOrigin, "/")
	if origin == "" {
		scheme := "https"
		if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
			scheme = "http"
		}
		origin = scheme + "://" + r.Host
	}

	tok, err := h.SetupToken.Regenerate()
	if err != nil {
		log.Printf("enroll device: %v", err)
		jsonError(w, 500, "internal error")
		return
	}

	link := origin + "/setup?token=" + tok
	png, err := qrcode.Encode(link, qrcode.Medium, 256)
	if err != nil {
		log.Printf("enroll device qr: %v", err)
		jsonError(w, 500, "qr error")
		return
	}

	var actor string
	if uid, ok := userIDFromRequest(r); ok {
		h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, uid).Scan(&actor)
	}
	auth.WriteAudit(h.DB, "device_enroll_token_issued", actor, realIP(r), origin)

	jsonOK(w, map[string]any{
		"url":        link,
		"qr":         "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
		"expires_in": int(h.SetupToken.TTL().Seconds()),
	})
}

// --- Passkey Remove ---

func (h *AuthHandler) PasskeyRemove(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromRequest(r)
	credID := r.FormValue("cred_id")
	var name string
	h.DB.QueryRow(`SELECT name FROM passkey_credentials WHERE id=? AND user_id=?`, credID, userID).Scan(&name)
	h.DB.Exec(`DELETE FROM passkey_credentials WHERE id=? AND user_id=?`, credID, userID)
	var username string
	h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&username)
	auth.WriteAudit(h.DB, "passkey_removed", username, realIP(r), name)
	http.Redirect(w, r, "/settings/passkeys", http.StatusFound)
}

// --- Passkey Registration (add another key while logged in) ---

func (h *AuthHandler) PasskeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromRequest(r)
	var username string
	h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&username)

	creation, err := auth.BeginPasskeyRegister(h.waForRequest(r), h.DB, userID, username)
	if err != nil {
		log.Printf("passkey register begin: %v", err)
		jsonError(w, 500, "internal error")
		return
	}
	jsonOK(w, creation)
}

func (h *AuthHandler) PasskeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	userID, _ := userIDFromRequest(r)

	parsed, err := protocol.ParseCredentialCreationResponseBody(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "parse error: "+err.Error())
		return
	}

	var username string
	h.DB.QueryRow(`SELECT username FROM users WHERE id=?`, userID).Scan(&username)

	name := r.URL.Query().Get("name")
	if name == "" {
		name = "Passkey"
	}
	if err := auth.FinishPasskeyRegister(h.waForRequest(r), h.DB, userID, username, parsed, name); err != nil {
		log.Printf("passkey register finish: %v", err)
		jsonError(w, 500, err.Error())
		return
	}
	auth.WriteAudit(h.DB, "passkey_registered", username, realIP(r), name)
	jsonOK(w, map[string]string{"status": "ok"})
}

// --- Passkey Login ---

func (h *AuthHandler) PasskeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	assertion, err := auth.BeginPasskeyLogin(h.waForRequest(r), h.DB)
	if err != nil {
		jsonError(w, 500, "internal error")
		return
	}
	jsonOK(w, assertion)
}

func (h *AuthHandler) PasskeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	parsed, err := protocol.ParseCredentialRequestResponseBody(r.Body)
	if err != nil {
		jsonError(w, http.StatusBadRequest, "parse error")
		return
	}

	ip := realIP(r)
	userID, username, err := auth.FinishPasskeyLogin(h.waForRequest(r), h.DB, parsed)
	if err != nil {
		log.Printf("passkey login finish: %v", err)
		auth.WriteAudit(h.DB, "passkey_login_failure", "", ip, err.Error())
		jsonError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	csrfToken, _ := auth.NewCSRFToken()
	sessID, err := auth.CreateSession(h.DB, userID, auth.SessionData{CSRFToken: csrfToken})
	if err != nil {
		jsonError(w, 500, "session error")
		return
	}
	auth.WriteAudit(h.DB, "passkey_login_success", username, ip, "")
	auth.SetSessionCookie(w, sessID)
	jsonOK(w, map[string]string{"status": "ok", "redirect": "/"})
}

// --- OAuth2 token endpoint ---

func (h *AuthHandler) OAuth2Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request")
		return
	}
	grantType := r.FormValue("grant_type")
	if grantType != "client_credentials" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		jsonOK(w, map[string]string{"error": "unsupported_grant_type"})
		return
	}

	clientID := r.FormValue("client_id")
	secret := r.FormValue("client_secret")
	if clientID == "" || secret == "" {
		var ok bool
		clientID, secret, ok = r.BasicAuth()
		if !ok {
			auth.WriteAudit(h.DB, "token_rejected", "", realIP(r), "missing_credentials")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
			return
		}
	}

	ip := realIP(r)
	tokenStr, err := auth.IssueToken(h.DB, clientID, secret, h.JWTSec)
	if err != nil {
		auth.WriteAudit(h.DB, "token_rejected", clientID, ip, err.Error())
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
		return
	}

	auth.WriteAudit(h.DB, "token_issued", clientID, ip, "")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"access_token": tokenStr,
		"token_type":   "Bearer",
		"expires_in":   3600,
	})
}
