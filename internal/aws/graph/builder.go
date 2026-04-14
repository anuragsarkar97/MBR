package graph

// builder.go constructs a ResourceGraph from a flat []collector.Resource slice.
//
// Each edge type is implemented as a separate unexported function (EdgeRule).
// To add a new relationship type, write a new function matching the edgeRule
// signature and append it to the rules slice in BuildGraph.

import (
	"fmt"
	"strings"

	"github.com/anuragsarkar97/mbr/internal/aws/collector"
)

// edgeRule is a function that scans the full resource index and adds edges to
// the graph for one specific relationship type.
// Receives an ID-keyed index for O(1) lookups by raw AWS ID.
type edgeRule func(byRawID map[string]*collector.Resource, g *ResourceGraph)

// BuildGraph constructs a ResourceGraph from a flat list of resources.
//
// Phase 1 edge rules:
//   - EBS volume → EC2 instance (attached-to)
//   - EC2 instance → Security Group (secured-by)
//   - EC2 instance → Subnet (contains, reversed: subnet contains EC2)
//   - Subnet → VPC (contains)
//   - IGW → VPC (routes-via)
//   - RDS instance → Security Group (secured-by)
//   - RDS cluster → Security Group (secured-by)
//   - Lambda → Security Group (secured-by, when VPC-attached)
func BuildGraph(resources []collector.Resource) *ResourceGraph {
	g := New()

	// Index all resources by both ARN-style ID and raw short ID.
	byID := make(map[string]*collector.Resource, len(resources))
	byRawID := make(map[string]*collector.Resource, len(resources))

	for i := range resources {
		r := &resources[i]
		byID[r.ID] = r
		if r.RawID != "" {
			byRawID[r.RawID] = r
		}
		g.AddNode(&Node{Resource: *r})
	}

	_ = byID // used in future Phase 2 rules

	// Apply each edge rule in order.
	rules := []edgeRule{
		ruleEBSToEC2,
		ruleEC2ToSG,
		ruleSubnetToVPC,
		ruleIGWToVPC,
		ruleEC2ToSubnet,
		ruleRDSToSG,
		ruleLambdaToSG,
	}
	for _, rule := range rules {
		rule(byRawID, g)
	}

	return g
}

// ── Edge rules ───────────────────────────────────────────────────────────────

// ruleEBSToEC2 draws an "attached-to" edge from each EBS volume to the EC2
// instance it is attached to, using the AttachedInstanceId metadata key.
func ruleEBSToEC2(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeEBSVolume) {
		attachedID := n.Resource.Metadata["AttachedInstanceId"]
		if attachedID == "" {
			continue
		}
		inst, ok := byRawID[attachedID]
		if !ok {
			continue
		}
		g.AddEdge(Edge{
			FromID:       n.Resource.ID,
			ToID:         inst.ID,
			Relationship: RelAttachedTo,
		})
	}
}

// ruleEC2ToSG draws a "secured-by" edge from each EC2 instance to each
// security group it references. The SecurityGroupIds metadata key holds a
// comma-separated list of raw SG IDs (e.g. "sg-abc,sg-def").
func ruleEC2ToSG(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeEC2Instance) {
		sgList := n.Resource.Metadata["SecurityGroupIds"]
		if sgList == "" {
			continue
		}
		for _, sgRawID := range strings.Split(sgList, ",") {
			sgRawID = strings.TrimSpace(sgRawID)
			sg, ok := byRawID[sgRawID]
			if !ok {
				continue
			}
			g.AddEdge(Edge{
				FromID:       n.Resource.ID,
				ToID:         sg.ID,
				Relationship: RelSecuredBy,
			})
		}
	}
}

// ruleEC2ToSubnet draws a "contains" edge from Subnet → EC2, expressing
// that the subnet contains the instance. Direction: subnet is parent.
func ruleEC2ToSubnet(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeEC2Instance) {
		subnetRawID := n.Resource.Metadata["SubnetId"]
		if subnetRawID == "" {
			continue
		}
		subnet, ok := byRawID[subnetRawID]
		if !ok {
			continue
		}
		g.AddEdge(Edge{
			FromID:       subnet.ID,
			ToID:         n.Resource.ID,
			Relationship: RelContains,
		})
	}
}

// ruleSubnetToVPC draws a "contains" edge from VPC → Subnet.
func ruleSubnetToVPC(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeSubnet) {
		vpcRawID := n.Resource.Metadata["VpcId"]
		if vpcRawID == "" {
			continue
		}
		vpc, ok := byRawID[vpcRawID]
		if !ok {
			continue
		}
		g.AddEdge(Edge{
			FromID:       vpc.ID,
			ToID:         n.Resource.ID,
			Relationship: RelContains,
		})
	}
}

// ruleIGWToVPC draws a "routes-via" edge from VPC → IGW, expressing
// that internet traffic routes through the gateway.
func ruleIGWToVPC(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeIGW) {
		vpcRawID := n.Resource.Metadata["AttachedVpcId"]
		if vpcRawID == "" {
			continue
		}
		vpc, ok := byRawID[vpcRawID]
		if !ok {
			continue
		}
		g.AddEdge(Edge{
			FromID:       vpc.ID,
			ToID:         n.Resource.ID,
			Relationship: RelRoutesVia,
		})
	}
}

// ruleRDSToSG draws "secured-by" edges from RDS instances and clusters to their
// security groups, using the comma-separated SecurityGroupIds metadata key.
func ruleRDSToSG(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, t := range []collector.ResourceType{collector.TypeRDSInstance, collector.TypeRDSCluster} {
		for _, n := range g.FilterByType(t) {
			sgList := n.Resource.Metadata["SecurityGroupIds"]
			if sgList == "" {
				continue
			}
			for _, sgRawID := range strings.Split(sgList, ",") {
				sgRawID = strings.TrimSpace(sgRawID)
				sg, ok := byRawID[sgRawID]
				if !ok {
					continue
				}
				g.AddEdge(Edge{
					FromID:       n.Resource.ID,
					ToID:         sg.ID,
					Relationship: RelSecuredBy,
				})
			}
		}
	}
}

// ruleLambdaToSG draws "secured-by" edges for VPC-attached Lambda functions.
func ruleLambdaToSG(byRawID map[string]*collector.Resource, g *ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeLambdaFunction) {
		sgList := n.Resource.Metadata["SecurityGroupIds"]
		if sgList == "" {
			continue
		}
		for _, sgRawID := range strings.Split(sgList, ",") {
			sgRawID = strings.TrimSpace(sgRawID)
			sg, ok := byRawID[sgRawID]
			if !ok {
				continue
			}
			g.AddEdge(Edge{
				FromID:       n.Resource.ID,
				ToID:         sg.ID,
				Relationship: RelSecuredBy,
			})
		}
	}
}

// rawIDFromARN extracts the resource short ID from an ARN-style string.
// e.g. "arn:aws:ec2:us-east-1:vpc/vpc-abc123" → "vpc-abc123"
// Returns "" if the input doesn't contain a "/".
func rawIDFromARN(arn string) string {
	parts := strings.Split(arn, "/")
	if len(parts) < 2 {
		return ""
	}
	return parts[len(parts)-1]
}

// buildARN constructs an ARN-style ID consistent with the format used by
// each collector, for use in edge rules that need to cross-reference IDs.
func buildARN(service, region, resourceType, rawID string) string {
	return fmt.Sprintf("arn:aws:%s:%s:%s/%s", service, region, resourceType, rawID)
}
