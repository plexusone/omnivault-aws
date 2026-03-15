// Package secretsmanager provides a OmniVault provider for AWS Secrets Manager.
//
// AWS Secrets Manager is designed for storing and rotating secrets like:
//   - Database credentials
//   - API keys
//   - OAuth tokens
//   - TLS certificates
//
// Features:
//   - Automatic credential rotation
//   - Fine-grained IAM access control
//   - Encryption with AWS KMS
//   - Versioning and staging labels
//
// Usage:
//
//	provider, err := secretsmanager.New(secretsmanager.Config{
//	    Region: "us-east-1",
//	})
//	secret, err := provider.Get(ctx, "prod/database/credentials")
package secretsmanager

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager/types"
	"github.com/plexusone/omnivault/vault"
)

// Config holds configuration for the Secrets Manager provider.
type Config struct {
	// Region is the AWS region (e.g., "us-east-1").
	Region string

	// Profile is the AWS credentials profile name.
	Profile string

	// EndpointURL is a custom endpoint URL (for LocalStack, testing).
	EndpointURL string

	// AWSConfig is an optional pre-configured AWS SDK config.
	AWSConfig *aws.Config

	// JSONParse attempts to parse secret values as JSON.
	// When true, JSON object keys become Fields in the Secret.
	// Default: true
	JSONParse *bool

	// VersionStage is the staging label for secret versions.
	// Default: "AWSCURRENT"
	VersionStage string
}

// Provider implements vault.Vault for AWS Secrets Manager.
type Provider struct {
	client      *secretsmanager.Client
	config      Config
	mu          sync.RWMutex
	closed      bool
	endpointURL string
}

// New creates a new Secrets Manager provider.
func New(cfg Config) (*Provider, error) {
	ctx := context.Background()
	return NewWithContext(ctx, cfg)
}

// NewWithContext creates a new Secrets Manager provider with context.
func NewWithContext(ctx context.Context, cfg Config) (*Provider, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, vault.NewVaultError("New", "", "aws-sm", err)
	}

	var opts []func(*secretsmanager.Options)
	if cfg.EndpointURL != "" {
		opts = append(opts, func(o *secretsmanager.Options) {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		})
	}

	client := secretsmanager.NewFromConfig(awsCfg, opts...)

	// Default JSONParse to true
	if cfg.JSONParse == nil {
		t := true
		cfg.JSONParse = &t
	}

	if cfg.VersionStage == "" {
		cfg.VersionStage = "AWSCURRENT"
	}

	return &Provider{
		client:      client,
		config:      cfg,
		endpointURL: cfg.EndpointURL,
	}, nil
}

func loadAWSConfig(ctx context.Context, cfg Config) (aws.Config, error) {
	if cfg.AWSConfig != nil {
		return *cfg.AWSConfig, nil
	}

	var opts []func(*config.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, config.WithRegion(cfg.Region))
	}

	if cfg.Profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(cfg.Profile))
	}

	return config.LoadDefaultConfig(ctx, opts...)
}

// Get retrieves a secret from AWS Secrets Manager.
func (p *Provider) Get(ctx context.Context, path string) (*vault.Secret, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("Get", path, p.Name(), vault.ErrClosed)
	}

	input := &secretsmanager.GetSecretValueInput{
		SecretId:     aws.String(path),
		VersionStage: aws.String(p.config.VersionStage),
	}

	result, err := p.client.GetSecretValue(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return nil, vault.NewVaultError("Get", path, p.Name(), vault.ErrSecretNotFound)
		}
		if isAccessDeniedError(err) {
			return nil, vault.NewVaultError("Get", path, p.Name(), vault.ErrAccessDenied)
		}
		return nil, vault.NewVaultError("Get", path, p.Name(), err)
	}

	secret := &vault.Secret{
		Metadata: vault.Metadata{
			Provider: p.Name(),
			Path:     path,
		},
	}

	// Handle binary vs string secrets
	if result.SecretBinary != nil {
		secret.ValueBytes = result.SecretBinary
		secret.Value = string(result.SecretBinary)
	} else if result.SecretString != nil {
		secret.Value = *result.SecretString

		// Try to parse as JSON
		if p.config.JSONParse != nil && *p.config.JSONParse {
			var jsonData map[string]any
			if err := json.Unmarshal([]byte(*result.SecretString), &jsonData); err == nil {
				secret.Fields = make(map[string]string)
				for k, v := range jsonData {
					switch val := v.(type) {
					case string:
						secret.Fields[k] = val
					case float64:
						secret.Fields[k] = strconv.FormatFloat(val, 'f', -1, 64)
					default:
						if b, err := json.Marshal(v); err == nil {
							secret.Fields[k] = string(b)
						}
					}
				}
				// Set primary value to password or first field
				if pw, ok := secret.Fields["password"]; ok {
					secret.Value = pw
				}
			}
		}
	}

	// Add version info to metadata
	if result.VersionId != nil {
		secret.Metadata.Version = *result.VersionId
	}
	if result.CreatedDate != nil {
		secret.Metadata.CreatedAt = &vault.Timestamp{Time: *result.CreatedDate}
	}
	if result.ARN != nil {
		if secret.Metadata.Extra == nil {
			secret.Metadata.Extra = make(map[string]any)
		}
		secret.Metadata.Extra["arn"] = *result.ARN
	}

	return secret, nil
}

// Set stores a secret in AWS Secrets Manager.
func (p *Provider) Set(ctx context.Context, path string, secret *vault.Secret) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return vault.NewVaultError("Set", path, p.Name(), vault.ErrClosed)
	}

	// Check if secret exists
	exists, err := p.existsUnlocked(ctx, path)
	if err != nil {
		return err
	}

	var secretValue string
	if len(secret.Fields) > 0 {
		// Store as JSON if fields are present
		data, err := json.Marshal(secret.Fields)
		if err != nil {
			return vault.NewVaultError("Set", path, p.Name(), err)
		}
		secretValue = string(data)
	} else {
		secretValue = secret.String()
	}

	if exists {
		// Update existing secret
		input := &secretsmanager.PutSecretValueInput{
			SecretId:     aws.String(path),
			SecretString: aws.String(secretValue),
		}
		_, err = p.client.PutSecretValue(ctx, input)
	} else {
		// Create new secret
		input := &secretsmanager.CreateSecretInput{
			Name:         aws.String(path),
			SecretString: aws.String(secretValue),
		}

		// Add tags from metadata
		if secret.Metadata.Tags != nil {
			for k, v := range secret.Metadata.Tags {
				input.Tags = append(input.Tags, types.Tag{
					Key:   aws.String(k),
					Value: aws.String(v),
				})
			}
		}

		_, err = p.client.CreateSecret(ctx, input)
	}

	if err != nil {
		return vault.NewVaultError("Set", path, p.Name(), err)
	}

	return nil
}

// Delete removes a secret from AWS Secrets Manager.
func (p *Provider) Delete(ctx context.Context, path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return vault.NewVaultError("Delete", path, p.Name(), vault.ErrClosed)
	}

	input := &secretsmanager.DeleteSecretInput{
		SecretId:                   aws.String(path),
		ForceDeleteWithoutRecovery: aws.Bool(true),
	}

	_, err := p.client.DeleteSecret(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return nil // Already deleted
		}
		return vault.NewVaultError("Delete", path, p.Name(), err)
	}

	return nil
}

// Exists checks if a secret exists in AWS Secrets Manager.
func (p *Provider) Exists(ctx context.Context, path string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false, vault.NewVaultError("Exists", path, p.Name(), vault.ErrClosed)
	}

	return p.existsUnlocked(ctx, path)
}

func (p *Provider) existsUnlocked(ctx context.Context, path string) (bool, error) {
	input := &secretsmanager.DescribeSecretInput{
		SecretId: aws.String(path),
	}

	_, err := p.client.DescribeSecret(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, vault.NewVaultError("Exists", path, p.Name(), err)
	}

	return true, nil
}

// List returns all secret names matching the prefix.
func (p *Provider) List(ctx context.Context, prefix string) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("List", prefix, p.Name(), vault.ErrClosed)
	}

	var results []string
	paginator := secretsmanager.NewListSecretsPaginator(p.client, &secretsmanager.ListSecretsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, vault.NewVaultError("List", prefix, p.Name(), err)
		}

		for _, secret := range page.SecretList {
			if secret.Name != nil {
				name := *secret.Name
				if strings.HasPrefix(name, prefix) {
					results = append(results, name)
				}
			}
		}
	}

	return results, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "aws-sm"
}

// Capabilities returns the provider capabilities.
func (p *Provider) Capabilities() vault.Capabilities {
	return vault.Capabilities{
		Read:       true,
		Write:      true,
		Delete:     true,
		List:       true,
		Versioning: true,
		Rotation:   true,
		Binary:     true,
		MultiField: true,
	}
}

// Close releases resources.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// GetVersion retrieves a specific version of a secret.
func (p *Provider) GetVersion(ctx context.Context, path, versionID string) (*vault.Secret, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("GetVersion", path, p.Name(), vault.ErrClosed)
	}

	input := &secretsmanager.GetSecretValueInput{
		SecretId:  aws.String(path),
		VersionId: aws.String(versionID),
	}

	result, err := p.client.GetSecretValue(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return nil, vault.NewVaultError("GetVersion", path, p.Name(), vault.ErrVersionNotFound)
		}
		return nil, vault.NewVaultError("GetVersion", path, p.Name(), err)
	}

	secret := &vault.Secret{
		Value: aws.ToString(result.SecretString),
		Metadata: vault.Metadata{
			Provider: p.Name(),
			Path:     path,
			Version:  aws.ToString(result.VersionId),
		},
	}

	return secret, nil
}

// ListVersions returns all versions of a secret.
func (p *Provider) ListVersions(ctx context.Context, path string) ([]vault.Version, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("ListVersions", path, p.Name(), vault.ErrClosed)
	}

	input := &secretsmanager.ListSecretVersionIdsInput{
		SecretId: aws.String(path),
	}

	result, err := p.client.ListSecretVersionIds(ctx, input)
	if err != nil {
		return nil, vault.NewVaultError("ListVersions", path, p.Name(), err)
	}

	var versions []vault.Version
	for _, v := range result.Versions {
		version := vault.Version{
			ID: aws.ToString(v.VersionId),
		}
		if v.CreatedDate != nil {
			version.CreatedAt = &vault.Timestamp{Time: *v.CreatedDate}
		}
		for _, stage := range v.VersionStages {
			if stage == "AWSCURRENT" {
				version.Current = true
				break
			}
		}
		versions = append(versions, version)
	}

	return versions, nil
}

// Rotate triggers rotation for a secret.
func (p *Provider) Rotate(ctx context.Context, path string) (*vault.Secret, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, vault.NewVaultError("Rotate", path, p.Name(), vault.ErrClosed)
	}

	input := &secretsmanager.RotateSecretInput{
		SecretId: aws.String(path),
	}

	_, err := p.client.RotateSecret(ctx, input)
	if err != nil {
		p.mu.Unlock()
		return nil, vault.NewVaultError("Rotate", path, p.Name(), err)
	}
	p.mu.Unlock()

	// Return the new secret value
	return p.Get(ctx, path)
}

// Helper functions

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "ResourceNotFoundException") ||
		strings.Contains(errStr, "SecretNotFound")
}

func isAccessDeniedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "AccessDeniedException") ||
		strings.Contains(errStr, "UnauthorizedException")
}

// Ensure Provider implements vault.Vault and vault.ExtendedVault.
var (
	_ vault.Vault         = (*Provider)(nil)
	_ vault.ExtendedVault = (*Provider)(nil)
)
