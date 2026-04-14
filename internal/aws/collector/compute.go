package collector

// compute.go collects Lambda functions, ELB (classic and v2), and Auto Scaling
// Groups.
//
// Metadata keys written by this file:
//
//	Lambda:       Runtime, Handler, MemoryMB, TimeoutSec, LastModified,
//	              Description, CodeSizeMB, VpcId, SecurityGroupIds
//	ELB v2:       Scheme, Type, State, VpcId, DNSName
//	ELB classic:  Scheme, DNSName, InstanceCount, VpcId
//	ASG:          MinSize, MaxSize, DesiredCapacity, LaunchTemplate,
//	              Status, HealthCheckType

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	autoscalingtypes "github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing"
	elbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"
	elbv2 "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
)

func init() {
	DefaultRegistry.Register(TypeLambdaFunction, func(cfg aws.Config) Collector {
		return &lambdaCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeELBV2, func(cfg aws.Config) Collector {
		return &elbv2Collector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeELBClassic, func(cfg aws.Config) Collector {
		return &elbClassicCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeASG, func(cfg aws.Config) Collector {
		return &asgCollector{cfg: cfg}
	})
}

// ── Lambda ────────────────────────────────────────────────────────────────────

type lambdaCollector struct{ cfg aws.Config }

func (c *lambdaCollector) Type() ResourceType { return TypeLambdaFunction }

func (c *lambdaCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := lambda.NewFromConfig(cfg)
	paginator := lambda.NewListFunctionsPaginator(client, &lambda.ListFunctionsInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("lambda ListFunctions %s: %w", region, err)
		}
		for _, fn := range page.Functions {
			resources = append(resources, normaliseLambda(fn, region))
		}
	}
	return resources, nil
}

func normaliseLambda(fn lambdatypes.FunctionConfiguration, region string) Resource {
	name := aws.ToString(fn.FunctionName)
	arn := aws.ToString(fn.FunctionArn)

	vpcID := ""
	sgIDs := ""
	if fn.VpcConfig != nil {
		vpcID = aws.ToString(fn.VpcConfig.VpcId)
		sgIDs = strings.Join(fn.VpcConfig.SecurityGroupIds, ",")
	}

	codeSizeMB := fmt.Sprintf("%.2f", float64(fn.CodeSize)/1024/1024)

	meta := map[string]string{
		"Runtime":          string(fn.Runtime),
		"Handler":          aws.ToString(fn.Handler),
		"MemoryMB":         fmt.Sprintf("%d", aws.ToInt32(fn.MemorySize)),
		"TimeoutSec":       fmt.Sprintf("%d", aws.ToInt32(fn.Timeout)),
		"LastModified":     aws.ToString(fn.LastModified),
		"Description":      aws.ToString(fn.Description),
		"CodeSizeMB":       codeSizeMB,
		"VpcId":            vpcID,
		"SecurityGroupIds": sgIDs,
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeLambdaFunction,
		Name:     name,
		Region:   region,
		Metadata: meta,
	}
}

// ── ELB v2 (ALB / NLB) ────────────────────────────────────────────────────────

type elbv2Collector struct{ cfg aws.Config }

func (c *elbv2Collector) Type() ResourceType { return TypeELBV2 }

func (c *elbv2Collector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := elbv2.NewFromConfig(cfg)
	paginator := elbv2.NewDescribeLoadBalancersPaginator(client, &elbv2.DescribeLoadBalancersInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("elbv2 DescribeLoadBalancers %s: %w", region, err)
		}
		for _, lb := range page.LoadBalancers {
			resources = append(resources, normaliseELBV2(lb, region))
		}
	}
	return resources, nil
}

func normaliseELBV2(lb elbv2types.LoadBalancer, region string) Resource {
	name := aws.ToString(lb.LoadBalancerName)
	arn := aws.ToString(lb.LoadBalancerArn)

	state := ""
	if lb.State != nil {
		state = string(lb.State.Code)
	}

	meta := map[string]string{
		"Scheme":  string(lb.Scheme),
		"Type":    string(lb.Type),
		"State":   state,
		"VpcId":   aws.ToString(lb.VpcId),
		"DNSName": aws.ToString(lb.DNSName),
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeELBV2,
		Name:     name,
		Region:   region,
		Metadata: meta,
	}
}

// ── ELB Classic ───────────────────────────────────────────────────────────────

type elbClassicCollector struct{ cfg aws.Config }

func (c *elbClassicCollector) Type() ResourceType { return TypeELBClassic }

func (c *elbClassicCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := elasticloadbalancing.NewFromConfig(cfg)
	paginator := elasticloadbalancing.NewDescribeLoadBalancersPaginator(client, &elasticloadbalancing.DescribeLoadBalancersInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("elb DescribeLoadBalancers %s: %w", region, err)
		}
		for _, lb := range page.LoadBalancerDescriptions {
			resources = append(resources, normaliseELBClassic(lb, region))
		}
	}
	return resources, nil
}

func normaliseELBClassic(lb elbtypes.LoadBalancerDescription, region string) Resource {
	name := aws.ToString(lb.LoadBalancerName)
	// Classic ELBs have no ARN; synthesise one for consistent ID format.
	arn := fmt.Sprintf("arn:aws:elasticloadbalancing:%s:loadbalancer/%s", region, name)

	meta := map[string]string{
		"Scheme":        aws.ToString(lb.Scheme),
		"DNSName":       aws.ToString(lb.DNSName),
		"InstanceCount": fmt.Sprintf("%d", len(lb.Instances)),
		"VpcId":         aws.ToString(lb.VPCId),
		"State":         "classic", // classic ELBs have no state field
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeELBClassic,
		Name:     name,
		Region:   region,
		Metadata: meta,
	}
}

// ── Auto Scaling Groups ───────────────────────────────────────────────────────

type asgCollector struct{ cfg aws.Config }

func (c *asgCollector) Type() ResourceType { return TypeASG }

func (c *asgCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := autoscaling.NewFromConfig(cfg)
	paginator := autoscaling.NewDescribeAutoScalingGroupsPaginator(client, &autoscaling.DescribeAutoScalingGroupsInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("autoscaling DescribeAutoScalingGroups %s: %w", region, err)
		}
		for _, asg := range page.AutoScalingGroups {
			resources = append(resources, normaliseASG(asg, region))
		}
	}
	return resources, nil
}

func normaliseASG(asg autoscalingtypes.AutoScalingGroup, region string) Resource {
	name := aws.ToString(asg.AutoScalingGroupName)
	arn := aws.ToString(asg.AutoScalingGroupARN)

	launchTemplate := ""
	if asg.LaunchTemplate != nil {
		launchTemplate = aws.ToString(asg.LaunchTemplate.LaunchTemplateName)
	} else if asg.LaunchConfigurationName != nil {
		launchTemplate = aws.ToString(asg.LaunchConfigurationName) + " (LC)"
	}

	status := "active"
	if asg.Status != nil {
		status = aws.ToString(asg.Status)
	}

	// Count instances by lifecycle state.
	var inService, pending, terminating int
	for _, inst := range asg.Instances {
		switch string(inst.LifecycleState) {
		case "InService":
			inService++
		case "Pending", "Pending:Wait", "Pending:Proceed":
			pending++
		case "Terminating", "Terminating:Wait", "Terminating:Proceed":
			terminating++
		}
	}

	// Availability zones as comma-separated string.
	azs := strings.Join(asg.AvailabilityZones, ",")

	// Target group ARNs as comma-separated string.
	tgARNs := strings.Join(asg.TargetGroupARNs, ",")

	meta := map[string]string{
		"MinSize":           fmt.Sprintf("%d", aws.ToInt32(asg.MinSize)),
		"MaxSize":           fmt.Sprintf("%d", aws.ToInt32(asg.MaxSize)),
		"DesiredCapacity":   fmt.Sprintf("%d", aws.ToInt32(asg.DesiredCapacity)),
		"InServiceCount":    fmt.Sprintf("%d", inService),
		"PendingCount":      fmt.Sprintf("%d", pending),
		"TerminatingCount":  fmt.Sprintf("%d", terminating),
		"LaunchTemplate":    launchTemplate,
		"Status":            status,
		"HealthCheckType":   aws.ToString(asg.HealthCheckType),
		"AvailabilityZones": azs,
		"TargetGroupARNs":   tgARNs,
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeASG,
		Name:     name,
		Region:   region,
		Tags:     asgTagsToMap(asg.Tags),
		Metadata: meta,
	}
}

func asgTagsToMap(tags []autoscalingtypes.TagDescription) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		if t.Key != nil && t.Value != nil {
			m[*t.Key] = *t.Value
		}
	}
	return m
}
