package collector

// vpc.go collects VPCs, subnets, security groups, and internet gateways.
//
// Metadata keys written by this file:
//
//	VPC:    CidrBlock, IsDefault, State
//	Subnet: CidrBlock, VpcId, AvailabilityZone, MapPublicIpOnLaunch
//	SG:     VpcId, Description
//	IGW:    AttachedVpcId (first attachment)

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// init registers all VPC-related collectors with the DefaultRegistry.
func init() {
	DefaultRegistry.Register(TypeVPC, func(cfg aws.Config) Collector {
		return &vpcCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeSubnet, func(cfg aws.Config) Collector {
		return &subnetCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeSecurityGroup, func(cfg aws.Config) Collector {
		return &sgCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeIGW, func(cfg aws.Config) Collector {
		return &igwCollector{cfg: cfg}
	})
}

// ── VPC ──────────────────────────────────────────────────────────────────────

type vpcCollector struct{ cfg aws.Config }

func (c *vpcCollector) Type() ResourceType { return TypeVPC }

// Collect fetches all VPCs in the region.
func (c *vpcCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := ec2.NewFromConfig(cfg)
	resp, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, fmt.Errorf("ec2 DescribeVpcs %s: %w", region, err)
	}

	resources := make([]Resource, 0, len(resp.Vpcs))
	for _, vpc := range resp.Vpcs {
		resources = append(resources, normaliseVPC(vpc, region))
	}
	return resources, nil
}

func normaliseVPC(vpc ec2types.Vpc, region string) Resource {
	tags := ec2TagsToMap(vpc.Tags)
	id := aws.ToString(vpc.VpcId)
	arn := fmt.Sprintf("arn:aws:ec2:%s:vpc/%s", region, id)

	isDefault := "false"
	if vpc.IsDefault != nil && *vpc.IsDefault {
		isDefault = "true"
	}

	return Resource{
		ID:     arn,
		RawID:  id,
		Type:   TypeVPC,
		Name:   tags["Name"],
		Region: region,
		Tags:   tags,
		Metadata: map[string]string{
			"CidrBlock": aws.ToString(vpc.CidrBlock),
			"IsDefault": isDefault,
			"State":     string(vpc.State),
		},
	}
}

// ── Subnet ───────────────────────────────────────────────────────────────────

type subnetCollector struct{ cfg aws.Config }

func (c *subnetCollector) Type() ResourceType { return TypeSubnet }

// Collect fetches all subnets in the region using the SDK paginator.
func (c *subnetCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeSubnetsPaginator(client, &ec2.DescribeSubnetsInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ec2 DescribeSubnets %s: %w", region, err)
		}
		for _, sn := range page.Subnets {
			resources = append(resources, normaliseSubnet(sn, region))
		}
	}
	return resources, nil
}

func normaliseSubnet(sn ec2types.Subnet, region string) Resource {
	tags := ec2TagsToMap(sn.Tags)
	id := aws.ToString(sn.SubnetId)
	arn := fmt.Sprintf("arn:aws:ec2:%s:subnet/%s", region, id)

	mapPublic := "false"
	if sn.MapPublicIpOnLaunch != nil && *sn.MapPublicIpOnLaunch {
		mapPublic = "true"
	}

	return Resource{
		ID:     arn,
		RawID:  id,
		Type:   TypeSubnet,
		Name:   tags["Name"],
		Region: region,
		Tags:   tags,
		Metadata: map[string]string{
			"CidrBlock":          aws.ToString(sn.CidrBlock),
			"VpcId":              aws.ToString(sn.VpcId),
			"AvailabilityZone":   aws.ToString(sn.AvailabilityZone),
			"MapPublicIpOnLaunch": mapPublic,
		},
	}
}

// ── Security Group ───────────────────────────────────────────────────────────

type sgCollector struct{ cfg aws.Config }

func (c *sgCollector) Type() ResourceType { return TypeSecurityGroup }

// Collect fetches all security groups in the region.
func (c *sgCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeSecurityGroupsPaginator(client, &ec2.DescribeSecurityGroupsInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ec2 DescribeSecurityGroups %s: %w", region, err)
		}
		for _, sg := range page.SecurityGroups {
			resources = append(resources, normaliseSG(sg, region))
		}
	}
	return resources, nil
}

func normaliseSG(sg ec2types.SecurityGroup, region string) Resource {
	tags := ec2TagsToMap(sg.Tags)
	id := aws.ToString(sg.GroupId)
	arn := fmt.Sprintf("arn:aws:ec2:%s:sg/%s", region, id)

	name := tags["Name"]
	if name == "" {
		name = aws.ToString(sg.GroupName)
	}

	return Resource{
		ID:     arn,
		RawID:  id,
		Type:   TypeSecurityGroup,
		Name:   name,
		Region: region,
		Tags:   tags,
		Metadata: map[string]string{
			"VpcId":         aws.ToString(sg.VpcId),
			"Description":   aws.ToString(sg.Description),
			"GroupName":     aws.ToString(sg.GroupName),
			"InboundRules":  formatIpPermissions(sg.IpPermissions),
			"OutboundRules": formatIpPermissions(sg.IpPermissionsEgress),
		},
	}
}

// formatIpPermissions serialises a slice of IpPermission into a newline-
// separated string, one entry per source/destination range. Each entry has
// the form: protocol|portRange|source|description
func formatIpPermissions(perms []ec2types.IpPermission) string {
	var lines []string
	for _, p := range perms {
		proto := aws.ToString(p.IpProtocol)
		portRange := formatPortRange(proto, p.FromPort, p.ToPort)
		displayProto := proto
		if proto == "-1" {
			displayProto = "all"
		}

		for _, r := range p.IpRanges {
			lines = append(lines, displayProto+"|"+portRange+"|"+aws.ToString(r.CidrIp)+"|"+aws.ToString(r.Description))
		}
		for _, r := range p.Ipv6Ranges {
			lines = append(lines, displayProto+"|"+portRange+"|"+aws.ToString(r.CidrIpv6)+"|"+aws.ToString(r.Description))
		}
		for _, pair := range p.UserIdGroupPairs {
			src := aws.ToString(pair.GroupId)
			if gn := aws.ToString(pair.GroupName); gn != "" {
				src = gn + " (" + src + ")"
			}
			lines = append(lines, displayProto+"|"+portRange+"|"+src+"|"+aws.ToString(pair.Description))
		}
		// Rule with no source/destination (edge case).
		if len(p.IpRanges) == 0 && len(p.Ipv6Ranges) == 0 && len(p.UserIdGroupPairs) == 0 {
			lines = append(lines, displayProto+"|"+portRange+"||")
		}
	}
	return strings.Join(lines, "\n")
}

// formatPortRange returns a human-readable port range string.
func formatPortRange(proto string, from, to *int32) string {
	if proto == "-1" {
		return "all"
	}
	if from == nil || to == nil {
		return "n/a"
	}
	f, t := *from, *to
	if f == -1 && t == -1 {
		return "all"
	}
	if f == t {
		return fmt.Sprintf("%d", f)
	}
	return fmt.Sprintf("%d-%d", f, t)
}

// ── Internet Gateway ─────────────────────────────────────────────────────────

type igwCollector struct{ cfg aws.Config }

func (c *igwCollector) Type() ResourceType { return TypeIGW }

// Collect fetches all internet gateways in the region.
func (c *igwCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeInternetGatewaysPaginator(client, &ec2.DescribeInternetGatewaysInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ec2 DescribeInternetGateways %s: %w", region, err)
		}
		for _, igw := range page.InternetGateways {
			resources = append(resources, normaliseIGW(igw, region))
		}
	}
	return resources, nil
}

func normaliseIGW(igw ec2types.InternetGateway, region string) Resource {
	tags := ec2TagsToMap(igw.Tags)
	id := aws.ToString(igw.InternetGatewayId)
	arn := fmt.Sprintf("arn:aws:ec2:%s:igw/%s", region, id)

	// Record the first attached VPC for edge building.
	attachedVpc := ""
	if len(igw.Attachments) > 0 {
		attachedVpc = aws.ToString(igw.Attachments[0].VpcId)
	}

	return Resource{
		ID:     arn,
		RawID:  id,
		Type:   TypeIGW,
		Name:   tags["Name"],
		Region: region,
		Tags:   tags,
		Metadata: map[string]string{
			"AttachedVpcId": attachedVpc,
		},
	}
}
