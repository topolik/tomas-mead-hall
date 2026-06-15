package auth

import (
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptTOTPSecret(t *testing.T) {
	key := hex.EncodeToString(make([]byte, 32)) // 64 zero hex chars
	secret := "JBSWY3DPEHPK3PXP"

	enc, err := EncryptTOTPSecret(secret, key)
	if err != nil {
		t.Fatalf("EncryptTOTPSecret: %v", err)
	}
	if enc == secret {
		t.Error("encrypted form should differ from plaintext")
	}

	dec, err := DecryptTOTPSecret(enc, key)
	if err != nil {
		t.Fatalf("DecryptTOTPSecret: %v", err)
	}
	if dec != secret {
		t.Errorf("round-trip failed: got %q want %q", dec, secret)
	}
}

func TestEncryptTOTPBadKey(t *testing.T) {
	_, err := EncryptTOTPSecret("secret", "notahexkey")
	if err == nil {
		t.Error("bad key should return error")
	}
}
