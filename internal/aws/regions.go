// Package aws provides shared AWS SDK configuration helpers.
package aws

import (
	"context"
	"fmt"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
)

// ListRegions returns all enabled AWS regions for the account by calling
// ec2.DescribeRegions on the region baked into cfg (defaults to us-east-1
// if none set). The result is sorted alphabetically for stable display.
func ListRegions(ctx context.Context, cfg aws.Config) ([]string, error) {
	// DescribeRegions works on any region endpoint; we use whatever is
	// in cfg, falling back to us-east-1 if the config has no region set.
	if cfg.Region == "" {
		cfg = WithRegion(cfg, "us-east-1")
	}

	client := ec2.NewFromConfig(cfg)
	resp, err := client.DescribeRegions(ctx, &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false), // only enabled regions
	})
	if err != nil {
		return nil, fmt.Errorf("describe regions: %w", err)
	}

	regions := make([]string, 0, len(resp.Regions))
	for _, r := range resp.Regions {
		if r.RegionName != nil {
			regions = append(regions, *r.RegionName)
		}
	}
	sort.Strings(regions)
	return regions, nil
}
