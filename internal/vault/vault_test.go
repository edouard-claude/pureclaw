package vault

import (
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testKey derives a key for testing purposes.
func testKey() []byte {
	return DeriveKey("test-passphrase", []byte("1234567890123456"))
}

func TestCreate_newVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if v == nil {
		t.Fatal("expected non-nil vault")
	}

	// Verify file was written.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	var f vaultFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if f.Salt == "" {
		t.Fatal("expected non-empty salt in file")
	}
	if len(f.Entries) != 0 {
		t.Fatalf("expected empty entries, got %d", len(f.Entries))
	}

	// Verify file permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if info.Mode().Perm() != vaultFilePerm {
		t.Fatalf("permissions = %o, want %o", info.Mode().Perm(), vaultFilePerm)
	}
}

func TestCreate_saveError(t *testing.T) {
	orig := atomicWrite
	atomicWrite = func(string, []byte, os.FileMode) error {
		return errors.New("injected write error")
	}
	t.Cleanup(func() { atomicWrite = orig })

	_, err := Create(testKey(), []byte("1234567890123456"), "/tmp/test-vault.enc")
	if err == nil {
		t.Fatal("expected error when save fails")
	}
	if !strings.Contains(err.Error(), "create") {
		t.Fatalf("expected 'create' in error, got: %v", err)
	}
}

func TestOpen_existingVault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	// Create vault with an entry.
	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := v.Set("api_key", "secret-value-123"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Re-open and verify.
	v2, err := Open(key, path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	val, err := v2.Get("api_key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if val != "secret-value-123" {
		t.Fatalf("got %q, want %q", val, "secret-value-123")
	}
}

func TestOpen_nonExistentFile(t *testing.T) {
	_, err := Open(testKey(), "/nonexistent/path/vault.enc")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestOpen_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	if err := os.WriteFile(path, []byte("not-json{"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(testKey(), path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected 'unmarshal' in error, got: %v", err)
	}
}

func TestOpen_invalidSaltBase64(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	f := vaultFile{Salt: "!!!not-base64!!!", Entries: map[string]string{}}
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(testKey(), path)
	if err == nil {
		t.Fatal("expected error for invalid salt base64")
	}
	if !strings.Contains(err.Error(), "decode salt") {
		t.Fatalf("expected 'decode salt' in error, got: %v", err)
	}
}

func TestOpen_invalidEntryBase64(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	f := vaultFile{
		Salt:    base64.StdEncoding.EncodeToString([]byte("1234567890123456")),
		Entries: map[string]string{"key1": "!!!not-base64!!!"},
	}
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := Open(testKey(), path)
	if err == nil {
		t.Fatal("expected error for invalid entry base64")
	}
	if !strings.Contains(err.Error(), "decode entry") {
		t.Fatalf("expected 'decode entry' in error, got: %v", err)
	}
}

func TestLoadSalt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	_, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	loadedSalt, err := LoadSalt(path)
	if err != nil {
		t.Fatalf("LoadSalt failed: %v", err)
	}
	if string(loadedSalt) != string(salt) {
		t.Fatalf("got salt %q, want %q", loadedSalt, salt)
	}
}

func TestLoadSalt_nonExistentFile(t *testing.T) {
	_, err := LoadSalt("/nonexistent/vault.enc")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got: %v", err)
	}
}

func TestLoadSalt_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	if err := os.WriteFile(path, []byte("{invalid}"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSalt(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Fatalf("expected 'unmarshal' in error, got: %v", err)
	}
}

func TestLoadSalt_invalidBase64(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	f := vaultFile{Salt: "!!!not-base64!!!", Entries: map[string]string{}}
	data, _ := json.Marshal(f)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadSalt(path)
	if err == nil {
		t.Fatal("expected error for invalid salt base64")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("expected 'decode' in error, got: %v", err)
	}
}

func TestVault_SetAndGet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	tests := []struct {
		key   string
		value string
	}{
		{"api_key", "sk-1234567890"},
		{"token", "tok-abcdef"},
		{"empty_value", ""},
		{"unicode_key", "clé-secrète-🔐"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if err := v.Set(tt.key, tt.value); err != nil {
				t.Fatalf("Set(%q) failed: %v", tt.key, err)
			}
			got, err := v.Get(tt.key)
			if err != nil {
				t.Fatalf("Get(%q) failed: %v", tt.key, err)
			}
			if got != tt.value {
				t.Fatalf("Get(%q) = %q, want %q", tt.key, got, tt.value)
			}
		})
	}
}

func TestVault_SetOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := v.Set("key", "value1"); err != nil {
		t.Fatalf("Set 1 failed: %v", err)
	}
	if err := v.Set("key", "value2"); err != nil {
		t.Fatalf("Set 2 failed: %v", err)
	}

	got, err := v.Get("key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got != "value2" {
		t.Fatalf("got %q, want %q", got, "value2")
	}
}

func TestVault_Set_saveError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	orig := atomicWrite
	atomicWrite = func(string, []byte, os.FileMode) error {
		return errors.New("injected write error")
	}
	t.Cleanup(func() { atomicWrite = orig })

	if err := v.Set("key", "value"); err == nil {
		t.Fatal("expected error when save fails")
	}

	// Verify key was rolled back.
	if _, ok := v.entries["key"]; ok {
		t.Fatal("expected key to be rolled back after save failure")
	}
}

func TestVault_Set_saveErrorOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Set an initial value.
	if err := v.Set("key", "original"); err != nil {
		t.Fatalf("Set original failed: %v", err)
	}

	orig := atomicWrite
	atomicWrite = func(string, []byte, os.FileMode) error {
		return errors.New("injected write error")
	}
	t.Cleanup(func() { atomicWrite = orig })

	// Overwrite should fail.
	if err := v.Set("key", "updated"); err == nil {
		t.Fatal("expected error when save fails")
	}

	// Verify the original value was restored, not deleted.
	atomicWrite = orig
	got, err := v.Get("key")
	if err != nil {
		t.Fatalf("Get after rollback failed: %v", err)
	}
	if got != "original" {
		t.Fatalf("expected rollback to original value, got %q", got)
	}
}

func TestVault_Get_notFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	_, err = v.Get("nonexistent")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got: %v", err)
	}
}

func TestVault_Get_decryptError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Inject corrupt ciphertext directly.
	v.entries["bad_key"] = []byte("this-is-not-valid-ciphertext")

	_, err = v.Get("bad_key")
	if err == nil {
		t.Fatal("expected error for corrupt ciphertext")
	}
	if !errors.Is(err, ErrDecrypt) {
		t.Fatalf("expected ErrDecrypt, got: %v", err)
	}
}

func TestVault_Delete(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if err := v.Set("key1", "val1"); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("key2", "val2"); err != nil {
		t.Fatal(err)
	}

	if err := v.Delete("key1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = v.Get("key1")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound after delete, got: %v", err)
	}

	// key2 should still exist.
	got, err := v.Get("key2")
	if err != nil {
		t.Fatalf("Get key2 failed: %v", err)
	}
	if got != "val2" {
		t.Fatalf("key2 = %q, want %q", got, "val2")
	}
}

func TestVault_Delete_notFound(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = v.Delete("nonexistent")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got: %v", err)
	}
}

func TestVault_Delete_saveError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := v.Set("key", "value"); err != nil {
		t.Fatal(err)
	}

	orig := atomicWrite
	atomicWrite = func(string, []byte, os.FileMode) error {
		return errors.New("injected write error")
	}
	t.Cleanup(func() { atomicWrite = orig })

	if err := v.Delete("key"); err == nil {
		t.Fatal("expected error when save fails")
	}

	// Verify key was rolled back (still present).
	if _, ok := v.entries["key"]; !ok {
		t.Fatal("expected key to be restored after delete save failure")
	}
}

func TestVault_List(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Empty vault.
	keys := v.List()
	if len(keys) != 0 {
		t.Fatalf("expected empty list, got %v", keys)
	}

	// Add entries.
	for _, k := range []string{"zeta", "alpha", "mid"} {
		if err := v.Set(k, "val-"+k); err != nil {
			t.Fatal(err)
		}
	}

	keys = v.List()
	want := []string{"alpha", "mid", "zeta"}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys, want %d", len(keys), len(want))
	}
	for i, k := range keys {
		if k != want[i] {
			t.Fatalf("keys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestVault_persistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	// Create and populate.
	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if err := v.Set("api_key", "sk-123"); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("token", "tok-abc"); err != nil {
		t.Fatal(err)
	}

	// Re-open.
	v2, err := Open(key, path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Verify all entries.
	got, err := v2.Get("api_key")
	if err != nil {
		t.Fatalf("Get api_key failed: %v", err)
	}
	if got != "sk-123" {
		t.Fatalf("api_key = %q, want %q", got, "sk-123")
	}

	got, err = v2.Get("token")
	if err != nil {
		t.Fatalf("Get token failed: %v", err)
	}
	if got != "tok-abc" {
		t.Fatalf("token = %q, want %q", got, "tok-abc")
	}

	// Verify list.
	keys := v2.List()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestVault_Set_encryptError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Inject GCM error so Encrypt fails.
	orig := newGCMWithRandomNonce
	newGCMWithRandomNonce = func(cipher.Block) (cipher.AEAD, error) {
		return nil, errors.New("injected gcm error")
	}
	t.Cleanup(func() { newGCMWithRandomNonce = orig })

	err = v.Set("key", "value")
	if err == nil {
		t.Fatal("expected error when encrypt fails")
	}
	if !strings.Contains(err.Error(), "encrypt") {
		t.Fatalf("expected 'encrypt' in error, got: %v", err)
	}
}

func TestVault_save_marshalError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	orig := jsonMarshalIndent
	jsonMarshalIndent = func(any, string, string) ([]byte, error) {
		return nil, errors.New("injected marshal error")
	}
	t.Cleanup(func() { jsonMarshalIndent = orig })

	err = v.Set("key", "value")
	if err == nil {
		t.Fatal("expected error when marshal fails")
	}
	if !strings.Contains(err.Error(), "marshal") {
		t.Fatalf("expected 'marshal' in error, got: %v", err)
	}
}

func TestVault_deleteAndPersist(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.enc")
	salt := []byte("1234567890123456")
	key := DeriveKey("pass", salt)

	v, err := Create(key, salt, path)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Set("keep", "val1"); err != nil {
		t.Fatal(err)
	}
	if err := v.Set("remove", "val2"); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("remove"); err != nil {
		t.Fatal(err)
	}

	// Re-open and verify deletion persisted.
	v2, err := Open(key, path)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	_, err = v2.Get("remove")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatal("expected delete to persist after re-open")
	}
	got, err := v2.Get("keep")
	if err != nil {
		t.Fatal(err)
	}
	if got != "val1" {
		t.Fatalf("keep = %q, want %q", got, "val1")
	}
}
