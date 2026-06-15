package auth

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"

	"github.com/pquerna/otp/totp"
	"github.com/skip2/go-qrcode"
)

// TOTPEnrollment holds the secret and QR PNG for display during setup.
type TOTPEnrollment struct {
	Secret    string
	QRDataURL string // base64 PNG data URL
}

// BeginTOTPEnroll generates a new TOTP secret for the given username.
func BeginTOTPEnroll(username, issuer string) (*TOTPEnrollment, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: username,
	})
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	img, err := key.Image(200, 200)
	if err != nil {
		return nil, err
	}
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	_ = qrcode.New // imported for side effect
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	return &TOTPEnrollment{
		Secret:    key.Secret(),
		QRDataURL: dataURL,
	}, nil
}

// VerifyTOTPCode checks a TOTP code against a secret.
func VerifyTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}

// EncryptTOTPSecret encrypts a TOTP secret with AES-GCM using the provided key hex.
func EncryptTOTPSecret(secret, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("TOTP key must be 32-byte hex")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptTOTPSecret decrypts a TOTP secret encrypted by EncryptTOTPSecret.
func DecryptTOTPSecret(encoded, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		return "", errors.New("TOTP key must be 32-byte hex")
	}
	ct, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(ct) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	plain, err := gcm.Open(nil, ct[:gcm.NonceSize()], ct[gcm.NonceSize():], nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// GetTOTPSecret returns the verified TOTP secret for a user, or empty string if none.
func GetTOTPSecret(db *sql.DB, userID int64, keyHex string) (string, error) {
	var encSecret string
	err := db.QueryRow(
		`SELECT secret FROM totp_credentials WHERE user_id=? AND verified=1 LIMIT 1`, userID,
	).Scan(&encSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return DecryptTOTPSecret(encSecret, keyHex)
}

// RemoveTOTPCredential deletes all TOTP credentials for a user.
func RemoveTOTPCredential(db *sql.DB, userID int64) error {
	_, err := db.Exec(`DELETE FROM totp_credentials WHERE user_id=?`, userID)
	return err
}

// SaveTOTPCredential stores an encrypted, verified TOTP secret for a user.
func SaveTOTPCredential(db *sql.DB, userID int64, secret, keyHex string) error {
	enc, err := EncryptTOTPSecret(secret, keyHex)
	if err != nil {
		return fmt.Errorf("encrypt TOTP: %w", err)
	}
	_, err = db.Exec(
		`INSERT INTO totp_credentials(user_id, secret, verified) VALUES(?,?,1)
		 ON CONFLICT DO NOTHING`,
		userID, enc,
	)
	return err
}
