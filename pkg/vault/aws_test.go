package vault

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
)

// fakeSecretsAPI is an in-memory stand-in for AWS Secrets Manager, keyed by the
// full (prefixed) secret name, so AWSVault's CRUD logic — the upsert fallback,
// not-found mapping, and prefix filtering — can be tested without AWS.
type fakeSecretsAPI struct {
	secrets map[string]string
}

func newFakeSecretsAPI() *fakeSecretsAPI {
	return &fakeSecretsAPI{secrets: map[string]string{}}
}

func notFound() error {
	return &types.ResourceNotFoundException{Message: aws.String("not found")}
}

func (f *fakeSecretsAPI) GetSecretValue(_ context.Context, in *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	v, ok := f.secrets[aws.ToString(in.SecretId)]
	if !ok {
		return nil, notFound()
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(v)}, nil
}

func (f *fakeSecretsAPI) PutSecretValue(_ context.Context, in *secretsmanager.PutSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.PutSecretValueOutput, error) {
	id := aws.ToString(in.SecretId)
	if _, ok := f.secrets[id]; !ok {
		return nil, notFound() // force AWSVault to fall back to CreateSecret
	}
	f.secrets[id] = aws.ToString(in.SecretString)
	return &secretsmanager.PutSecretValueOutput{}, nil
}

func (f *fakeSecretsAPI) CreateSecret(_ context.Context, in *secretsmanager.CreateSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.CreateSecretOutput, error) {
	f.secrets[aws.ToString(in.Name)] = aws.ToString(in.SecretString)
	return &secretsmanager.CreateSecretOutput{}, nil
}

func (f *fakeSecretsAPI) DeleteSecret(_ context.Context, in *secretsmanager.DeleteSecretInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.DeleteSecretOutput, error) {
	id := aws.ToString(in.SecretId)
	if _, ok := f.secrets[id]; !ok {
		return nil, notFound()
	}
	delete(f.secrets, id)
	return &secretsmanager.DeleteSecretOutput{}, nil
}

func (f *fakeSecretsAPI) ListSecrets(_ context.Context, _ *secretsmanager.ListSecretsInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.ListSecretsOutput, error) {
	out := &secretsmanager.ListSecretsOutput{}
	for name := range f.secrets {
		n := name
		out.SecretList = append(out.SecretList, types.SecretListEntry{Name: aws.String(n)})
	}
	return out, nil
}

func TestAWSVault_CRUDWithPrefix(t *testing.T) {
	ctx := context.Background()
	fake := newFakeSecretsAPI()
	v := &AWSVault{client: fake, prefix: "janus/"}

	// Missing secret maps to a clean not-found error, not a raw AWS error.
	if _, err := v.GetSecret(ctx, "stripe-key"); err == nil {
		t.Fatal("GetSecret(missing): expected error, got nil")
	}

	// Create-on-first-write (Put -> not found -> CreateSecret fallback) and the
	// secret is stored under the prefixed name.
	if err := v.SetSecret(ctx, "stripe-key", "sk_live_1"); err != nil {
		t.Fatalf("SetSecret (create): %v", err)
	}
	if got := fake.secrets["janus/stripe-key"]; got != "sk_live_1" {
		t.Fatalf("stored under wrong key/value: janus/stripe-key = %q", got)
	}

	got, err := v.GetSecret(ctx, "stripe-key")
	if err != nil || got != "sk_live_1" {
		t.Fatalf("GetSecret = (%q, %v), want (sk_live_1, nil)", got, err)
	}

	// Update-in-place (Put succeeds, no duplicate create).
	if err := v.SetSecret(ctx, "stripe-key", "sk_live_2"); err != nil {
		t.Fatalf("SetSecret (update): %v", err)
	}
	if got, _ := v.GetSecret(ctx, "stripe-key"); got != "sk_live_2" {
		t.Fatalf("GetSecret after update = %q, want sk_live_2", got)
	}

	// List strips the prefix and hides non-gateway secrets.
	fake.secrets["someone-elses-secret"] = "x"
	keys, err := v.ListSecrets(ctx)
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if len(keys) != 1 || keys[0] != "stripe-key" {
		t.Fatalf("ListSecrets = %v, want [stripe-key] (prefix stripped, others hidden)", keys)
	}

	// Delete is idempotent.
	if err := v.DeleteSecret(ctx, "stripe-key"); err != nil {
		t.Fatalf("DeleteSecret: %v", err)
	}
	if err := v.DeleteSecret(ctx, "stripe-key"); err != nil {
		t.Fatalf("DeleteSecret (already gone) should be nil, got %v", err)
	}
}
