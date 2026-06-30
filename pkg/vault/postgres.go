package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"fmt"
	"io"
)

// PostgresVault stores secrets encrypted (AES-256-GCM) in the shared Postgres
// database. Unlike the local file vault, it is consistent across all replicas
// and survives pod restarts — the correct choice for a horizontally-scaled
// deployment. Values are never stored in plaintext.
type PostgresVault struct {
	db  *sql.DB
	gcm cipher.AEAD
}

// NewPostgresVault initialises the vault table and the AES-GCM cipher. The
// encryption key is derived (SHA-256) from encKey, so any sufficiently strong
// secret works; rotating it makes existing ciphertext undecryptable.
func NewPostgresVault(db *sql.DB, encKey string) (*PostgresVault, error) {
	if db == nil {
		return nil, fmt.Errorf("postgres vault requires a database handle")
	}
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

	v := &PostgresVault{db: db, gcm: gcm}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS vault_secrets (
			name TEXT PRIMARY KEY,
			ciphertext TEXT NOT NULL,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`); err != nil {
		return nil, fmt.Errorf("failed to init vault schema: %w", err)
	}
	return v, nil
}

func (v *PostgresVault) encrypt(plaintext string) (string, error) {
	nonce := make([]byte, v.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := v.gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (v *PostgresVault) decrypt(encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	ns := v.gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext too short")
	}
	plain, err := v.gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed (wrong key?): %w", err)
	}
	return string(plain), nil
}

func (v *PostgresVault) GetSecret(ctx context.Context, name string) (string, error) {
	var ct string
	err := v.db.QueryRowContext(ctx, "SELECT ciphertext FROM vault_secrets WHERE name = $1", name).Scan(&ct)
	if err == sql.ErrNoRows {
		return "", fmt.Errorf("secret %s not found in vault", name)
	}
	if err != nil {
		return "", err
	}
	return v.decrypt(ct)
}

func (v *PostgresVault) SetSecret(ctx context.Context, name, value string) error {
	ct, err := v.encrypt(value)
	if err != nil {
		return err
	}
	_, err = v.db.ExecContext(ctx, `
		INSERT INTO vault_secrets (name, ciphertext, updated_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (name) DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = CURRENT_TIMESTAMP`,
		name, ct)
	return err
}

func (v *PostgresVault) ListSecrets(ctx context.Context) ([]string, error) {
	rows, err := v.db.QueryContext(ctx, "SELECT name FROM vault_secrets ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (v *PostgresVault) DeleteSecret(ctx context.Context, name string) error {
	_, err := v.db.ExecContext(ctx, "DELETE FROM vault_secrets WHERE name = $1", name)
	return err
}
