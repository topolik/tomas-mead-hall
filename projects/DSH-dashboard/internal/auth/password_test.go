package auth

import (
	"testing"
)

func TestHashVerifyPassword(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	ok, err := VerifyPassword("hunter2", hash)
	if err != nil || !ok {
		t.Errorf("VerifyPassword correct password: got ok=%v err=%v", ok, err)
	}

	ok, err = VerifyPassword("wrong", hash)
	if err != nil || ok {
		t.Errorf("VerifyPassword wrong password: got ok=%v err=%v", ok, err)
	}
}

func TestHashPasswordDifferentSalts(t *testing.T) {
	h1, _ := HashPassword("same")
	h2, _ := HashPassword("same")
	if h1 == h2 {
		t.Error("two hashes of same password should differ (different salts)")
	}
}
