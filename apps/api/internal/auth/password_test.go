package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword() error: %v", err)
	}
	if hash == "correct-horse-battery-staple" {
		t.Fatal("hash must not equal the plaintext")
	}
	if err := VerifyPassword(hash, "correct-horse-battery-staple"); err != nil {
		t.Errorf("VerifyPassword() error: %v", err)
	}
	if err := VerifyPassword(hash, "wrong-password-entirely"); err != ErrInvalidCredentials {
		t.Errorf("VerifyPassword(wrong) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestHashPassword_RejectsShortPasswords(t *testing.T) {
	if _, err := HashPassword("short"); err != ErrPasswordTooShort {
		t.Fatalf("HashPassword(short) error = %v, want ErrPasswordTooShort", err)
	}
}

func TestGenerateToken_ProducesUniqueUnrelatedTokensAndHashes(t *testing.T) {
	token1, hash1, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}
	token2, hash2, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken() error: %v", err)
	}
	if token1 == token2 {
		t.Fatal("two generated tokens must not be equal")
	}
	if hash1 == hash2 {
		t.Fatal("two generated hashes must not be equal")
	}
	if HashToken(token1) != hash1 {
		t.Error("HashToken(token1) must reproduce the same hash GenerateToken returned")
	}
}
