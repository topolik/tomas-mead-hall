package handler

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"strings"
)

func navBadges(db *sql.DB) (notifCount, planCount, threadCount int) {
	db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE dismissed_at IS NULL`).Scan(&notifCount)
	db.QueryRow(`SELECT COUNT(*) FROM plans WHERE status='pending'`).Scan(&planCount)
	db.QueryRow(`SELECT COUNT(*) FROM threads WHERE status='open'`).Scan(&threadCount)
	return
}

// realIP returns the client's IP, preferring X-Real-IP / X-Forwarded-For
// when set by a trusted proxy, falling back to RemoteAddr.
func realIP(r *http.Request) string {
	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return strings.TrimSpace(ip)
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonOK(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
