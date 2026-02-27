package vault

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"

	"github.com/edouard/pureclaw/internal/platform"
)

// Sentinel errors.
var (
	ErrKeyNotFound = errors.New("vault: key not found")
	ErrDecrypt     = errors.New("vault: decryption failed")
)

// vaultFilePerm is the file permission for vault files (owner read/write only).
const vaultFilePerm = 0600

// Replaceable for testing error paths.
var (
	atomicWrite      = platform.AtomicWrite
	jsonMarshalIndent = func(v any, prefix, indent string) ([]byte, error) { return json.MarshalIndent(v, prefix, indent) }
)

// vaultFile is the on-disk JSON representation of the vault.
type vaultFile struct {
	Salt    string            `json:"salt"`
	Entries map[string]string `json:"entries"`
}

// Vault holds encrypted secrets in memory and persists them to disk.
type Vault struct {
	key     []byte
	path    string
	salt    []byte
	entries map[string][]byte // key name → encrypted value
}

// LoadSalt reads just the salt from an existing vault file.
// Returns os.ErrNotExist if the vault file doesn't exist.
func LoadSalt(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f vaultFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("vault: load salt: unmarshal: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(f.Salt)
	if err != nil {
		return nil, fmt.Errorf("vault: load salt: decode: %w", err)
	}
	return salt, nil
}

// Create creates a new empty vault file with the given salt and derived key.
// Returns the Vault ready for Get/Set/Delete/List operations.
func Create(derivedKey []byte, salt []byte, path string) (*Vault, error) {
	v := &Vault{
		key:     derivedKey,
		path:    path,
		salt:    salt,
		entries: make(map[string][]byte),
	}
	if err := v.save(); err != nil {
		return nil, fmt.Errorf("vault: create: %w", err)
	}
	slog.Info("vault created", "component", "vault", "operation", "create", "path", path)
	return v, nil
}

// Open loads an existing vault file using the provided derived key.
func Open(derivedKey []byte, path string) (*Vault, error) {
	v := &Vault{
		key:     derivedKey,
		path:    path,
		entries: make(map[string][]byte),
	}
	if err := v.load(); err != nil {
		return nil, err
	}
	slog.Info("vault loaded", "component", "vault", "operation", "open", "path", path, "entries", len(v.entries))
	return v, nil
}

// Get decrypts and returns the value for the given key.
func (v *Vault) Get(key string) (string, error) {
	ciphertext, ok := v.entries[key]
	if !ok {
		return "", ErrKeyNotFound
	}
	plaintext, err := Decrypt(v.key, ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrDecrypt, err)
	}
	return string(plaintext), nil
}

// Set encrypts the value and stores it under the given key, then saves atomically.
func (v *Vault) Set(key string, value string) error {
	ciphertext, err := Encrypt(v.key, []byte(value))
	if err != nil {
		return fmt.Errorf("vault: set: encrypt: %w", err)
	}
	prev, existed := v.entries[key]
	v.entries[key] = ciphertext
	if err := v.save(); err != nil {
		// Rollback in-memory state on save failure.
		if existed {
			v.entries[key] = prev
		} else {
			delete(v.entries, key)
		}
		return fmt.Errorf("vault: set: %w", err)
	}
	slog.Info("secret stored", "component", "vault", "operation", "set", "key", key)
	return nil
}

// Delete removes the key from the vault and saves atomically.
func (v *Vault) Delete(key string) error {
	ciphertext, ok := v.entries[key]
	if !ok {
		return ErrKeyNotFound
	}
	delete(v.entries, key)
	if err := v.save(); err != nil {
		// Rollback in-memory state on save failure.
		v.entries[key] = ciphertext
		return fmt.Errorf("vault: delete: %w", err)
	}
	slog.Info("secret deleted", "component", "vault", "operation", "delete", "key", key)
	return nil
}

// List returns sorted key names from the vault. No decryption is performed.
func (v *Vault) List() []string {
	keys := make([]string, 0, len(v.entries))
	for k := range v.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// save serializes the vault to JSON and writes it atomically.
func (v *Vault) save() error {
	f := vaultFile{
		Salt:    base64.StdEncoding.EncodeToString(v.salt),
		Entries: make(map[string]string, len(v.entries)),
	}
	for k, ct := range v.entries {
		f.Entries[k] = base64.StdEncoding.EncodeToString(ct)
	}
	data, err := jsonMarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("vault: save: marshal: %w", err)
	}
	return atomicWrite(v.path, data, vaultFilePerm)
}

// load reads the vault file from disk and parses all entries.
func (v *Vault) load() error {
	data, err := os.ReadFile(v.path)
	if err != nil {
		return fmt.Errorf("vault: open: read: %w", err)
	}
	var f vaultFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("vault: open: unmarshal: %w", err)
	}
	salt, err := base64.StdEncoding.DecodeString(f.Salt)
	if err != nil {
		return fmt.Errorf("vault: open: decode salt: %w", err)
	}
	v.salt = salt
	for k, encoded := range f.Entries {
		ct, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return fmt.Errorf("vault: open: decode entry %q: %w", k, err)
		}
		v.entries[k] = ct
	}
	return nil
}
