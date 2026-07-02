package vault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// VaultProvider defines the interface for interacting with various secrets managers
type VaultProvider interface {
	GetSecret(ctx context.Context, secretName string) (string, error)
	SetSecret(ctx context.Context, secretName string, secretValue string) error
	ListSecrets(ctx context.Context) ([]string, error)
	DeleteSecret(ctx context.Context, secretName string) error
}

// localVaultMagic prefixes the on-disk file when its contents are AES-256-GCM
// encrypted. It is not valid JSON (JSON documents can't start with these raw
// bytes followed by binary data), so its presence unambiguously distinguishes
// an encrypted file from a legacy plaintext-JSON file written by older
// versions of this package.
var localVaultMagic = []byte("MCPVAULT1")

// LocalVault implements a local file-based vault (ideal for air-gapped/local
// setups). Contents are encrypted at rest with AES-256-GCM using the same
// key-derivation approach as PostgresVault (SHA-256 of encKey, minimum 16
// bytes), so a single VAULT_ENCRYPTION_KEY (falling back to JWT_SECRET) works
// across both providers.
type LocalVault struct {
	mu       sync.RWMutex
	filePath string
	secrets  map[string]string
	gcm      cipher.AEAD
}

// NewLocalVault opens (or creates) the local vault file at filePath, encrypting
// its contents with a key derived from encKey. If a pre-existing file is found
// to be legacy plaintext JSON (no localVaultMagic header), it is loaded as-is
// and immediately re-written encrypted — a one-time, transparent migration
// that never loses secrets.
func NewLocalVault(filePath string, encKey string) (*LocalVault, error) {
	if len(encKey) < 16 {
		return nil, fmt.Errorf("vault encryption key too weak (need >= 16 bytes)")
	}
	key := sha256.Sum256([]byte(encKey))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to init GCM: %w", err)
	}

	lv := &LocalVault{
		filePath: filePath,
		secrets:  make(map[string]string),
		gcm:      gcm,
	}
	migrated, err := lv.load()
	if err != nil {
		// If file doesn't exist, create an empty encrypted one.
		if os.IsNotExist(err) {
			return lv, lv.save()
		}
		return nil, err
	}
	if migrated {
		// Legacy plaintext file was loaded; persist it encrypted so future
		// loads take the encrypted path. Never silently drop secrets if this
		// fails.
		if err := lv.save(); err != nil {
			return nil, fmt.Errorf("failed to migrate legacy plaintext vault to encrypted: %w", err)
		}
	}
	return lv, nil
}

// load reads the vault file and populates l.secrets. It returns (true, nil)
// when the file was legacy plaintext JSON and needs to be re-persisted
// encrypted by the caller.
func (l *LocalVault) load() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.Open(l.filePath)
	if err != nil {
		return false, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return false, err
	}

	if len(data) == 0 {
		return false, nil
	}

	if bytes.HasPrefix(data, localVaultMagic) {
		plain, err := l.decrypt(data[len(localVaultMagic):])
		if err != nil {
			return false, fmt.Errorf("failed to decrypt local vault (wrong VAULT_ENCRYPTION_KEY?): %w", err)
		}
		return false, json.Unmarshal(plain, &l.secrets)
	}

	// No encryption header: treat as legacy plaintext JSON and flag for
	// migration.
	if err := json.Unmarshal(data, &l.secrets); err != nil {
		return false, fmt.Errorf("local vault file is neither a valid encrypted vault nor legacy plaintext JSON: %w", err)
	}
	return true, nil
}

func (l *LocalVault) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, l.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return l.gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func (l *LocalVault) decrypt(sealed []byte) ([]byte, error) {
	ns := l.gcm.NonceSize()
	if len(sealed) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	plain, err := l.gcm.Open(nil, sealed[:ns], sealed[ns:], nil)
	if err != nil {
		return nil, fmt.Errorf("decryption failed (wrong key?): %w", err)
	}
	return plain, nil
}

func (l *LocalVault) saveLocked() error {
	data, err := json.MarshalIndent(l.secrets, "", "  ")
	if err != nil {
		return err
	}
	sealed, err := l.encrypt(data)
	if err != nil {
		return err
	}
	out := make([]byte, 0, len(localVaultMagic)+len(sealed))
	out = append(out, localVaultMagic...)
	out = append(out, sealed...)
	return os.WriteFile(l.filePath, out, 0600)
}

func (l *LocalVault) save() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.saveLocked()
}

func (l *LocalVault) GetSecret(ctx context.Context, secretName string) (string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	val, ok := l.secrets[secretName]
	if !ok {
		return "", fmt.Errorf("secret %s not found in local vault", secretName)
	}
	return val, nil
}

func (l *LocalVault) SetSecret(ctx context.Context, secretName string, secretValue string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.secrets[secretName] = secretValue
	return l.saveLocked()
}

func (l *LocalVault) ListSecrets(ctx context.Context) ([]string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	keys := make([]string, 0, len(l.secrets))
	for k := range l.secrets {
		keys = append(keys, k)
	}
	return keys, nil
}

func (l *LocalVault) DeleteSecret(ctx context.Context, secretName string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.secrets, secretName)
	return l.saveLocked()
}

// errNotImplemented is returned by cloud providers that have not yet been wired
// to a real secrets backend. Failing loudly prevents silently injecting bogus
// credentials (which the previous stubs did) into downstream requests.
var errNotImplemented = fmt.Errorf("vault provider not implemented")

// InitVault initializes the vault based on config selection.
//
//   - local:    file-based, single-node/dev only (per-pod, not shared)
//   - postgres: encrypted secrets in the shared DB — correct for multi-replica
//   - aws/gcp/azure: not yet implemented (fails closed rather than returning fakes)
//
// db is used only by the postgres provider; encKey is used by both local
// (file encryption) and postgres.
func InitVault(provider, localPath string, db *sql.DB, encKey string) (VaultProvider, error) {
	switch provider {
	case "local":
		return NewLocalVault(localPath, encKey)
	case "postgres":
		return NewPostgresVault(db, encKey)
	case "aws", "gcp", "azure":
		return nil, fmt.Errorf("%w: %q (use 'local'/'postgres' or implement this provider)", errNotImplemented, provider)
	default:
		return nil, fmt.Errorf("unknown vault provider: %s", provider)
	}
}
