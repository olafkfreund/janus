package vault

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

// AWSVault stores each downstream credential as its own secret in AWS Secrets
// Manager. Unlike the local/postgres providers it does NOT encrypt values in
// this process — Secrets Manager encrypts at rest with a KMS key and enforces
// access via IAM. Credentials are resolved server-side and never returned to
// the LLM client.
//
// Authentication uses the SDK's default credential chain, so in EKS it picks up
// the pod's IRSA web-identity role automatically — no static keys. The IRSA
// role must grant secretsmanager:GetSecretValue (and, for portal/CLI writes,
// CreateSecret/PutSecretValue/DeleteSecret/ListSecrets) on the secret ARNs the
// gateway manages (see deployment/secrets.tf).
type AWSVault struct {
	client awsSecretsAPI
	// prefix namespaces gateway-managed secrets (e.g. "janus/") so they are
	// isolated from other secrets in the account and ListSecrets can filter to
	// just ours. Empty means no namespacing.
	prefix string
}

// awsSecretsAPI is the subset of the Secrets Manager client the vault uses.
// Declared as an interface so the provider can be unit-tested with a fake.
type awsSecretsAPI interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
	PutSecretValue(context.Context, *secretsmanager.PutSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error)
	CreateSecret(context.Context, *secretsmanager.CreateSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error)
	DeleteSecret(context.Context, *secretsmanager.DeleteSecretInput, ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error)
	ListSecrets(context.Context, *secretsmanager.ListSecretsInput, ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error)
}

// compile-time proof AWSVault satisfies the provider contract.
var _ VaultProvider = (*AWSVault)(nil)

// NewAWSVault builds a provider backed by AWS Secrets Manager. prefix (may be
// empty) namespaces the gateway's secret names. It loads AWS config from the
// ambient environment (IRSA in EKS, shared config locally); region comes from
// AWS_REGION / the pod's config as usual.
func NewAWSVault(ctx context.Context, prefix string) (*AWSVault, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("aws vault: load AWS config: %w", err)
	}
	return &AWSVault{client: secretsmanager.NewFromConfig(cfg), prefix: prefix}, nil
}

// id maps a caller-facing secret name to its full Secrets Manager name.
func (v *AWSVault) id(name string) string { return v.prefix + name }

func (v *AWSVault) GetSecret(ctx context.Context, name string) (string, error) {
	out, err := v.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
		SecretId: aws.String(v.id(name)),
	})
	if err != nil {
		var nf *types.ResourceNotFoundException
		if errors.As(err, &nf) {
			return "", fmt.Errorf("secret %s not found in vault", name)
		}
		return "", fmt.Errorf("aws vault: get %q: %w", name, err)
	}
	if out.SecretString != nil {
		return *out.SecretString, nil
	}
	if out.SecretBinary != nil {
		return string(out.SecretBinary), nil
	}
	return "", fmt.Errorf("secret %s has no value", name)
}

// SetSecret creates the secret if absent, otherwise stages a new version. AWS
// has no single upsert call, so we PutSecretValue and fall back to CreateSecret
// only on ResourceNotFoundException.
func (v *AWSVault) SetSecret(ctx context.Context, name, value string) error {
	id := v.id(name)
	_, err := v.client.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
		SecretId:     aws.String(id),
		SecretString: aws.String(value),
	})
	if err == nil {
		return nil
	}
	var nf *types.ResourceNotFoundException
	if errors.As(err, &nf) {
		if _, cerr := v.client.CreateSecret(ctx, &secretsmanager.CreateSecretInput{
			Name:         aws.String(id),
			SecretString: aws.String(value),
		}); cerr != nil {
			return fmt.Errorf("aws vault: create %q: %w", name, cerr)
		}
		return nil
	}
	return fmt.Errorf("aws vault: put %q: %w", name, err)
}

func (v *AWSVault) DeleteSecret(ctx context.Context, name string) error {
	_, err := v.client.DeleteSecret(ctx, &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(v.id(name)),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	})
	if err != nil {
		var nf *types.ResourceNotFoundException
		if errors.As(err, &nf) {
			return nil // already gone — deletion is idempotent
		}
		return fmt.Errorf("aws vault: delete %q: %w", name, err)
	}
	return nil
}

// ListSecrets returns the caller-facing names (prefix stripped) of gateway
// secrets. When a prefix is set, only matching secrets are returned so we never
// leak unrelated account secrets into the portal's vault view.
func (v *AWSVault) ListSecrets(ctx context.Context) ([]string, error) {
	var names []string
	p := secretsmanager.NewListSecretsPaginator(v.client, &secretsmanager.ListSecretsInput{})
	for p.HasMorePages() {
		page, err := p.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("aws vault: list: %w", err)
		}
		for _, s := range page.SecretList {
			if s.Name == nil {
				continue
			}
			n := *s.Name
			if v.prefix != "" {
				if !strings.HasPrefix(n, v.prefix) {
					continue
				}
				n = strings.TrimPrefix(n, v.prefix)
			}
			names = append(names, n)
		}
	}
	return names, nil
}
