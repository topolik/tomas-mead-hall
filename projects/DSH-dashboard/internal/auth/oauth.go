package auth

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const tokenDuration = time.Hour

// GenerateClientCredentials creates a new OAuth2 client_id + secret pair.
func GenerateClientCredentials() (clientID, secret string, err error) {
	idBytes := make([]byte, 16)
	secBytes := make([]byte, 32)
	if _, err = rand.Read(idBytes); err != nil {
		return
	}
	if _, err = rand.Read(secBytes); err != nil {
		return
	}
	clientID = "dsh_" + hex.EncodeToString(idBytes)
	secret = hex.EncodeToString(secBytes)
	return
}

// CreateOAuth2Client hashes the secret and inserts the client into the DB.
func CreateOAuth2Client(db *sql.DB, name string) (clientID, plainSecret string, err error) {
	clientID, plainSecret, err = GenerateClientCredentials()
	if err != nil {
		return
	}
	hash, err := HashPassword(plainSecret)
	if err != nil {
		return
	}
	_, err = db.Exec(
		`INSERT INTO oauth2_clients(client_id, client_secret_hash, name) VALUES(?,?,?)`,
		clientID, hash, name,
	)
	return
}

// IssueToken validates client credentials and issues a JWT.
func IssueToken(db *sql.DB, clientID, secret, jwtSecret string) (string, error) {
	var hash string
	var revokedAt sql.NullString
	err := db.QueryRow(
		`SELECT client_secret_hash, revoked_at FROM oauth2_clients WHERE client_id=?`, clientID,
	).Scan(&hash, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("invalid_client")
	}
	if err != nil {
		return "", err
	}
	if revokedAt.Valid {
		return "", errors.New("invalid_client")
	}
	ok, err := VerifyPassword(secret, hash)
	if err != nil || !ok {
		return "", errors.New("invalid_client")
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"iss": "dsh",
		"sub": clientID,
		"iat": now.Unix(),
		"exp": now.Add(tokenDuration).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(jwtSecret))
}

// ValidateToken parses and validates a Bearer JWT, returning the client_id.
// It also checks that the client has not been revoked in the DB.
func ValidateToken(db *sql.DB, tokenStr, jwtSecret string) (string, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(jwtSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return "", err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return "", errors.New("invalid token")
	}
	clientID, _ := claims["sub"].(string)

	var revokedAt sql.NullString
	err = db.QueryRow(
		`SELECT revoked_at FROM oauth2_clients WHERE client_id=?`, clientID,
	).Scan(&revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errors.New("unknown client")
	}
	if err != nil {
		return "", err
	}
	if revokedAt.Valid {
		return clientID, errors.New("client revoked")
	}
	return clientID, nil
}

// RecordClientUsage updates last_used_at/ip on the client record.
func RecordClientUsage(db *sql.DB, clientID, remoteIP string) {
	db.Exec(
		`UPDATE oauth2_clients SET last_used_at=datetime('now'), last_used_ip=? WHERE client_id=?`,
		remoteIP, clientID,
	)
}

// RevokeOAuth2Client marks a client as revoked.
func RevokeOAuth2Client(db *sql.DB, clientID string) error {
	res, err := db.Exec(
		`UPDATE oauth2_clients SET revoked_at=datetime('now') WHERE client_id=? AND revoked_at IS NULL`,
		clientID,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("client not found or already revoked")
	}
	return nil
}
