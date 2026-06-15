package auth

import (
	"database/sql"
	"os"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	f, err := os.CreateTemp("", "dsh_test_*.db")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })

	db, err := sql.Open("sqlite", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE oauth2_clients (
		client_id TEXT PRIMARY KEY,
		client_secret_hash TEXT NOT NULL,
		name TEXT NOT NULL,
		created_at DATETIME NOT NULL DEFAULT (datetime('now')),
		revoked_at DATETIME
	)`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestIssueValidateToken(t *testing.T) {
	db := newTestDB(t)
	const jwtSecret = "testsecret"

	clientID, plainSecret, err := CreateOAuth2Client(db, "test")
	if err != nil {
		t.Fatalf("CreateOAuth2Client: %v", err)
	}

	// Valid credentials
	tokenStr, err := IssueToken(db, clientID, plainSecret, jwtSecret)
	if err != nil {
		t.Fatalf("IssueToken: %v", err)
	}

	sub, err := ValidateToken(db, tokenStr, jwtSecret)
	if err != nil || sub != clientID {
		t.Errorf("ValidateToken: got sub=%q err=%v", sub, err)
	}

	// Wrong secret
	_, err = IssueToken(db, clientID, "wrong", jwtSecret)
	if err == nil {
		t.Error("IssueToken with wrong secret should fail")
	}

	// Wrong JWT secret
	_, err = ValidateToken(db, tokenStr, "wrongsecret")
	if err == nil {
		t.Error("ValidateToken with wrong secret should fail")
	}

	// Revoke and try again
	if err := RevokeOAuth2Client(db, clientID); err != nil {
		t.Fatalf("RevokeOAuth2Client: %v", err)
	}
	_, err = IssueToken(db, clientID, plainSecret, jwtSecret)
	if err == nil {
		t.Error("IssueToken on revoked client should fail")
	}
}
