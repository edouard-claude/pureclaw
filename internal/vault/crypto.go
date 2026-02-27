package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"

	"golang.org/x/crypto/pbkdf2"
)

// Replaceable for testing error paths.
var (
	randRead              = func(b []byte) (int, error) { return rand.Read(b) }
	newGCMWithRandomNonce = func(block cipher.Block) (cipher.AEAD, error) { return cipher.NewGCMWithRandomNonce(block) }
)

const (
	// SaltSize is the number of random bytes used for PBKDF2 salt.
	SaltSize = 16

	// PBKDF2Iterations is the OWASP 2023 recommendation for SHA-256.
	PBKDF2Iterations = 600_000

	// KeySize is the AES-256 key length in bytes.
	KeySize = 32
)

// DeriveKey derives a 32-byte AES-256 key from a passphrase and salt
// using PBKDF2-SHA256 with 600,000 iterations.
func DeriveKey(passphrase string, salt []byte) []byte {
	return pbkdf2.Key([]byte(passphrase), salt, PBKDF2Iterations, KeySize, sha256.New)
}

// Encrypt encrypts plaintext using AES-256-GCM with a random nonce.
// The returned ciphertext includes the auto-generated nonce prefix and GCM tag.
func Encrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault encrypt: new cipher: %w", err)
	}
	gcm, err := newGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("vault encrypt: new gcm: %w", err)
	}
	return gcm.Seal(nil, nil, plaintext, nil), nil
}

// Decrypt decrypts ciphertext produced by Encrypt using AES-256-GCM.
func Decrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("vault decrypt: new cipher: %w", err)
	}
	gcm, err := newGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("vault decrypt: new gcm: %w", err)
	}
	plaintext, err := gcm.Open(nil, nil, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("vault decrypt: open: %w", err)
	}
	return plaintext, nil
}

// GenerateSalt generates a cryptographically secure random salt.
func GenerateSalt() ([]byte, error) {
	salt := make([]byte, SaltSize)
	if _, err := randRead(salt); err != nil {
		return nil, fmt.Errorf("vault: generate salt: %w", err)
	}
	return salt, nil
}
