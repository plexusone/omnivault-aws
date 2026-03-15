# Release Notes - v0.2.0

**Release Date:** 2026-03-14

## Highlights

Module renamed from `github.com/agentplexus/omnivault-aws` to `github.com/plexusone/omnivault-aws`.

## Breaking Changes

- Module path changed from `github.com/agentplexus/omnivault-aws` to `github.com/plexusone/omnivault-aws`

## Upgrade Guide

1. Update import paths in your Go files:

   ```go
   // Before
   import "github.com/agentplexus/omnivault-aws"

   // After
   import "github.com/plexusone/omnivault-aws"
   ```

2. Update your `go.mod` dependency:

   ```bash
   go get github.com/plexusone/omnivault-aws@v0.2.0
   ```

3. Run `go mod tidy` to update `go.sum`

## Changed

- Dependency `github.com/agentplexus/omnivault` renamed to `github.com/plexusone/omnivault` and updated to v0.3.0

## Dependencies

- Update `github.com/plexusone/omnivault` to v0.3.0 (renamed from `github.com/agentplexus/omnivault`)
- Update `github.com/aws/aws-sdk-go-v2/config` to v1.32.12
- Update `github.com/aws/aws-sdk-go-v2/service/secretsmanager` to v1.41.4
- Update `github.com/aws/aws-sdk-go-v2/service/ssm` to v1.68.3

## Installation

```bash
go get github.com/plexusone/omnivault-aws@v0.2.0
```

## Links

- [Full Changelog](CHANGELOG.md)
- [Documentation](https://pkg.go.dev/github.com/plexusone/omnivault-aws)
- [Compare v0.1.0...v0.2.0](https://github.com/plexusone/omnivault-aws/compare/v0.1.0...v0.2.0)
