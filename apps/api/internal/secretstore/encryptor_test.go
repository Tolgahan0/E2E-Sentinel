package secretstore

import "testing"

func validKey() []byte {
	return make([]byte, KeySize)
}

func TestNewEncryptor_RejectsWrongKeySize(t *testing.T) {
	if _, err := NewEncryptor([]byte("too-short")); err != ErrInvalidKeySize {
		t.Fatalf("NewEncryptor(short key) error = %v, want ErrInvalidKeySize", err)
	}
}

func TestEncryptor_EncryptDecryptRoundTrip(t *testing.T) {
	enc, err := NewEncryptor(validKey())
	if err != nil {
		t.Fatalf("NewEncryptor() error: %v", err)
	}

	ciphertext, nonce, err := enc.Encrypt("sk-super-secret-key")
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}
	if string(ciphertext) == "sk-super-secret-key" {
		t.Fatal("ciphertext must not equal plaintext")
	}

	got, err := enc.Decrypt(ciphertext, nonce)
	if err != nil {
		t.Fatalf("Decrypt() error: %v", err)
	}
	if got != "sk-super-secret-key" {
		t.Errorf("Decrypt() = %q, want %q", got, "sk-super-secret-key")
	}
}

func TestEncryptor_DecryptFailsWithWrongKey(t *testing.T) {
	enc1, _ := NewEncryptor(validKey())
	key2 := validKey()
	key2[0] = 1
	enc2, _ := NewEncryptor(key2)

	ciphertext, nonce, err := enc1.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}

	if _, err := enc2.Decrypt(ciphertext, nonce); err != ErrDecryptionFailed {
		t.Fatalf("Decrypt() with wrong key error = %v, want ErrDecryptionFailed", err)
	}
}

func TestEncryptor_DecryptFailsWithTamperedCiphertext(t *testing.T) {
	enc, _ := NewEncryptor(validKey())
	ciphertext, nonce, err := enc.Encrypt("secret")
	if err != nil {
		t.Fatalf("Encrypt() error: %v", err)
	}
	ciphertext[0] ^= 0xFF

	if _, err := enc.Decrypt(ciphertext, nonce); err != ErrDecryptionFailed {
		t.Fatalf("Decrypt() with tampered ciphertext error = %v, want ErrDecryptionFailed", err)
	}
}
