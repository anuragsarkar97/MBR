package orphan

// database.go defines orphan-detection rules for database resource types.

import (
	"github.com/angsak/mbr/internal/aws/collector"
	"github.com/angsak/mbr/internal/aws/graph"
)

func init() {
	Rules = append(Rules,
		rdsStoppedRule{},
		elastiCacheNotAvailableRule{},
	)
}

// ── RDS: stopped instances ────────────────────────────────────────────────────

// rdsStoppedRule flags RDS instances whose status is "stopped".
// AWS still charges storage costs for stopped instances; they are orphan
// candidates unless the owner intentionally stopped them temporarily.
type rdsStoppedRule struct{}

func (r rdsStoppedRule) Name() string                        { return "rds-stopped" }
func (r rdsStoppedRule) AppliesTo() collector.ResourceType   { return collector.TypeRDSInstance }
func (r rdsStoppedRule) Detect(g *graph.ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeRDSInstance) {
		if n.Resource.Metadata["Status"] == "stopped" {
			n.IsOrphan = true
			n.OrphanReasons = append(n.OrphanReasons,
				"RDS instance is stopped (storage costs still accrue)")
		}
	}
}

// ── ElastiCache: non-available clusters ───────────────────────────────────────

// elastiCacheNotAvailableRule flags ElastiCache clusters not in "available"
// state — these are either failed, creating, or otherwise not serving traffic.
type elastiCacheNotAvailableRule struct{}

func (r elastiCacheNotAvailableRule) Name() string                        { return "elasticache-not-available" }
func (r elastiCacheNotAvailableRule) AppliesTo() collector.ResourceType   { return collector.TypeElastiCache }
func (r elastiCacheNotAvailableRule) Detect(g *graph.ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeElastiCache) {
		status := n.Resource.Metadata["Status"]
		if status != "" && status != "available" {
			n.IsOrphan = true
			n.OrphanReasons = append(n.OrphanReasons,
				"ElastiCache cluster status is "+status+" (not serving traffic)")
		}
	}
}
