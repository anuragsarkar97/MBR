package collector

// database.go collects RDS instances, RDS clusters, DynamoDB tables, and
// ElastiCache clusters.
//
// Metadata keys written by this file:
//
//	RDS instance:  Engine, EngineVersion, Status, DBInstanceClass, MultiAZ,
//	               StorageGB, StorageType, VpcId, SecurityGroupIds, Endpoint
//	RDS cluster:   Engine, EngineVersion, Status, MultiAZ, Members,
//	               SecurityGroupIds, Endpoint
//	DynamoDB:      Status, BillingMode, ItemCount, SizeBytes, StreamEnabled
//	ElastiCache:   Engine, EngineVersion, CacheNodeType, Status,
//	               NumNodes, SubnetGroupName, ReplicationGroupId

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticache"
	elasticachetypes "github.com/aws/aws-sdk-go-v2/service/elasticache/types"
	"github.com/aws/aws-sdk-go-v2/service/rds"
	rdstypes "github.com/aws/aws-sdk-go-v2/service/rds/types"
)

func init() {
	DefaultRegistry.Register(TypeRDSInstance, func(cfg aws.Config) Collector {
		return &rdsInstanceCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeRDSCluster, func(cfg aws.Config) Collector {
		return &rdsClusterCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeDynamoTable, func(cfg aws.Config) Collector {
		return &dynamoCollector{cfg: cfg}
	})
	DefaultRegistry.Register(TypeElastiCache, func(cfg aws.Config) Collector {
		return &elastiCacheCollector{cfg: cfg}
	})
}

// ── RDS Instances ─────────────────────────────────────────────────────────────

type rdsInstanceCollector struct{ cfg aws.Config }

func (c *rdsInstanceCollector) Type() ResourceType { return TypeRDSInstance }

func (c *rdsInstanceCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := rds.NewFromConfig(cfg)
	paginator := rds.NewDescribeDBInstancesPaginator(client, &rds.DescribeDBInstancesInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("rds DescribeDBInstances %s: %w", region, err)
		}
		for _, db := range page.DBInstances {
			resources = append(resources, normaliseRDSInstance(db, region))
		}
	}
	return resources, nil
}

func normaliseRDSInstance(db rdstypes.DBInstance, region string) Resource {
	id := aws.ToString(db.DBInstanceIdentifier)
	arn := aws.ToString(db.DBInstanceArn)

	endpoint := ""
	port := ""
	if db.Endpoint != nil && db.Endpoint.Address != nil {
		endpoint = fmt.Sprintf("%s:%d", *db.Endpoint.Address, db.Endpoint.Port)
		port = fmt.Sprintf("%d", db.Endpoint.Port)
	}

	vpcID := ""
	if db.DBSubnetGroup != nil {
		vpcID = aws.ToString(db.DBSubnetGroup.VpcId)
	}

	paramGroup := ""
	if len(db.DBParameterGroups) > 0 {
		paramGroup = aws.ToString(db.DBParameterGroups[0].DBParameterGroupName)
	}

	meta := map[string]string{
		"Engine":             aws.ToString(db.Engine),
		"EngineVersion":      aws.ToString(db.EngineVersion),
		"Status":             aws.ToString(db.DBInstanceStatus),
		"DBInstanceClass":    aws.ToString(db.DBInstanceClass),
		"MultiAZ":            fmt.Sprintf("%v", db.MultiAZ),
		"StorageGB":          fmt.Sprintf("%d", aws.ToInt32(db.AllocatedStorage)),
		"StorageType":        aws.ToString(db.StorageType),
		"VpcId":              vpcID,
		"SecurityGroupIds":   rdsVpcSGIds(db.VpcSecurityGroups),
		"Endpoint":           endpoint,
		"Port":               port,
		"BackupRetention":    fmt.Sprintf("%d", aws.ToInt32(db.BackupRetentionPeriod)),
		"PubliclyAccessible": fmt.Sprintf("%v", aws.ToBool(db.PubliclyAccessible)),
		"DeletionProtection": fmt.Sprintf("%v", db.DeletionProtection),
		"ParameterGroup":     paramGroup,
	}

	return Resource{
		ID:       arn,
		RawID:    id,
		Type:     TypeRDSInstance,
		Name:     id,
		Region:   region,
		Tags:     rdsTagsToMap(db.TagList),
		Metadata: meta,
	}
}

// ── RDS Clusters ──────────────────────────────────────────────────────────────

type rdsClusterCollector struct{ cfg aws.Config }

func (c *rdsClusterCollector) Type() ResourceType { return TypeRDSCluster }

func (c *rdsClusterCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := rds.NewFromConfig(cfg)
	paginator := rds.NewDescribeDBClustersPaginator(client, &rds.DescribeDBClustersInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("rds DescribeDBClusters %s: %w", region, err)
		}
		for _, cl := range page.DBClusters {
			resources = append(resources, normaliseRDSCluster(cl, region))
		}
	}
	return resources, nil
}

func normaliseRDSCluster(c rdstypes.DBCluster, region string) Resource {
	id := aws.ToString(c.DBClusterIdentifier)
	arn := aws.ToString(c.DBClusterArn)

	endpoint := aws.ToString(c.Endpoint)
	multiAZ := len(c.AvailabilityZones) > 1

	meta := map[string]string{
		"Engine":           aws.ToString(c.Engine),
		"EngineVersion":    aws.ToString(c.EngineVersion),
		"Status":           aws.ToString(c.Status),
		"MultiAZ":          fmt.Sprintf("%v", multiAZ),
		"Members":          fmt.Sprintf("%d", len(c.DBClusterMembers)),
		"SecurityGroupIds": rdsClusterSGIds(c.VpcSecurityGroups),
		"Endpoint":         endpoint,
	}

	return Resource{
		ID:       arn,
		RawID:    id,
		Type:     TypeRDSCluster,
		Name:     id,
		Region:   region,
		Tags:     rdsTagsToMap(c.TagList),
		Metadata: meta,
	}
}

// ── DynamoDB ──────────────────────────────────────────────────────────────────

type dynamoCollector struct{ cfg aws.Config }

func (c *dynamoCollector) Type() ResourceType { return TypeDynamoTable }

func (c *dynamoCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := dynamodb.NewFromConfig(cfg)
	paginator := dynamodb.NewListTablesPaginator(client, &dynamodb.ListTablesInput{})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("dynamodb ListTables %s: %w", region, err)
		}
		for _, tableName := range page.TableNames {
			desc, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				continue // skip tables we can't describe
			}
			if desc.Table != nil {
				resources = append(resources, normaliseDynamoTable(*desc.Table, region))
			}
		}
	}
	return resources, nil
}

func normaliseDynamoTable(t dynamodbtypes.TableDescription, region string) Resource {
	name := aws.ToString(t.TableName)
	arn := aws.ToString(t.TableArn)

	billing := "PROVISIONED"
	if t.BillingModeSummary != nil {
		billing = string(t.BillingModeSummary.BillingMode)
	}

	streamEnabled := "false"
	if t.StreamSpecification != nil && aws.ToBool(t.StreamSpecification.StreamEnabled) {
		streamEnabled = "true"
	}

	meta := map[string]string{
		"Status":        string(t.TableStatus),
		"BillingMode":   billing,
		"ItemCount":     fmt.Sprintf("%d", aws.ToInt64(t.ItemCount)),
		"SizeBytes":     fmt.Sprintf("%d", aws.ToInt64(t.TableSizeBytes)),
		"StreamEnabled": streamEnabled,
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeDynamoTable,
		Name:     name,
		Region:   region,
		Metadata: meta,
	}
}

// ── ElastiCache ───────────────────────────────────────────────────────────────

type elastiCacheCollector struct{ cfg aws.Config }

func (c *elastiCacheCollector) Type() ResourceType { return TypeElastiCache }

func (c *elastiCacheCollector) Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error) {
	client := elasticache.NewFromConfig(cfg)
	paginator := elasticache.NewDescribeCacheClustersPaginator(client, &elasticache.DescribeCacheClustersInput{
		ShowCacheNodeInfo: aws.Bool(true),
	})

	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("elasticache DescribeCacheClusters %s: %w", region, err)
		}
		for _, cluster := range page.CacheClusters {
			resources = append(resources, normaliseElastiCache(cluster, region))
		}
	}
	return resources, nil
}

func normaliseElastiCache(c elasticachetypes.CacheCluster, region string) Resource {
	id := aws.ToString(c.CacheClusterId)
	arn := aws.ToString(c.ARN)

	meta := map[string]string{
		"Engine":             aws.ToString(c.Engine),
		"EngineVersion":      aws.ToString(c.EngineVersion),
		"CacheNodeType":      aws.ToString(c.CacheNodeType),
		"Status":             aws.ToString(c.CacheClusterStatus),
		"NumNodes":           fmt.Sprintf("%d", aws.ToInt32(c.NumCacheNodes)),
		"SubnetGroupName":    aws.ToString(c.CacheSubnetGroupName),
		"ReplicationGroupId": aws.ToString(c.ReplicationGroupId),
	}

	return Resource{
		ID:       arn,
		RawID:    id,
		Type:     TypeElastiCache,
		Name:     id,
		Region:   region,
		Metadata: meta,
	}
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func rdsVpcSGIds(sgs []rdstypes.VpcSecurityGroupMembership) string {
	var ids []string
	for _, sg := range sgs {
		if sg.VpcSecurityGroupId != nil {
			ids = append(ids, *sg.VpcSecurityGroupId)
		}
	}
	return strings.Join(ids, ",")
}

func rdsClusterSGIds(sgs []rdstypes.VpcSecurityGroupMembership) string {
	return rdsVpcSGIds(sgs)
}

func rdsTagsToMap(tags []rdstypes.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		if t.Key != nil && t.Value != nil {
			m[*t.Key] = *t.Value
		}
	}
	return m
}
