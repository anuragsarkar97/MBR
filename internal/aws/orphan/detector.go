// Package orphan implements rules that detect unused ("orphaned") AWS resources.
//
// Extension pattern: create a new file in this package, define a struct
// implementing OrphanRule, and append it to Rules in an init() function.
// RunAll will pick it up automatically.
package orphan

import (
	"github.com/anuragsarkar97/mbr/internal/aws/collector"
	"github.com/anuragsarkar97/mbr/internal/aws/graph"
)

// OrphanRule examines the resource graph and marks nodes as orphans.
// Each rule is responsible for exactly one resource type and one condition.
// Rules must be idempotent and must only modify IsOrphan and OrphanReasons
// on the target node — never on any other node.
type OrphanRule interface {
	// Name returns the human-readable rule name shown in the TUI orphan view.
	Name() string

	// AppliesTo returns the ResourceType this rule targets.
	AppliesTo() collector.ResourceType

	// Detect scans g and sets node.IsOrphan = true on matching nodes,
	// appending a reason string to node.OrphanReasons.
	Detect(g *graph.ResourceGraph)
}

// Rules is the package-level registry of all orphan detection rules.
// Each rule file appends to this slice in its init() function.
var Rules []OrphanRule

// RunAll applies every registered OrphanRule to the graph in order.
// It is safe to call multiple times — rules are idempotent.
func RunAll(g *graph.ResourceGraph) {
	for _, rule := range Rules {
		rule.Detect(g)
	}
}
