package auth

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// credFlagByte encodes BackupEligible/BackupState into a single byte for storage.
func credFlagByte(f webauthn.CredentialFlags) byte {
	return byte(f.ProtocolValue())
}

// credFlagsFromByte restores CredentialFlags from the stored byte.
func credFlagsFromByte(b byte) webauthn.CredentialFlags {
	return webauthn.NewCredentialFlags(protocol.AuthenticatorFlags(b))
}

// NewWebAuthn creates a configured WebAuthn instance.
func NewWebAuthn(origin string) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPDisplayName: "DSH Dashboard",
		RPID:          rpIDFromOrigin(origin),
		RPOrigins:     []string{origin},
	})
}

// NewWebAuthnMap creates a WebAuthn instance per unique RPID from a list of origins.
func NewWebAuthnMap(origins []string) (map[string]*webauthn.WebAuthn, error) {
	m := make(map[string]*webauthn.WebAuthn)
	for _, o := range origins {
		rpid := rpIDFromOrigin(o)
		if _, exists := m[rpid]; exists {
			continue
		}
		wa, err := NewWebAuthn(o)
		if err != nil {
			return nil, fmt.Errorf("webauthn for %s: %w", o, err)
		}
		m[rpid] = wa
	}
	return m, nil
}

// RPIDFromOrigin is the exported version for use by handlers.
func RPIDFromOrigin(origin string) string { return rpIDFromOrigin(origin) }

func rpIDFromOrigin(origin string) string {
	// strip scheme and port: http://localhost:8080 → localhost
	s := origin
	for _, prefix := range []string{"https://", "http://"} {
		s = trimPrefix(s, prefix)
	}
	if i := indexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	return s
}

func trimPrefix(s, prefix string) string {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

// webauthnUser implements webauthn.User for a DSH user.
type webauthnUser struct {
	id          int64
	username    string
	credentials []webauthn.Credential
}

func (u *webauthnUser) WebAuthnID() []byte                         { return []byte(fmt.Sprintf("%d", u.id)) }
func (u *webauthnUser) WebAuthnName() string                       { return u.username }
func (u *webauthnUser) WebAuthnDisplayName() string                { return u.username }
func (u *webauthnUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

// LoadWebAuthnUser builds a webauthnUser with all stored credentials.
func LoadWebAuthnUser(db *sql.DB, userID int64, username string) (*webauthnUser, error) {
	rows, err := db.Query(
		`SELECT credential_id, public_key, sign_count, flags FROM passkey_credentials WHERE user_id=?`, userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []webauthn.Credential
	for rows.Next() {
		var credIDStr string
		var pubKey []byte
		var signCount uint32
		var flagByte byte
		if err := rows.Scan(&credIDStr, &pubKey, &signCount, &flagByte); err != nil {
			return nil, err
		}
		credID, _ := base64.StdEncoding.DecodeString(credIDStr)
		creds = append(creds, webauthn.Credential{
			ID:              credID,
			PublicKey:       pubKey,
			AttestationType: "none",
			Flags:           credFlagsFromByte(flagByte),
			Authenticator: webauthn.Authenticator{
				SignCount: signCount,
			},
		})
	}
	return &webauthnUser{id: userID, username: username, credentials: creds}, nil
}

// StoreWebAuthnSession persists the WebAuthn session data.
func StoreWebAuthnSession(db *sql.DB, sessionKey string, userID *int64, data *webauthn.SessionData) error {
	b, _ := json.Marshal(data)
	var uid interface{}
	if userID != nil {
		uid = *userID
	}
	_, err := db.Exec(
		`INSERT OR REPLACE INTO webauthn_sessions(id, user_id, data, expires_at) VALUES(?,?,?,?)`,
		sessionKey, uid, string(b), time.Now().Add(5*time.Minute),
	)
	return err
}

// LoadWebAuthnSession retrieves and deletes the WebAuthn session data.
func LoadWebAuthnSession(db *sql.DB, sessionKey string) (*webauthn.SessionData, error) {
	var dataStr string
	var expiresAt time.Time
	err := db.QueryRow(
		`SELECT data, expires_at FROM webauthn_sessions WHERE id=?`, sessionKey,
	).Scan(&dataStr, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errors.New("webauthn session not found")
	}
	if err != nil {
		return nil, err
	}
	if time.Now().After(expiresAt) {
		return nil, errors.New("webauthn session expired")
	}
	_, _ = db.Exec(`DELETE FROM webauthn_sessions WHERE id=?`, sessionKey)
	var sd webauthn.SessionData
	if err := json.Unmarshal([]byte(dataStr), &sd); err != nil {
		return nil, err
	}
	return &sd, nil
}

// SavePasskeyCredential stores a new credential after successful registration.
func SavePasskeyCredential(db *sql.DB, userID int64, cred *webauthn.Credential, name string) error {
	credIDStr := base64.StdEncoding.EncodeToString(cred.ID)
	_, err := db.Exec(
		`INSERT INTO passkey_credentials(user_id, credential_id, public_key, sign_count, flags, name) VALUES(?,?,?,?,?,?)`,
		userID, credIDStr, cred.PublicKey, cred.Authenticator.SignCount, credFlagByte(cred.Flags), name,
	)
	return err
}

// FindUserByPasskey looks up a user by credential ID after authentication.
func FindUserByPasskey(db *sql.DB, rawID []byte) (int64, string, error) {
	credIDStr := base64.StdEncoding.EncodeToString(rawID)
	var userID int64
	var username string
	err := db.QueryRow(
		`SELECT u.id, u.username FROM users u
		 JOIN passkey_credentials pc ON pc.user_id=u.id
		 WHERE pc.credential_id=?`, credIDStr,
	).Scan(&userID, &username)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", errors.New("credential not found")
	}
	return userID, username, err
}

// UpdatePasskeySignCount updates the sign count after successful authentication.
func UpdatePasskeySignCount(db *sql.DB, rawID []byte, signCount uint32) error {
	credIDStr := base64.StdEncoding.EncodeToString(rawID)
	_, err := db.Exec(
		`UPDATE passkey_credentials SET sign_count=? WHERE credential_id=?`, signCount, credIDStr,
	)
	return err
}

// BeginPasskeyRegister starts the WebAuthn registration ceremony.
func BeginPasskeyRegister(wa *webauthn.WebAuthn, db *sql.DB, userID int64, username string) (
	*protocol.CredentialCreation, error,
) {
	user, err := LoadWebAuthnUser(db, userID, username)
	if err != nil {
		return nil, err
	}
	creation, sessionData, err := wa.BeginRegistration(user)
	if err != nil {
		return nil, err
	}
	if err := StoreWebAuthnSession(db, "reg_"+fmt.Sprintf("%d", userID), &userID, sessionData); err != nil {
		return nil, err
	}
	return creation, nil
}

// FinishPasskeyRegister completes the WebAuthn registration ceremony.
func FinishPasskeyRegister(wa *webauthn.WebAuthn, db *sql.DB, userID int64, username string,
	response *protocol.ParsedCredentialCreationData, name string,
) error {
	user, err := LoadWebAuthnUser(db, userID, username)
	if err != nil {
		return err
	}
	sessionData, err := LoadWebAuthnSession(db, "reg_"+fmt.Sprintf("%d", userID))
	if err != nil {
		return err
	}
	cred, err := wa.CreateCredential(user, *sessionData, response)
	if err != nil {
		return err
	}
	return SavePasskeyCredential(db, userID, cred, name)
}

// BeginPasskeyLogin starts the WebAuthn authentication ceremony (usernameless).
func BeginPasskeyLogin(wa *webauthn.WebAuthn, db *sql.DB) (*protocol.CredentialAssertion, error) {
	assertion, sessionData, err := wa.BeginDiscoverableLogin()
	if err != nil {
		return nil, err
	}
	if err := StoreWebAuthnSession(db, "login_anon", nil, sessionData); err != nil {
		return nil, err
	}
	return assertion, nil
}

// FinishPasskeyLogin completes the WebAuthn authentication ceremony.
// Returns the authenticated user ID and username.
func FinishPasskeyLogin(wa *webauthn.WebAuthn, db *sql.DB,
	response *protocol.ParsedCredentialAssertionData,
) (int64, string, error) {
	sessionData, err := LoadWebAuthnSession(db, "login_anon")
	if err != nil {
		return 0, "", err
	}

	var foundUserID int64
	var foundUsername string

	handler := func(rawID, userHandle []byte) (webauthn.User, error) {
		uid, uname, err := FindUserByPasskey(db, rawID)
		if err != nil {
			return nil, err
		}
		foundUserID = uid
		foundUsername = uname
		return LoadWebAuthnUser(db, uid, uname)
	}

	cred, err := wa.ValidateDiscoverableLogin(handler, *sessionData, response)
	if err != nil {
		return 0, "", err
	}
	_ = UpdatePasskeySignCount(db, cred.ID, cred.Authenticator.SignCount)
	return foundUserID, foundUsername, nil
}
