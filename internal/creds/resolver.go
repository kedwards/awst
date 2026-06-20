package creds

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// Provider is the small slice of aws.CredentialsProvider this package uses.
// Kept local so tests can stub it without depending on the SDK loader.
type Provider interface {
	Retrieve(ctx context.Context) (aws.Credentials, error)
}

// Resolve asks the provider for credentials and copies the relevant fields.
// Errors are wrapped with the profile name for clearer CLI output.
func Resolve(ctx context.Context, profile string, p Provider) (Credentials, error) {
	c, err := p.Retrieve(ctx)
	if err != nil {
		return Credentials{}, fmt.Errorf("resolve profile %q: %w", profile, err)
	}
	return Credentials{
		AccessKeyID:     c.AccessKeyID,
		SecretAccessKey: c.SecretAccessKey,
		SessionToken:    c.SessionToken,
	}, nil
}

// NewSDKProvider builds a Provider backed by the AWS SDK default credential
// chain for the named profile and returns the effective region.
func NewSDKProvider(ctx context.Context, profile, region string) (Provider, string, error) {
	opts := []func(*config.LoadOptions) error{
		config.WithSharedConfigProfile(profile),
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("load aws config for profile %q: %w", profile, err)
	}
	return cfg.Credentials, cfg.Region, nil
}
