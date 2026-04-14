package collector

// ec2.go collects EC2 instances and EBS volumes.
//
// Metadata keys written by this file:
//
//	EC2 instances: State, InstanceType, VpcId, SubnetId, PrivateIp, PublicIp,
//	               Platform, ImageId, KeyName
//	EBS volumes:   State, VolumeType, SizeGiB, AvailabilityZone, Encrypted,
//	               AttachedInstanceId (first attachment, if any)

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// ── EC2 Instances ────────────────────────────────────────────────────────────

// ec2InstanceCollector fetches EC2 instances for a single region.
type ec2InstanceCollector struct {
	cfg aws.Config
}

// init registers ec2InstanceCollector with the DefaultRegistry.
// Importing this package is sufficient to enable EC2 instance collection.
func init() {
	DefaultRegistry.Register(TypeEC2Instance, func(cfg aws.Config) Collector {
		return &ec2InstanceCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeEBSVolume, func(cfg aws.Config) Collector {
		return &ebsVolumeCollector{cfg: cfg}
	})
}

// Type implements Collector.
func (c *ec2InstanceCollector) Type() ResourceType { return TypeEC2Instance }

// Collect pages through DescribeInstances and normalises each instance into
// a Resource. It uses the SDK paginator to handle accounts with many instances.
func (c *ec2InstanceCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ec2 DescribeInstances %s: %w", region, err)
		}
		for _, r := range page.Reservations {
			for _, inst := range r.Instances {
				resources = append(resources, normaliseEC2Instance(inst, region))
			}
		}
	}
	return resources, nil
}

// normaliseEC2Instance converts an SDK instance to the canonical Resource.
func normaliseEC2Instance(inst ec2types.Instance, region string) Resource {
	tags := ec2TagsToMap(inst.Tags)
	name := tags["Name"]

	id := ""
	if inst.InstanceId != nil {
		id = *inst.InstanceId
	}

	// Build ARN-style ID for consistency with other resource types.
	arn := fmt.Sprintf("arn:aws:ec2:%s:instance/%s", region, id)

	meta := map[string]string{
		"State":        string(inst.State.Name),
		"InstanceType": string(inst.InstanceType),
	}
	if inst.VpcId != nil {
		meta["VpcId"] = *inst.VpcId
	}
	if inst.SubnetId != nil {
		meta["SubnetId"] = *inst.SubnetId
	}
	if inst.PrivateIpAddress != nil {
		meta["PrivateIp"] = *inst.PrivateIpAddress
	}
	if inst.PublicIpAddress != nil {
		meta["PublicIp"] = *inst.PublicIpAddress
	}
	if inst.ImageId != nil {
		meta["ImageId"] = *inst.ImageId
	}
	if inst.KeyName != nil {
		meta["KeyName"] = *inst.KeyName
	}
	if inst.Platform != "" {
		meta["Platform"] = string(inst.Platform)
	}
	// Collect security group IDs for edge building in graph/builder.go.
	sgIDs := ""
	for i, sg := range inst.SecurityGroups {
		if sg.GroupId != nil {
			if i > 0 {
				sgIDs += ","
			}
			sgIDs += *sg.GroupId
		}
	}
	if sgIDs != "" {
		meta["SecurityGroupIds"] = sgIDs
	}

	return Resource{
		ID:       arn,
		RawID:    id,
		Type:     TypeEC2Instance,
		Name:     name,
		Region:   region,
		Tags:     tags,
		Metadata: meta,
	}
}

// ── EBS Volumes ──────────────────────────────────────────────────────────────

// ebsVolumeCollector fetches EBS volumes for a single region.
type ebsVolumeCollector struct {
	cfg aws.Config
}

// Type implements Collector.
func (c *ebsVolumeCollector) Type() ResourceType { return TypeEBSVolume }

// Collect pages through DescribeVolumes and normalises each volume.
func (c *ebsVolumeCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := ec2.NewFromConfig(cfg)
	paginator := ec2.NewDescribeVolumesPaginator(client, &ec2.DescribeVolumesInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("ec2 DescribeVolumes %s: %w", region, err)
		}
		for _, vol := range page.Volumes {
			resources = append(resources, normaliseEBSVolume(vol, region))
		}
	}
	return resources, nil
}

// normaliseEBSVolume converts an SDK volume to the canonical Resource.
func normaliseEBSVolume(vol ec2types.Volume, region string) Resource {
	tags := ec2TagsToMap(vol.Tags)
	name := tags["Name"]

	id := ""
	if vol.VolumeId != nil {
		id = *vol.VolumeId
	}
	arn := fmt.Sprintf("arn:aws:ec2:%s:volume/%s", region, id)

	az := ""
	if vol.AvailabilityZone != nil {
		az = *vol.AvailabilityZone
	}

	size := ""
	if vol.Size != nil {
		size = fmt.Sprintf("%d", *vol.Size)
	}

	encrypted := "false"
	if vol.Encrypted != nil && *vol.Encrypted {
		encrypted = "true"
	}

	meta := map[string]string{
		"State":            string(vol.State),
		"VolumeType":       string(vol.VolumeType),
		"SizeGiB":          size,
		"AvailabilityZone": az,
		"Encrypted":        encrypted,
	}

	// Record the first attached instance ID so graph/builder can draw an edge.
	if len(vol.Attachments) > 0 && vol.Attachments[0].InstanceId != nil {
		meta["AttachedInstanceId"] = *vol.Attachments[0].InstanceId
	}

	return Resource{
		ID:       arn,
		RawID:    id,
		Type:     TypeEBSVolume,
		Name:     name,
		Region:   region,
		Tags:     tags,
		Metadata: meta,
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// ec2TagsToMap converts the EC2 SDK []Tag slice to a plain map[string]string.
func ec2TagsToMap(tags []ec2types.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		if t.Key != nil && t.Value != nil {
			m[*t.Key] = *t.Value
		}
	}
	return m
}
