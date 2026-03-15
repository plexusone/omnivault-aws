// Package parameterstore provides a OmniVault provider for AWS Systems Manager Parameter Store.
//
// AWS Parameter Store is designed for storing configuration and secrets:
//   - Application configuration
//   - Feature flags
//   - Secrets (with SecureString type)
//   - License keys
//
// Features:
//   - Hierarchical parameter paths (e.g., /myapp/prod/database/password)
//   - Three parameter types: String, StringList, SecureString
//   - KMS encryption for SecureString
//   - Parameter policies for expiration and notification
//
// Usage:
//
//	provider, err := parameterstore.New(parameterstore.Config{
//	    Region: "us-east-1",
//	})
//	secret, err := provider.Get(ctx, "/myapp/prod/database/password")
package parameterstore

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
	"github.com/plexusone/omnivault/vault"
)

// Config holds configuration for the Parameter Store provider.
type Config struct {
	// Region is the AWS region (e.g., "us-east-1").
	Region string

	// Profile is the AWS credentials profile name.
	Profile string

	// EndpointURL is a custom endpoint URL (for LocalStack, testing).
	EndpointURL string

	// AWSConfig is an optional pre-configured AWS SDK config.
	AWSConfig *aws.Config

	// WithDecryption specifies whether to decrypt SecureString parameters.
	// Default: true
	WithDecryption *bool

	// DefaultType is the default parameter type for Set operations.
	// Default: SecureString
	DefaultType types.ParameterType

	// KMSKeyID is the KMS key ID for encrypting SecureString parameters.
	// If empty, uses the default SSM key.
	KMSKeyID string

	// PathPrefix is an optional prefix added to all parameter paths.
	// Useful for namespacing (e.g., "/myapp/prod")
	PathPrefix string
}

// Provider implements vault.Vault for AWS Parameter Store.
type Provider struct {
	client *ssm.Client
	config Config
	mu     sync.RWMutex
	closed bool
}

// New creates a new Parameter Store provider.
func New(cfg Config) (*Provider, error) {
	ctx := context.Background()
	return NewWithContext(ctx, cfg)
}

// NewWithContext creates a new Parameter Store provider with context.
func NewWithContext(ctx context.Context, cfg Config) (*Provider, error) {
	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return nil, vault.NewVaultError("New", "", "aws-ssm", err)
	}

	var opts []func(*ssm.Options)
	if cfg.EndpointURL != "" {
		opts = append(opts, func(o *ssm.Options) {
			o.BaseEndpoint = aws.String(cfg.EndpointURL)
		})
	}

	client := ssm.NewFromConfig(awsCfg, opts...)

	// Set defaults
	if cfg.WithDecryption == nil {
		t := true
		cfg.WithDecryption = &t
	}

	if cfg.DefaultType == "" {
		cfg.DefaultType = types.ParameterTypeSecureString
	}

	return &Provider{
		client: client,
		config: cfg,
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

// fullPath returns the full parameter path with prefix.
func (p *Provider) fullPath(path string) string {
	if p.config.PathPrefix == "" {
		return path
	}
	// Ensure path starts with /
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	prefix := strings.TrimSuffix(p.config.PathPrefix, "/")
	return prefix + path
}

// stripPrefix removes the path prefix from a parameter name.
func (p *Provider) stripPrefix(name string) string {
	if p.config.PathPrefix == "" {
		return name
	}
	prefix := strings.TrimSuffix(p.config.PathPrefix, "/")
	return strings.TrimPrefix(name, prefix)
}

// Get retrieves a parameter from AWS Parameter Store.
func (p *Provider) Get(ctx context.Context, path string) (*vault.Secret, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("Get", path, p.Name(), vault.ErrClosed)
	}

	fullPath := p.fullPath(path)

	input := &ssm.GetParameterInput{
		Name:           aws.String(fullPath),
		WithDecryption: p.config.WithDecryption,
	}

	result, err := p.client.GetParameter(ctx, input)
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
		Value: aws.ToString(result.Parameter.Value),
		Metadata: vault.Metadata{
			Provider: p.Name(),
			Path:     path,
			Version:  strconv.FormatInt(result.Parameter.Version, 10),
		},
	}

	// Handle StringList type
	if result.Parameter.Type == types.ParameterTypeStringList {
		values := strings.Split(aws.ToString(result.Parameter.Value), ",")
		secret.Fields = make(map[string]string)
		for i, v := range values {
			secret.Fields[string(rune('0'+i))] = strings.TrimSpace(v)
		}
	}

	// Add metadata
	if result.Parameter.LastModifiedDate != nil {
		secret.Metadata.ModifiedAt = &vault.Timestamp{Time: *result.Parameter.LastModifiedDate}
	}
	if result.Parameter.ARN != nil {
		if secret.Metadata.Extra == nil {
			secret.Metadata.Extra = make(map[string]any)
		}
		secret.Metadata.Extra["arn"] = *result.Parameter.ARN
		secret.Metadata.Extra["type"] = string(result.Parameter.Type)
	}

	return secret, nil
}

// Set stores a parameter in AWS Parameter Store.
func (p *Provider) Set(ctx context.Context, path string, secret *vault.Secret) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return vault.NewVaultError("Set", path, p.Name(), vault.ErrClosed)
	}

	fullPath := p.fullPath(path)

	input := &ssm.PutParameterInput{
		Name:      aws.String(fullPath),
		Value:     aws.String(secret.String()),
		Type:      p.config.DefaultType,
		Overwrite: aws.Bool(true),
	}

	// Use KMS key if specified
	if p.config.KMSKeyID != "" && p.config.DefaultType == types.ParameterTypeSecureString {
		input.KeyId = aws.String(p.config.KMSKeyID)
	}

	// Add tags from metadata
	if secret.Metadata.Tags != nil {
		var tags []types.Tag
		for k, v := range secret.Metadata.Tags {
			tags = append(tags, types.Tag{
				Key:   aws.String(k),
				Value: aws.String(v),
			})
		}
		input.Tags = tags
	}

	_, err := p.client.PutParameter(ctx, input)
	if err != nil {
		return vault.NewVaultError("Set", path, p.Name(), err)
	}

	return nil
}

// Delete removes a parameter from AWS Parameter Store.
func (p *Provider) Delete(ctx context.Context, path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.closed {
		return vault.NewVaultError("Delete", path, p.Name(), vault.ErrClosed)
	}

	fullPath := p.fullPath(path)

	input := &ssm.DeleteParameterInput{
		Name: aws.String(fullPath),
	}

	_, err := p.client.DeleteParameter(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return nil // Already deleted
		}
		return vault.NewVaultError("Delete", path, p.Name(), err)
	}

	return nil
}

// Exists checks if a parameter exists.
func (p *Provider) Exists(ctx context.Context, path string) (bool, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return false, vault.NewVaultError("Exists", path, p.Name(), vault.ErrClosed)
	}

	fullPath := p.fullPath(path)

	input := &ssm.GetParameterInput{
		Name: aws.String(fullPath),
	}

	_, err := p.client.GetParameter(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, vault.NewVaultError("Exists", path, p.Name(), err)
	}

	return true, nil
}

// List returns all parameter names matching the path prefix.
func (p *Provider) List(ctx context.Context, prefix string) ([]string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("List", prefix, p.Name(), vault.ErrClosed)
	}

	fullPrefix := p.fullPath(prefix)

	var results []string
	paginator := ssm.NewGetParametersByPathPaginator(p.client, &ssm.GetParametersByPathInput{
		Path:           aws.String(fullPrefix),
		Recursive:      aws.Bool(true),
		WithDecryption: p.config.WithDecryption,
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, vault.NewVaultError("List", prefix, p.Name(), err)
		}

		for _, param := range page.Parameters {
			if param.Name != nil {
				name := p.stripPrefix(*param.Name)
				results = append(results, name)
			}
		}
	}

	return results, nil
}

// Name returns the provider name.
func (p *Provider) Name() string {
	return "aws-ssm"
}

// Capabilities returns the provider capabilities.
func (p *Provider) Capabilities() vault.Capabilities {
	return vault.Capabilities{
		Read:       true,
		Write:      true,
		Delete:     true,
		List:       true,
		Versioning: true,
	}
}

// Close releases resources.
func (p *Provider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// GetVersion retrieves a specific version of a parameter.
func (p *Provider) GetVersion(ctx context.Context, path, version string) (*vault.Secret, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("GetVersion", path, p.Name(), vault.ErrClosed)
	}

	fullPath := p.fullPath(path)

	// Parameter Store uses name:version format
	nameWithVersion := fullPath + ":" + version

	input := &ssm.GetParameterInput{
		Name:           aws.String(nameWithVersion),
		WithDecryption: p.config.WithDecryption,
	}

	result, err := p.client.GetParameter(ctx, input)
	if err != nil {
		if isNotFoundError(err) {
			return nil, vault.NewVaultError("GetVersion", path, p.Name(), vault.ErrVersionNotFound)
		}
		return nil, vault.NewVaultError("GetVersion", path, p.Name(), err)
	}

	return &vault.Secret{
		Value: aws.ToString(result.Parameter.Value),
		Metadata: vault.Metadata{
			Provider: p.Name(),
			Path:     path,
			Version:  version,
		},
	}, nil
}

// ListVersions returns all versions of a parameter.
func (p *Provider) ListVersions(ctx context.Context, path string) ([]vault.Version, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.closed {
		return nil, vault.NewVaultError("ListVersions", path, p.Name(), vault.ErrClosed)
	}

	fullPath := p.fullPath(path)

	input := &ssm.GetParameterHistoryInput{
		Name:           aws.String(fullPath),
		WithDecryption: p.config.WithDecryption,
	}

	var versions []vault.Version
	paginator := ssm.NewGetParameterHistoryPaginator(p.client, input)

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, vault.NewVaultError("ListVersions", path, p.Name(), err)
		}

		for _, h := range page.Parameters {
			v := vault.Version{
				ID: strconv.FormatInt(h.Version, 10),
			}
			if h.LastModifiedDate != nil {
				v.CreatedAt = &vault.Timestamp{Time: *h.LastModifiedDate}
			}
			versions = append(versions, v)
		}
	}

	// Mark the latest as current
	if len(versions) > 0 {
		versions[len(versions)-1].Current = true
	}

	return versions, nil
}

// Helper functions

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "ParameterNotFound") ||
		strings.Contains(errStr, "ParameterVersionNotFound")
}

func isAccessDeniedError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "AccessDeniedException") ||
		strings.Contains(errStr, "UnauthorizedException")
}

// Ensure Provider implements vault.Vault.
var _ vault.Vault = (*Provider)(nil)
