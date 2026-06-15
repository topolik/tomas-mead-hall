package handler

import (
	"database/sql"
	"net/http"
	"strings"

	"dsh/internal/auth"
)

type contextKey string

const (
	ctxUserID   contextKey = "userID"
	ctxUsername contextKey = "username"
	ctxCSRF     contextKey = "csrf"
	ctxAgent    contextKey = "agent"
)

// RequireSession redirects unauthenticated requests. When no passkeys are
// registered yet it sends to /setup; otherwise to /login.
func RequireSession(db *sql.DB, jwtSecret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, _, err := auth.SessionFromRequest(r, db)
		if err != nil {
			if !hasPasskeys(db) {
				http.Redirect(w, r, "/setup", http.StatusFound)
			} else {
				http.Redirect(w, r, "/login", http.StatusFound)
			}
			return
		}
		r = r.WithContext(withValue(r.Context(), ctxUserID, sess.UserID))
		next(w, r)
	}
}

// RequireJWT validates a Bearer token on API routes.
func RequireJWT(db *sql.DB, jwtSecret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		bearer := r.Header.Get("Authorization")
		if !strings.HasPrefix(bearer, "Bearer ") {
			jsonError(w, http.StatusUnauthorized, "missing token")
			return
		}
		clientID, err := auth.ValidateToken(db, strings.TrimPrefix(bearer, "Bearer "), jwtSecret)
		ip := realIP(r)
		detail := r.Method + " " + r.URL.Path
		if err != nil {
			go auth.WriteAudit(db, "api_call_failure", clientID, ip, detail+": "+err.Error())
			jsonError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		go auth.RecordClientUsage(db, clientID, ip)
		go auth.WriteAudit(db, "api_call", clientID, ip, detail)

		// Resolve the client's display name for authenticated authorship
		// (threads/messages). Falls back to the raw client id.
		agent := clientID
		var name string
		if db.QueryRow(`SELECT name FROM oauth2_clients WHERE client_id=?`, clientID).Scan(&name) == nil && name != "" {
			agent = name
		}
		r = r.WithContext(withValue(r.Context(), ctxAgent, agent))
		next(w, r)
	}
}

// agentFromRequest returns the authenticated OAuth client's display name,
// stashed in the context by RequireJWT.
func agentFromRequest(r *http.Request) string {
	if v, ok := r.Context().Value(ctxAgent).(string); ok {
		return v
	}
	return ""
}

// CheckCSRF validates the CSRF token on state-mutating requests.
func CheckCSRF(db *sql.DB, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, data, err := auth.SessionFromRequest(r, db)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		formToken := r.FormValue("_csrf")
		if formToken == "" {
			formToken = r.Header.Get("X-CSRF-Token")
		}
		if formToken != data.CSRFToken {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		r = r.WithContext(withValue(r.Context(), ctxUserID, sess.UserID))
		r = r.WithContext(withValue(r.Context(), ctxCSRF, data.CSRFToken))
		next(w, r)
	}
}

func hasPasskeys(db *sql.DB) bool {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM passkey_credentials`).Scan(&count)
	return count > 0
}
