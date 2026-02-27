package main

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/edouard/pureclaw/internal/vault"
)

// chdir changes the working directory to dir and returns a cleanup func.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(prev) })
}

// createTestVault creates a vault.enc in dir with the given passphrase and key-value pairs.
func createTestVault(t *testing.T, dir, passphrase string, entries map[string]string) {
	t.Helper()
	salt, err := vault.GenerateSalt()
	if err != nil {
		t.Fatalf("generate salt: %v", err)
	}
	key := vault.DeriveKey(passphrase, salt)
	path := dir + "/vault.enc"
	v, err := vault.Create(key, salt, path)
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	for k, val := range entries {
		if err := v.Set(k, val); err != nil {
			t.Fatalf("set %q: %v", k, err)
		}
	}
}

func TestRunVault_noArgs(t *testing.T) {
	var stderr bytes.Buffer
	code := runVault(nil, strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected usage message, got %q", stderr.String())
	}
}

func TestRunVault_unknownSubcommand(t *testing.T) {
	var stderr bytes.Buffer
	code := runVault([]string{"bogus"}, strings.NewReader(""), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "unknown subcommand") {
		t.Fatalf("expected unknown subcommand error, got %q", stderr.String())
	}
}

func TestVaultSet(t *testing.T) {
	t.Run("new vault auto-create", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stdout, stderr bytes.Buffer
		input := "test-passphrase\nmy-secret-value\n"
		code := runVault([]string{"set", "api_key"}, strings.NewReader(input), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "Secret stored: api_key") {
			t.Fatalf("expected confirmation, got %q", stderr.String())
		}
	})

	t.Run("existing vault", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", map[string]string{"existing": "val"})

		var stdout, stderr bytes.Buffer
		input := "pass123\nnew-value\n"
		code := runVault([]string{"set", "new_key"}, strings.NewReader(input), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
	})



	t.Run("missing key arg", func(t *testing.T) {
		var stderr bytes.Buffer
		code := runVault([]string{"set"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("empty stdin for passphrase", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"set", "key"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("empty stdin for value", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		// Only passphrase, no value line
		code := runVault([]string{"set", "key"}, strings.NewReader("pass\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
}

func TestVaultGet(t *testing.T) {
	t.Run("existing key", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", map[string]string{"api_key": "sk-secret"})

		var stdout, stderr bytes.Buffer
		code := runVault([]string{"get", "api_key"}, strings.NewReader("pass123\n"), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
		got := strings.TrimSpace(stdout.String())
		if got != "sk-secret" {
			t.Fatalf("got %q, want %q", got, "sk-secret")
		}
	})

	t.Run("nonexistent key", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", nil)

		var stderr bytes.Buffer
		code := runVault([]string{"get", "missing"}, strings.NewReader("pass123\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "not found") {
			t.Fatalf("expected not found error, got %q", stderr.String())
		}
	})

	t.Run("no vault file", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"get", "key"}, strings.NewReader("pass\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("wrong passphrase", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "correct-pass", map[string]string{"key": "val"})

		var stderr bytes.Buffer
		code := runVault([]string{"get", "key"}, strings.NewReader("wrong-pass\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "wrong passphrase") {
			t.Fatalf("expected wrong passphrase error, got %q", stderr.String())
		}
	})

	t.Run("missing key arg", func(t *testing.T) {
		var stderr bytes.Buffer
		code := runVault([]string{"get"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("empty stdin", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"get", "key"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
}

func TestVaultDelete(t *testing.T) {
	t.Run("existing key", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", map[string]string{"api_key": "val"})

		var stderr bytes.Buffer
		code := runVault([]string{"delete", "api_key"}, strings.NewReader("pass123\n"), io.Discard, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "Secret deleted: api_key") {
			t.Fatalf("expected confirmation, got %q", stderr.String())
		}
	})

	t.Run("nonexistent key", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", nil)

		var stderr bytes.Buffer
		code := runVault([]string{"delete", "missing"}, strings.NewReader("pass123\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
		if !strings.Contains(stderr.String(), "not found") {
			t.Fatalf("expected not found error, got %q", stderr.String())
		}
	})

	t.Run("no vault file", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"delete", "key"}, strings.NewReader("pass\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("missing key arg", func(t *testing.T) {
		var stderr bytes.Buffer
		code := runVault([]string{"delete"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("empty stdin", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"delete", "key"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
}

func TestVaultList(t *testing.T) {
	t.Run("multiple keys", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", map[string]string{
			"beta_key":    "val2",
			"alpha_key":   "val1",
			"charlie_key": "val3",
		})

		var stdout, stderr bytes.Buffer
		code := runVault([]string{"list"}, strings.NewReader("pass123\n"), &stdout, &stderr)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0; stderr: %s", code, stderr.String())
		}
		lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d: %q", len(lines), stdout.String())
		}
		// List returns sorted keys
		want := []string{"alpha_key", "beta_key", "charlie_key"}
		for i, w := range want {
			if lines[i] != w {
				t.Fatalf("line %d: got %q, want %q", i, lines[i], w)
			}
		}
	})

	t.Run("empty vault", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		createTestVault(t, dir, "pass123", nil)

		var stdout bytes.Buffer
		code := runVault([]string{"list"}, strings.NewReader("pass123\n"), &stdout, io.Discard)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
		if strings.TrimSpace(stdout.String()) != "" {
			t.Fatalf("expected empty output, got %q", stdout.String())
		}
	})

	t.Run("no vault file", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"list"}, strings.NewReader("pass\n"), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("extra args", func(t *testing.T) {
		var stderr bytes.Buffer
		code := runVault([]string{"list", "extra"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})

	t.Run("empty stdin", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)

		var stderr bytes.Buffer
		code := runVault([]string{"list"}, strings.NewReader(""), io.Discard, &stderr)
		if code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
}

func TestReadPassphrase(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var w bytes.Buffer
		scanner := newTestScanner("my-pass\n")
		got, err := readPassphrase(scanner, &w)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "my-pass" {
			t.Fatalf("got %q, want %q", got, "my-pass")
		}
		if !strings.Contains(w.String(), "Passphrase:") {
			t.Fatalf("expected prompt, got %q", w.String())
		}
	})

	t.Run("empty input", func(t *testing.T) {
		var w bytes.Buffer
		scanner := newTestScanner("")
		_, err := readPassphrase(scanner, &w)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestReadValue(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var w bytes.Buffer
		scanner := newTestScanner("my-value\n")
		got, err := readValue(scanner, &w)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "my-value" {
			t.Fatalf("got %q, want %q", got, "my-value")
		}
		if !strings.Contains(w.String(), "Value:") {
			t.Fatalf("expected prompt, got %q", w.String())
		}
	})

	t.Run("empty input", func(t *testing.T) {
		var w bytes.Buffer
		scanner := newTestScanner("")
		_, err := readValue(scanner, &w)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestOpenVault(t *testing.T) {
	t.Run("no vault file", func(t *testing.T) {
		dir := t.TempDir()
		_, err := openVault("pass", dir+"/vault.enc")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}

func TestCreateOrOpenVault(t *testing.T) {
	t.Run("create new", func(t *testing.T) {
		dir := t.TempDir()
		v, err := createOrOpenVault("pass", dir+"/vault.enc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v == nil {
			t.Fatal("expected vault, got nil")
		}
	})

	t.Run("open existing", func(t *testing.T) {
		dir := t.TempDir()
		createTestVault(t, dir, "pass123", nil)
		v, err := createOrOpenVault("pass123", dir+"/vault.enc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v == nil {
			t.Fatal("expected vault, got nil")
		}
	})
}

func TestVaultUserError(t *testing.T) {
	t.Run("decrypt error", func(t *testing.T) {
		err := vault.ErrDecrypt
		got := vaultUserError(err)
		if got != "wrong passphrase or corrupted vault" {
			t.Fatalf("got %q, want user-friendly message", got)
		}
	})

	t.Run("other error", func(t *testing.T) {
		err := os.ErrNotExist
		got := vaultUserError(err)
		if got != err.Error() {
			t.Fatalf("got %q, want %q", got, err.Error())
		}
	})
}

// errReader returns an error on Read.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

func TestReadPassphrase_scannerError(t *testing.T) {
	var w bytes.Buffer
	scanner := bufio.NewScanner(errReader{err: os.ErrPermission})
	_, err := readPassphrase(scanner, &w)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestReadValue_scannerError(t *testing.T) {
	var w bytes.Buffer
	scanner := bufio.NewScanner(errReader{err: os.ErrPermission})
	_, err := readValue(scanner, &w)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVaultSet_setError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	// Create vault, then make dir read-only so v.Set's save fails
	createTestVault(t, dir, "pass123", nil)
	// Make vault.enc read-only so atomic write cannot create temp file
	os.Chmod(dir, 0555)
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	var stderr bytes.Buffer
	code := runVault([]string{"set", "key"}, strings.NewReader("pass123\nvalue\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestVaultSet_createOrOpenError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	// Write invalid JSON to vault.enc — LoadSalt returns non-os.ErrNotExist error
	os.WriteFile(dir+"/vault.enc", []byte("not-json"), 0600)

	var stderr bytes.Buffer
	code := runVault([]string{"set", "key"}, strings.NewReader("pass\nvalue\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestVaultDelete_nonKeyError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	createTestVault(t, dir, "pass123", map[string]string{"key": "val"})
	// Make dir read-only so delete's save fails
	os.Chmod(dir, 0555)
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	var stderr bytes.Buffer
	code := runVault([]string{"delete", "key"}, strings.NewReader("pass123\n"), io.Discard, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestCreateOrOpenVault_generateSaltError(t *testing.T) {
	original := generateSalt
	defer func() { generateSalt = original }()
	generateSalt = func() ([]byte, error) {
		return nil, errors.New("salt generation failed")
	}

	dir := t.TempDir()
	// No vault.enc exists, so createOrOpenVault will try GenerateSalt
	_, err := createOrOpenVault("pass", dir+"/vault.enc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateOrOpenVault_corruptedVault(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/vault.enc"
	// Write invalid JSON — LoadSalt returns a non-os.ErrNotExist error
	os.WriteFile(path, []byte("{invalid}"), 0600)

	_, err := createOrOpenVault("pass", path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateOrOpenVault_createError(t *testing.T) {
	dir := t.TempDir()
	subdir := dir + "/readonly"
	os.Mkdir(subdir, 0555)
	t.Cleanup(func() { os.Chmod(subdir, 0755) })

	_, err := createOrOpenVault("pass", subdir+"/vault.enc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCreateOrOpenVault_openError(t *testing.T) {
	original := vaultOpen
	defer func() { vaultOpen = original }()
	vaultOpen = func(key []byte, path string) (*vault.Vault, error) {
		return nil, errors.New("open failed")
	}

	dir := t.TempDir()
	createTestVault(t, dir, "pass", nil)
	_, err := createOrOpenVault("pass", dir+"/vault.enc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOpenVault_openError(t *testing.T) {
	original := vaultOpen
	defer func() { vaultOpen = original }()
	vaultOpen = func(key []byte, path string) (*vault.Vault, error) {
		return nil, errors.New("open failed")
	}

	dir := t.TempDir()
	createTestVault(t, dir, "pass", nil)
	_, err := openVault("pass", dir+"/vault.enc")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func newTestScanner(input string) *bufio.Scanner {
	return bufio.NewScanner(strings.NewReader(input))
}
