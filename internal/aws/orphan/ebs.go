package orphan

// ebs.go detects EBS volumes that are not attached to any EC2 instance.
// An unattached volume has state == "available" in AWS.

import (
	"github.com/angsak/mbr/internal/aws/collector"
	"github.com/angsak/mbr/internal/aws/graph"
)

// init registers the UnattachedEBSRule with the package-level Rules slice.
func init() {
	Rules = append(Rules, UnattachedEBSRule{})
}

// UnattachedEBSRule flags EBS volumes whose state is "available" AND that
// have no inbound "attached-to" edges from an EC2 instance.
// Such volumes accrue storage costs without serving any workload.
type UnattachedEBSRule struct{}

// Name implements OrphanRule.
func (r UnattachedEBSRule) Name() string { return "Unattached EBS Volume" }

// AppliesTo implements OrphanRule.
func (r UnattachedEBSRule) AppliesTo() collector.ResourceType {
	return collector.TypeEBSVolume
}

// Detect iterates over all EBS volume nodes. A node is flagged as orphaned
// when its AWS state is "available" (meaning AWS itself considers it detached)
// AND the graph has no inbound edges pointing to it (confirming no EC2 is
// referencing it in our collected data).
func (r UnattachedEBSRule) Detect(g *graph.ResourceGraph) {
	for _, node := range g.FilterByType(collector.TypeEBSVolume) {
		state := node.Resource.Metadata["State"]
		if state != "available" {
			// Volume is in-use, creating, or in another transient state.
			continue
		}

		// Double-check via the graph: no dependents means no EC2 has an edge to it.
		if len(g.Dependents(node.Resource.ID)) == 0 {
			node.IsOrphan = true
			node.OrphanReasons = append(node.OrphanReasons,
				"EBS volume is available (not attached to any instance)")
		}
	}
}
