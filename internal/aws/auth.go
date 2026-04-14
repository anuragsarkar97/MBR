// Package aws provides shared AWS SDK configuration helpers.
package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
)

// LoadConfig builds an aws.Config using the standard credential chain
// (env vars → ~/.aws/credentials → IAM role). The profile and region
// parameters may be empty strings to use defaults.
//
// Profile maps to AWS_PROFILE / --profile CLI flag.
// Region maps to AWS_DEFAULT_REGION / --region CLI flag; if empty the
// value in ~/.aws/config is used.
func LoadConfig(ctx context.Context, profile, region string) (aws.Config, error) {
	opts := []func(*config.LoadOptions) error{}

	if profile != "" {
		opts = append(opts, config.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load AWS config: %w", err)
	}
	return cfg, nil
}

// WithRegion returns a copy of cfg with the region overridden.
// Used when fanning out across multiple regions from a single base config.
func WithRegion(cfg aws.Config, region string) aws.Config {
	copy := cfg.Copy()
	copy.Region = region
	return copy
}
