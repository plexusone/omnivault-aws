# Release Notes - v0.1.0

**Release Date:** 2025-12-27

## Highlights

Initial release of OmniVault AWS provider with support for AWS Secrets Manager and Parameter Store.

## Added

- AWS Secrets Manager provider implementing `vault.Vault` and `vault.ExtendedVault` interfaces
- AWS Parameter Store provider implementing `vault.Vault` interface
- Convenience functions `NewSecretsManager()` and `NewParameterStore()` for quick setup
- Support for custom AWS config, region, profile, and endpoint URL
- JSON parsing for Secrets Manager secrets with field extraction
- Version staging labels support for Secrets Manager
- Path prefix support for Parameter Store namespacing
- Secret rotation trigger for Secrets Manager
- Version history retrieval for both providers
- Example usage demonstrating direct provider usage and OmniVault client integration

## Fixed

- Deadlock in `secretsmanager.Provider.Rotate()` method
- Type conversions in provider implementations

## Installation

```bash
go get github.com/plexusone/omnivault-aws@v0.1.0
```

## Quick Start

```go
import (
    "context"
    aws "github.com/plexusone/omnivault-aws"
)

func main() {
    ctx := context.Background()

    // Create Secrets Manager provider
    provider, err := aws.NewSecretsManager(aws.Config{
        Region: "us-east-1",
    })
    if err != nil {
        log.Fatal(err)
    }

    // Get a secret
    secret, err := provider.Get(ctx, "prod/database/credentials")
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Password:", secret.Value)
}
```

## Links

- [Full Changelog](CHANGELOG.md)
- [Documentation](https://pkg.go.dev/github.com/plexusone/omnivault-aws)
