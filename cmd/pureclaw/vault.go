package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/edouard/pureclaw/internal/vault"
)

const defaultVaultPath = "vault.enc"

// Replaceable for testing error paths.
var (
	generateSalt = vault.GenerateSalt
	vaultOpen    = vault.Open
)

// runVault dispatches vault subcommands: get, set, delete, list.
func runVault(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printVaultUsage(stderr)
		return 1
	}

	scanner := bufio.NewScanner(stdin)

	switch args[0] {
	case "set":
		return vaultSet(args[1:], scanner, stdout, stderr)
	case "get":
		return vaultGet(args[1:], scanner, stdout, stderr)
	case "delete":
		return vaultDelete(args[1:], scanner, stdout, stderr)
	case "list":
		return vaultList(args[1:], scanner, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "vault: unknown subcommand %q\n", args[0])
		printVaultUsage(stderr)
		return 1
	}
}

func vaultSet(args []string, scanner *bufio.Scanner, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: pureclaw vault set <key>")
		return 1
	}
	key := args[0]

	passphrase, err := readPassphrase(scanner, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	value, err := readValue(scanner, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	v, err := createOrOpenVault(passphrase, defaultVaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", vaultUserError(err))
		return 1
	}

	if err := v.Set(key, value); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	slog.Info("secret stored", "component", "vault-cli", "operation", "set", "key", key)
	fmt.Fprintf(stderr, "Secret stored: %s\n", key)
	return 0
}

func vaultGet(args []string, scanner *bufio.Scanner, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: pureclaw vault get <key>")
		return 1
	}
	key := args[0]

	passphrase, err := readPassphrase(scanner, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	v, err := openVault(passphrase, defaultVaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", vaultUserError(err))
		return 1
	}

	value, err := v.Get(key)
	if err != nil {
		if errors.Is(err, vault.ErrKeyNotFound) {
			fmt.Fprintf(stderr, "Error: key %q not found\n", key)
		} else {
			fmt.Fprintf(stderr, "Error: %s\n", vaultUserError(err))
		}
		return 1
	}
	slog.Info("secret retrieved", "component", "vault-cli", "operation", "get", "key", key)
	fmt.Fprintln(stdout, value)
	return 0
}

func vaultDelete(args []string, scanner *bufio.Scanner, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "Usage: pureclaw vault delete <key>")
		return 1
	}
	key := args[0]

	passphrase, err := readPassphrase(scanner, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	v, err := openVault(passphrase, defaultVaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", vaultUserError(err))
		return 1
	}

	if err := v.Delete(key); err != nil {
		if errors.Is(err, vault.ErrKeyNotFound) {
			fmt.Fprintf(stderr, "Error: key %q not found\n", key)
		} else {
			fmt.Fprintf(stderr, "Error: %v\n", err)
		}
		return 1
	}
	slog.Info("secret deleted", "component", "vault-cli", "operation", "delete", "key", key)
	fmt.Fprintf(stderr, "Secret deleted: %s\n", key)
	return 0
}

func vaultList(args []string, scanner *bufio.Scanner, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "Usage: pureclaw vault list")
		return 1
	}

	passphrase, err := readPassphrase(scanner, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}

	v, err := openVault(passphrase, defaultVaultPath)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %s\n", vaultUserError(err))
		return 1
	}

	keys := v.List()
	for _, k := range keys {
		fmt.Fprintln(stdout, k)
	}
	slog.Info("vault listed", "component", "vault-cli", "operation", "list", "count", len(keys))
	return 0
}

// readPassphrase prompts on w and reads a line from the scanner.
func readPassphrase(scanner *bufio.Scanner, w io.Writer) (string, error) {
	fmt.Fprint(w, "Passphrase: ")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading passphrase: %w", err)
		}
		return "", fmt.Errorf("reading passphrase: unexpected end of input")
	}
	return strings.TrimRight(scanner.Text(), "\r\n"), nil
}

// readValue prompts on w and reads a line from the scanner.
func readValue(scanner *bufio.Scanner, w io.Writer) (string, error) {
	fmt.Fprint(w, "Value: ")
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return "", fmt.Errorf("reading value: %w", err)
		}
		return "", fmt.Errorf("reading value: unexpected end of input")
	}
	return strings.TrimRight(scanner.Text(), "\r\n"), nil
}

// openVault loads an existing vault. Returns an error if vault.enc doesn't exist.
func openVault(passphrase, path string) (*vault.Vault, error) {
	salt, err := vault.LoadSalt(path)
	if err != nil {
		return nil, fmt.Errorf("vault not found at %s (run 'pureclaw init' or 'pureclaw vault set' first): %w", path, err)
	}
	key := vault.DeriveKey(passphrase, salt)
	v, err := vaultOpen(key, path)
	if err != nil {
		return nil, fmt.Errorf("vault: open: %w", err)
	}
	return v, nil
}

// createOrOpenVault opens an existing vault or creates a new one if it doesn't exist.
func createOrOpenVault(passphrase, path string) (*vault.Vault, error) {
	salt, err := vault.LoadSalt(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			newSalt, err := generateSalt()
			if err != nil {
				return nil, fmt.Errorf("vault: generate salt: %w", err)
			}
			key := vault.DeriveKey(passphrase, newSalt)
			v, err := vault.Create(key, newSalt, path)
			if err != nil {
				return nil, fmt.Errorf("vault: create: %w", err)
			}
			return v, nil
		}
		return nil, fmt.Errorf("vault: load salt: %w", err)
	}
	key := vault.DeriveKey(passphrase, salt)
	v, err := vaultOpen(key, path)
	if err != nil {
		return nil, fmt.Errorf("vault: open: %w", err)
	}
	return v, nil
}

// vaultUserError returns a user-friendly error message.
func vaultUserError(err error) string {
	if errors.Is(err, vault.ErrDecrypt) {
		return "wrong passphrase or corrupted vault"
	}
	return err.Error()
}

func printVaultUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: pureclaw vault <subcommand>")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Subcommands:")
	fmt.Fprintln(w, "  set <key>     Store a secret")
	fmt.Fprintln(w, "  get <key>     Retrieve a secret")
	fmt.Fprintln(w, "  delete <key>  Delete a secret")
	fmt.Fprintln(w, "  list          List all secret keys")
}
