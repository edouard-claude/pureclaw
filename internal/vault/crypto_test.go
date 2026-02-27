package vault

import (
	"bytes"
	"crypto/cipher"
	"errors"
	"strings"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	tests := []struct {
		name       string
		passphrase string
		salt       []byte
	}{
		{
			name:       "basic derivation",
			passphrase: "my-secret-passphrase",
			salt:       []byte("1234567890123456"),
		},
		{
			name:       "empty passphrase",
			passphrase: "",
			salt:       []byte("1234567890123456"),
		},
		{
			name:       "unicode passphrase",
			passphrase: "pässwörd-ünïcödé-🔐",
			salt:       []byte("abcdefghijklmnop"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key := DeriveKey(tt.passphrase, tt.salt)
			if len(key) != KeySize {
				t.Fatalf("key length = %d, want %d", len(key), KeySize)
			}
		})
	}
}

func TestDeriveKey_deterministic(t *testing.T) {
	salt := []byte("1234567890123456")
	key1 := DeriveKey("passphrase", salt)
	key2 := DeriveKey("passphrase", salt)
	if !bytes.Equal(key1, key2) {
		t.Fatal("same passphrase+salt must produce same key")
	}
}

func TestDeriveKey_differentSaltsProduceDifferentKeys(t *testing.T) {
	key1 := DeriveKey("passphrase", []byte("salt-aaaaaaaaaa01"))
	key2 := DeriveKey("passphrase", []byte("salt-bbbbbbbbbb02"))
	if bytes.Equal(key1, key2) {
		t.Fatal("different salts must produce different keys")
	}
}

func TestDeriveKey_differentPassphrasesProduceDifferentKeys(t *testing.T) {
	salt := []byte("1234567890123456")
	key1 := DeriveKey("passphrase-a", salt)
	key2 := DeriveKey("passphrase-b", salt)
	if bytes.Equal(key1, key2) {
		t.Fatal("different passphrases must produce different keys")
	}
}

func TestEncryptDecrypt_roundTrip(t *testing.T) {
	tests := []struct {
		name      string
		plaintext string
	}{
		{"short text", "hello"},
		{"empty string", ""},
		{"long text", "this is a much longer piece of text that tests encryption of larger payloads with AES-256-GCM"},
		{"binary-like", "\x00\x01\x02\xff\xfe\xfd"},
		{"unicode", "clé secrète 🔑 ключ"},
	}
	key := DeriveKey("test-passphrase", []byte("1234567890123456"))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ciphertext, err := Encrypt(key, []byte(tt.plaintext))
			if err != nil {
				t.Fatalf("Encrypt failed: %v", err)
			}
			plaintext, err := Decrypt(key, ciphertext)
			if err != nil {
				t.Fatalf("Decrypt failed: %v", err)
			}
			if string(plaintext) != tt.plaintext {
				t.Fatalf("got %q, want %q", plaintext, tt.plaintext)
			}
		})
	}
}

func TestEncrypt_producesUniqueOutput(t *testing.T) {
	key := DeriveKey("passphrase", []byte("1234567890123456"))
	plaintext := []byte("same-input")

	ct1, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt 1 failed: %v", err)
	}
	ct2, err := Encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("Encrypt 2 failed: %v", err)
	}
	if bytes.Equal(ct1, ct2) {
		t.Fatal("two encryptions of same plaintext should produce different ciphertexts (random nonce)")
	}
}

func TestDecrypt_wrongKey(t *testing.T) {
	key1 := DeriveKey("correct-passphrase", []byte("1234567890123456"))
	key2 := DeriveKey("wrong-passphrase", []byte("1234567890123456"))

	ciphertext, err := Encrypt(key1, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	_, err = Decrypt(key2, ciphertext)
	if err == nil {
		t.Fatal("Decrypt with wrong key should fail")
	}
}

func TestDecrypt_tamperedCiphertext(t *testing.T) {
	key := DeriveKey("passphrase", []byte("1234567890123456"))
	ciphertext, err := Encrypt(key, []byte("secret"))
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}

	// Tamper with the ciphertext.
	tampered := make([]byte, len(ciphertext))
	copy(tampered, ciphertext)
	tampered[len(tampered)-1] ^= 0xff

	_, err = Decrypt(key, tampered)
	if err == nil {
		t.Fatal("Decrypt with tampered ciphertext should fail")
	}
}

func TestDecrypt_shortCiphertext(t *testing.T) {
	key := DeriveKey("passphrase", []byte("1234567890123456"))
	_, err := Decrypt(key, []byte("short"))
	if err == nil {
		t.Fatal("Decrypt with short ciphertext should fail")
	}
}

func TestEncrypt_invalidKeySize(t *testing.T) {
	_, err := Encrypt([]byte("short-key"), []byte("plaintext"))
	if err == nil {
		t.Fatal("Encrypt with invalid key size should fail")
	}
}

func TestDecrypt_invalidKeySize(t *testing.T) {
	_, err := Decrypt([]byte("short-key"), []byte("some-ciphertext-data-here-12345678"))
	if err == nil {
		t.Fatal("Decrypt with invalid key size should fail")
	}
}

func TestGenerateSalt(t *testing.T) {
	salt, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt failed: %v", err)
	}
	if len(salt) != SaltSize {
		t.Fatalf("salt length = %d, want %d", len(salt), SaltSize)
	}
}

func TestGenerateSalt_uniqueness(t *testing.T) {
	salt1, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt 1 failed: %v", err)
	}
	salt2, err := GenerateSalt()
	if err != nil {
		t.Fatalf("GenerateSalt 2 failed: %v", err)
	}
	if bytes.Equal(salt1, salt2) {
		t.Fatal("two generated salts should be unique")
	}
}

func TestEncrypt_gcmError(t *testing.T) {
	key := DeriveKey("passphrase", []byte("1234567890123456"))

	orig := newGCMWithRandomNonce
	newGCMWithRandomNonce = func(cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("injected gcm error")
	}
	t.Cleanup(func() { newGCMWithRandomNonce = orig })

	_, err := Encrypt(key, []byte("plaintext"))
	if err == nil {
		t.Fatal("expected error when newGCMWithRandomNonce fails")
	}
	if !strings.Contains(err.Error(), "new gcm") {
		t.Fatalf("expected 'new gcm' in error, got: %v", err)
	}
}

func TestDecrypt_gcmError(t *testing.T) {
	key := DeriveKey("passphrase", []byte("1234567890123456"))

	orig := newGCMWithRandomNonce
	newGCMWithRandomNonce = func(cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("injected gcm error")
	}
	t.Cleanup(func() { newGCMWithRandomNonce = orig })

	_, err := Decrypt(key, []byte("some-ciphertext-data-here-12345678"))
	if err == nil {
		t.Fatal("expected error when newGCMWithRandomNonce fails")
	}
	if !strings.Contains(err.Error(), "new gcm") {
		t.Fatalf("expected 'new gcm' in error, got: %v", err)
	}
}

func TestGenerateSalt_randReadError(t *testing.T) {
	orig := randRead
	randRead = func([]byte) (int, error) {
		return 0, errors.New("injected rand error")
	}
	t.Cleanup(func() { randRead = orig })

	_, err := GenerateSalt()
	if err == nil {
		t.Fatal("expected error when randRead fails")
	}
}
