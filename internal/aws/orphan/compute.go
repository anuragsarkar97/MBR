package orphan

// compute.go defines orphan-detection rules for compute resource types.

import (
	"strconv"

	"github.com/anuragsarkar97/mbr/internal/aws/collector"
	"github.com/anuragsarkar97/mbr/internal/aws/graph"
)

func init() {
	Rules = append(Rules,
		elbv2NotActiveRule{},
		elbClassicNoInstancesRule{},
		asgZeroDesiredRule{},
	)
}

// ── ELB v2: not active ────────────────────────────────────────────────────────

type elbv2NotActiveRule struct{}

func (r elbv2NotActiveRule) Name() string                      { return "elbv2-not-active" }
func (r elbv2NotActiveRule) AppliesTo() collector.ResourceType { return collector.TypeELBV2 }
func (r elbv2NotActiveRule) Detect(g *graph.ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeELBV2) {
		state := n.Resource.Metadata["State"]
		if state != "" && state != "active" {
			n.IsOrphan = true
			n.OrphanReasons = append(n.OrphanReasons,
				"Load balancer state is "+state+" (not active)")
		}
	}
}

// ── ELB Classic: no instances ─────────────────────────────────────────────────

type elbClassicNoInstancesRule struct{}

func (r elbClassicNoInstancesRule) Name() string { return "elb-no-instances" }
func (r elbClassicNoInstancesRule) AppliesTo() collector.ResourceType {
	return collector.TypeELBClassic
}
func (r elbClassicNoInstancesRule) Detect(g *graph.ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeELBClassic) {
		count, _ := strconv.Atoi(n.Resource.Metadata["InstanceCount"])
		if count == 0 {
			n.IsOrphan = true
			n.OrphanReasons = append(n.OrphanReasons,
				"Classic ELB has no registered instances")
		}
	}
}

// ── ASG: desired capacity is zero ─────────────────────────────────────────────

type asgZeroDesiredRule struct{}

func (r asgZeroDesiredRule) Name() string                      { return "asg-zero-desired" }
func (r asgZeroDesiredRule) AppliesTo() collector.ResourceType { return collector.TypeASG }
func (r asgZeroDesiredRule) Detect(g *graph.ResourceGraph) {
	for _, n := range g.FilterByType(collector.TypeASG) {
		desired, _ := strconv.Atoi(n.Resource.Metadata["DesiredCapacity"])
		if desired == 0 {
			n.IsOrphan = true
			n.OrphanReasons = append(n.OrphanReasons,
				"Auto Scaling Group desired capacity is 0 (no instances running)")
		}
	}
}
