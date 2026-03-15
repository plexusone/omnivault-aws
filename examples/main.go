// Example usage of omnivault-aws
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/plexusone/omnivault"
	"github.com/plexusone/omnivault/vault"

	aws "github.com/plexusone/omnivault-aws"
	"github.com/plexusone/omnivault-aws/secretsmanager"
)

func main() {
	ctx := context.Background()

	// Example 1: Using Secrets Manager directly
	fmt.Println("=== AWS Secrets Manager ===")
	smProvider, err := secretsmanager.New(secretsmanager.Config{
		Region: getEnvOrDefault("AWS_REGION", "us-east-1"),
	})
	if err != nil {
		log.Printf("Failed to create Secrets Manager provider: %v", err)
		log.Println("(This is expected if AWS credentials are not configured)")
	} else {
		// Store a secret
		err = smProvider.Set(ctx, "myapp/test-secret", &vault.Secret{
			Value: "my-secret-value",
			Fields: map[string]string{
				"username": "admin",
				"password": "secret123",
			},
			Metadata: vault.Metadata{
				Tags: map[string]string{
					"environment": "development",
				},
			},
		})
		if err != nil {
			log.Printf("Failed to store secret: %v", err)
		} else {
			fmt.Println("Stored secret: myapp/test-secret")

			// Retrieve the secret
			secret, err := smProvider.Get(ctx, "myapp/test-secret")
			if err != nil {
				log.Printf("Failed to get secret: %v", err)
			} else {
				fmt.Printf("Retrieved: %s\n", secret.Value)
				fmt.Printf("Username: %s\n", secret.Fields["username"])
			}

			// Clean up
			_ = smProvider.Delete(ctx, "myapp/test-secret")
			fmt.Println("Deleted test secret")
		}
	}

	// Example 2: Using convenience function
	fmt.Println("\n=== Using Convenience Functions ===")
	provider, err := aws.NewSecretsManager(aws.Config{
		Region: "us-east-1",
	})
	if err != nil {
		log.Printf("Failed to create provider: %v", err)
	} else {
		fmt.Printf("Created provider: %s\n", provider.Name())
	}

	// Example 3: With OmniVault client
	fmt.Println("\n=== With OmniVault Client ===")
	if provider != nil {
		client, err := omnivault.NewClient(omnivault.Config{
			CustomVault: provider,
		})
		if err != nil {
			log.Fatal(err)
		}
		defer client.Close()

		fmt.Printf("OmniVault client using: %s\n", client.Name())
		fmt.Printf("Capabilities: %+v\n", client.Capabilities())
	}

	// Example 4: Multi-provider resolver (AWS + local)
	fmt.Println("\n=== Multi-Provider Resolver ===")
	fmt.Println("In production, you could use:")
	fmt.Println("  resolver.Register(\"secret\", awsProvider)  // EKS")
	fmt.Println("  resolver.Register(\"secret\", keyringProvider)  // Local dev")
	fmt.Println("Then use: resolver.Resolve(ctx, \"secret://database/password\")")
}

func getEnvOrDefault(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}
