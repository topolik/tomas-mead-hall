package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"dsh/internal/model"
)

const (
	SessionCookieName = "dsh_session"
	sessionDuration   = 24 * time.Hour
)

// SessionData holds extra in-session state.
type SessionData struct {
	CSRFToken string `json:"csrf_token,omitempty"`
}

func NewSessionID() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

func CreateSession(db *sql.DB, userID int64, data SessionData) (string, error) {
	id, err := NewSessionID()
	if err != nil {
		return "", err
	}
	dataJSON, _ := json.Marshal(data)
	_, err = db.Exec(
		`INSERT INTO sessions(id, user_id, data, expires_at) VALUES(?,?,?,?)`,
		id, userID, string(dataJSON), time.Now().Add(sessionDuration),
	)
	return id, err
}

func GetSession(db *sql.DB, id string) (*model.Session, *SessionData, error) {
	row := db.QueryRow(
		`SELECT id, user_id, data, created_at, expires_at FROM sessions WHERE id=? AND expires_at > datetime('now')`,
		id,
	)
	var s model.Session
	if err := row.Scan(&s.ID, &s.UserID, &s.Data, &s.CreatedAt, &s.ExpiresAt); err != nil {
		return nil, nil, err
	}
	var d SessionData
	_ = json.Unmarshal([]byte(s.Data), &d)
	return &s, &d, nil
}

func UpdateSessionData(db *sql.DB, sessionID string, data SessionData) error {
	dataJSON, _ := json.Marshal(data)
	_, err := db.Exec(`UPDATE sessions SET data=? WHERE id=?`, string(dataJSON), sessionID)
	return err
}

func DeleteSession(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE id=?`, id)
	return err
}

func SetSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionDuration.Seconds()),
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
}

func SessionFromRequest(r *http.Request, db *sql.DB) (*model.Session, *SessionData, error) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return nil, nil, err
	}
	return GetSession(db, c.Value)
}

// NewCSRFToken generates a random CSRF token.
func NewCSRFToken() (string, error) {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}
