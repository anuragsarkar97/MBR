// Package cost fetches 30-day blended cost from AWS Cost Explorer for a
// single resource. It requires ce:GetCostAndUsageWithResources IAM permission
// and resource-level cost allocation to be enabled in the billing console.
// If neither is available it falls back to service-level cost for the
// resource's owning service.
package cost

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/angsak/mbr/internal/aws/collector"
)

// Result holds the cost lookup outcome for one resource.
type Result struct {
	// USD is the 30-day blended cost in US dollars.
	USD float64

	// Granularity is "resource" when a per-resource breakdown was available,
	// "service" when we could only get a service-level aggregate, or "none"
	// when Cost Explorer returned no data at all.
	Granularity string

	// Err is set when the API call failed entirely (e.g. no IAM permission).
	Err error
}

// serviceFor maps a collector ResourceType to the AWS Cost Explorer service
// name used in billing filters.
var serviceFor = map[collector.ResourceType]string{
	collector.TypeEC2Instance:    "Amazon EC2",
	collector.TypeEBSVolume:      "Amazon EC2",
	collector.TypeVPC:            "Amazon EC2",
	collector.TypeSubnet:         "Amazon EC2",
	collector.TypeSecurityGroup:  "Amazon EC2",
	collector.TypeIGW:            "Amazon EC2",
	collector.TypeELBV2:          "Amazon Elastic Load Balancing",
	collector.TypeELBClassic:     "Amazon Elastic Load Balancing",
	collector.TypeASG:            "Amazon EC2",
	collector.TypeRDSInstance:    "Amazon Relational Database Service",
	collector.TypeRDSCluster:     "Amazon Relational Database Service",
	collector.TypeDynamoTable:    "Amazon DynamoDB",
	collector.TypeElastiCache:    "Amazon ElastiCache",
	collector.TypeLambdaFunction: "AWS Lambda",
}

// FetchResource tries to get the 30-day cost for a specific resource ID.
// Cost Explorer is a global service; the client must be configured to call
// us-east-1. If the account does not have resource-level cost allocation
// enabled it falls back to FetchService.
func FetchResource(ctx context.Context, cfg aws.Config, res collector.Resource) Result {
	// Cost Explorer is only available from us-east-1.
	ceCfg := cfg.Copy()
	ceCfg.Region = "us-east-1"

	client := costexplorer.NewFromConfig(ceCfg)

	end := time.Now().Format("2006-01-02")
	start := time.Now().AddDate(0, -1, 0).Format("2006-01-02")

	// Resource-level cost allocation uses the short raw ID (not the ARN).
	rawID := res.RawID
	if rawID == "" {
		rawID = res.ID
	}

	out, err := client.GetCostAndUsageWithResources(ctx, &costexplorer.GetCostAndUsageWithResourcesInput{
		TimePeriod: &cetypes.DateInterval{Start: aws.String(start), End: aws.String(end)},
		Granularity: cetypes.GranularityMonthly,
		Filter: &cetypes.Expression{
			And: []cetypes.Expression{
				{
					Dimensions: &cetypes.DimensionValues{
						Key:    cetypes.DimensionResourceId,
						Values: []string{rawID},
					},
				},
			},
		},
		Metrics: []string{"BlendedCost"},
	})
	if err == nil && len(out.ResultsByTime) > 0 {
		total := sumResults(out.ResultsByTime)
		if total > 0 {
			return Result{USD: total, Granularity: "resource"}
		}
	}

	// Fall back to service-level cost.
	return FetchService(ctx, cfg, res)
}

// FetchService returns the 30-day cost for the AWS service that owns res.
// This is a coarser number shared with all resources of the same service.
func FetchService(ctx context.Context, cfg aws.Config, res collector.Resource) Result {
	svc, ok := serviceFor[res.Type]
	if !ok {
		return Result{Granularity: "none"}
	}

	ceCfg := cfg.Copy()
	ceCfg.Region = "us-east-1"
	client := costexplorer.NewFromConfig(ceCfg)

	end := time.Now().Format("2006-01-02")
	start := time.Now().AddDate(0, -1, 0).Format("2006-01-02")

	out, err := client.GetCostAndUsage(ctx, &costexplorer.GetCostAndUsageInput{
		TimePeriod:  &cetypes.DateInterval{Start: aws.String(start), End: aws.String(end)},
		Granularity: cetypes.GranularityMonthly,
		Filter: &cetypes.Expression{
			Dimensions: &cetypes.DimensionValues{
				Key:    cetypes.DimensionServiceCode,
				Values: []string{svc},
			},
		},
		Metrics: []string{"BlendedCost"},
	})
	if err != nil {
		return Result{Err: fmt.Errorf("cost explorer: %w", err)}
	}

	total := sumResults(out.ResultsByTime)
	return Result{USD: total, Granularity: "service"}
}

// ServiceNameFor returns the Cost Explorer service name for a resource type.
func ServiceNameFor(rt collector.ResourceType) (string, bool) {
	s, ok := serviceFor[rt]
	return s, ok
}

func sumResults(results []cetypes.ResultByTime) float64 {
	var total float64
	for _, r := range results {
		if m, ok := r.Total["BlendedCost"]; ok && m.Amount != nil {
			v, _ := strconv.ParseFloat(*m.Amount, 64)
			total += v
		}
	}
	return total
}
