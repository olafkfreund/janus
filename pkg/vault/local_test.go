package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testEncKey = "test-vault-encryption-key-32bytes!!"

// TestLocalVault_RoundTrip verifies that a secret set on one LocalVault
// instance survives being persisted to disk and reloaded by a fresh
// instance using the same key.
func TestLocalVault_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	ctx := context.Background()

	lv, err := NewLocalVault(path, testEncKey)
	if err != nil {
		t.Fatalf("NewLocalVault: %v", err)
	}
	if err := lv.SetSecret(ctx, "db-password", "s3cr3t-value"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	// The on-disk file must not contain the plaintext secret.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected non-empty vault file")
	}
	if containsPlaintext(raw, "s3cr3t-value") {
		t.Fatal("vault file contains plaintext secret value; expected it to be encrypted")
	}
	// File mode must remain 0600.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("expected file mode 0600, got %o", perm)
	}

	// Reload with a fresh instance and the correct key.
	lv2, err := NewLocalVault(path, testEncKey)
	if err != nil {
		t.Fatalf("NewLocalVault (reload): %v", err)
	}
	got, err := lv2.GetSecret(ctx, "db-password")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "s3cr3t-value" {
		t.Fatalf("GetSecret = %q, want %q", got, "s3cr3t-value")
	}

	keys, err := lv2.ListSecrets(ctx)
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(keys) != 1 || keys[0] != "db-password" {
		t.Fatalf("ListSecrets = %v, want [db-password]", keys)
	}

	if err := lv2.DeleteSecret(ctx, "db-password"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if _, err := lv2.GetSecret(ctx, "db-password"); err == nil {
		t.Fatal("expected error after DeleteSecret, got nil")
	}
}

// TestLocalVault_WrongKeyFailsClosed verifies that opening an existing
// encrypted vault with the wrong key returns an error rather than silently
// yielding empty/garbage secrets.
func TestLocalVault_WrongKeyFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	ctx := context.Background()

	lv, err := NewLocalVault(path, testEncKey)
	if err != nil {
		t.Fatalf("NewLocalVault: %v", err)
	}
	if err := lv.SetSecret(ctx, "api-key", "top-secret"); err != nil {
		t.Fatalf("SetSecret: %v", err)
	}

	_, err = NewLocalVault(path, "a-completely-different-key-value")
	if err == nil {
		t.Fatal("expected error opening vault with wrong key, got nil")
	}
}

// TestLocalVault_MigratesLegacyPlaintext verifies that a pre-existing
// plaintext JSON vault file (as written by older versions of this package)
// is transparently loaded and then re-persisted encrypted, without losing
// any secrets.
func TestLocalVault_MigratesLegacyPlaintext(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	ctx := context.Background()

	legacy := map[string]string{
		"legacy-secret": "legacy-value",
		"another":       "value2",
	}
	data, err := json.MarshalIndent(legacy, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile (seed legacy plaintext): %v", err)
	}

	lv, err := NewLocalVault(path, testEncKey)
	if err != nil {
		t.Fatalf("NewLocalVault (migrate): %v", err)
	}

	// Secrets should be immediately readable post-migration.
	got, err := lv.GetSecret(ctx, "legacy-secret")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got != "legacy-value" {
		t.Fatalf("GetSecret = %q, want %q", got, "legacy-value")
	}

	// The file on disk must now be encrypted (carry the magic header and not
	// be parseable as plaintext JSON of the original secrets).
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(raw) < len(localVaultMagic) || string(raw[:len(localVaultMagic)]) != string(localVaultMagic) {
		t.Fatal("expected migrated vault file to start with encryption magic header")
	}
	if containsPlaintext(raw, "legacy-value") {
		t.Fatal("migrated vault file still contains plaintext secret value")
	}

	// A second load (post-migration) must also work and see the same data.
	lv2, err := NewLocalVault(path, testEncKey)
	if err != nil {
		t.Fatalf("NewLocalVault (post-migration reload): %v", err)
	}
	got2, err := lv2.GetSecret(ctx, "another")
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got2 != "value2" {
		t.Fatalf("GetSecret = %q, want %q", got2, "value2")
	}
}

// TestLocalVault_WeakKeyRejected mirrors PostgresVault's minimum key length
// enforcement.
func TestLocalVault_WeakKeyRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")

	if _, err := NewLocalVault(path, "short"); err == nil {
		t.Fatal("expected error for encryption key < 16 bytes, got nil")
	}
}

func containsPlaintext(data []byte, needle string) bool {
	return bytes.Contains(data, []byte(needle))
}
