package aws

import (
	"github.com/plexusone/omnivault-aws/parameterstore"
	"github.com/plexusone/omnivault-aws/secretsmanager"
	"github.com/plexusone/omnivault/vault"
)

// NewSecretsManager creates a new AWS Secrets Manager provider.
// This is a convenience function that wraps secretsmanager.New().
//
// Usage:
//
//	provider, err := aws.NewSecretsManager(aws.Config{
//	    Region: "us-east-1",
//	})
func NewSecretsManager(cfg Config) (vault.Vault, error) {
	return secretsmanager.New(secretsmanager.Config{
		Region:      cfg.Region,
		Profile:     cfg.Profile,
		EndpointURL: cfg.EndpointURL,
		AWSConfig:   cfg.AWSConfig,
	})
}

// NewParameterStore creates a new AWS Parameter Store provider.
// This is a convenience function that wraps parameterstore.New().
//
// Usage:
//
//	provider, err := aws.NewParameterStore(aws.Config{
//	    Region: "us-east-1",
//	})
func NewParameterStore(cfg Config) (vault.Vault, error) {
	return parameterstore.New(parameterstore.Config{
		Region:      cfg.Region,
		Profile:     cfg.Profile,
		EndpointURL: cfg.EndpointURL,
		AWSConfig:   cfg.AWSConfig,
	})
}

// SecretsManagerConfig is an alias for secretsmanager.Config.
type SecretsManagerConfig = secretsmanager.Config

// ParameterStoreConfig is an alias for parameterstore.Config.
type ParameterStoreConfig = parameterstore.Config

// NewSecretsManagerWithConfig creates a Secrets Manager provider with full config.
func NewSecretsManagerWithConfig(cfg SecretsManagerConfig) (vault.Vault, error) {
	return secretsmanager.New(cfg)
}

// NewParameterStoreWithConfig creates a Parameter Store provider with full config.
func NewParameterStoreWithConfig(cfg ParameterStoreConfig) (vault.Vault, error) {
	return parameterstore.New(cfg)
}
