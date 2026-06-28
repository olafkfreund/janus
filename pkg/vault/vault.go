package vault

import (
	"context"
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

// LocalVault implements a local file-based vault (ideal for air-gapped/local setups)
type LocalVault struct {
	mu       sync.RWMutex
	filePath string
	secrets  map[string]string
}

func NewLocalVault(filePath string) (*LocalVault, error) {
	lv := &LocalVault{
		filePath: filePath,
		secrets:  make(map[string]string),
	}
	if err := lv.load(); err != nil {
		// If file doesn't exist, create an empty one
		if os.IsNotExist(err) {
			return lv, lv.save()
		}
		return nil, err
	}
	return lv, nil
}

func (l *LocalVault) load() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.Open(l.filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}

	if len(data) == 0 {
		return nil
	}

	return json.Unmarshal(data, &l.secrets)
}

func (l *LocalVault) saveLocked() error {
	data, err := json.MarshalIndent(l.secrets, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(l.filePath, data, 0600)
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

// AWSVault represents AWS Secrets Manager
type AWSVault struct{}

func (a *AWSVault) GetSecret(ctx context.Context, secretName string) (string, error) {
	return "aws-secret-stub", nil
}

func (a *AWSVault) SetSecret(ctx context.Context, secretName string, secretValue string) error {
	return nil
}

func (a *AWSVault) ListSecrets(ctx context.Context) ([]string, error) {
	return []string{"aws/prod/db-password", "aws/prod/stripe-api-key"}, nil
}

func (a *AWSVault) DeleteSecret(ctx context.Context, secretName string) error {
	return nil
}

// GCPVault represents Google Cloud Secret Manager
type GCPVault struct{}

func (g *GCPVault) GetSecret(ctx context.Context, secretName string) (string, error) {
	return "gcp-secret-stub", nil
}

func (g *GCPVault) SetSecret(ctx context.Context, secretName string, secretValue string) error {
	return nil
}

func (g *GCPVault) ListSecrets(ctx context.Context) ([]string, error) {
	return []string{"projects/my-project/secrets/gateway-token", "projects/my-project/secrets/sales-api-key"}, nil
}

func (g *GCPVault) DeleteSecret(ctx context.Context, secretName string) error {
	return nil
}

// AzureVault represents Azure Key Vault
type AzureVault struct{}

func (az *AzureVault) GetSecret(ctx context.Context, secretName string) (string, error) {
	return "azure-secret-stub", nil
}

func (az *AzureVault) SetSecret(ctx context.Context, secretName string, secretValue string) error {
	return nil
}

func (az *AzureVault) ListSecrets(ctx context.Context) ([]string, error) {
	return []string{"azure-kv/billing-secret", "azure-kv/azure-cognitive-key"}, nil
}

func (az *AzureVault) DeleteSecret(ctx context.Context, secretName string) error {
	return nil
}

// InitVault initializes the vault based on config selection
func InitVault(provider, localPath string) (VaultProvider, error) {
	switch provider {
	case "aws":
		return &AWSVault{}, nil
	case "gcp":
		return &GCPVault{}, nil
	case "azure":
		return &AzureVault{}, nil
	case "local":
		return NewLocalVault(localPath)
	default:
		return nil, fmt.Errorf("unknown vault provider: %s", provider)
	}
}
