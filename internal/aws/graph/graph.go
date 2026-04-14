// Package graph provides the in-memory resource dependency graph that is
// built after all collectors have run. It is the central data structure
// used by the orphan detector, TUI views, and cost attribution.
package graph

import (
	"sort"
	"sync"

	"github.com/angsak/mbr/internal/aws/collector"
)

// Relationship describes the semantic of a directed edge between two nodes.
// The TUI graph renderer uses this to colour-code and label edges.
type Relationship string

const (
	// RelContains expresses hierarchical ownership: VPC→Subnet, ASG→EC2.
	RelContains Relationship = "contains"

	// RelAttachedTo expresses physical attachment: EBS→EC2, ENI→EC2.
	RelAttachedTo Relationship = "attached-to"

	// RelSecuredBy expresses firewall association: EC2→SG, RDS→SG.
	RelSecuredBy Relationship = "secured-by"

	// RelRoutesVia expresses network routing: Subnet→IGW.
	RelRoutesVia Relationship = "routes-via"

	// RelBalances expresses load balancer target membership: ELB→EC2.
	RelBalances Relationship = "balances"

	// RelInvokes expresses invocation: EventBridge→Lambda (Phase 3).
	RelInvokes Relationship = "invokes"

	// RelBackedBy expresses cluster membership: RDS Cluster→RDS Instance.
	RelBackedBy Relationship = "backed-by"
)

// Node wraps a collector.Resource with graph-computed metadata.
// Fields like IsOrphan and CostUSD are populated after graph construction
// by the orphan detector and cost packages respectively.
type Node struct {
	// Resource is the underlying AWS resource from the collector.
	Resource collector.Resource

	// InDegree is the number of edges pointing into this node.
	// Updated automatically by AddEdge.
	InDegree int

	// OutDegree is the number of edges leaving this node.
	// Updated automatically by AddEdge.
	OutDegree int

	// CostUSD is the 30-day blended cost in USD.
	// Populated lazily by the cost package when the user selects a resource.
	CostUSD float64

	// IsOrphan is set to true by the orphan detector when the node matches
	// one or more orphan rules.
	IsOrphan bool

	// OrphanReasons lists human-readable reasons this node was flagged.
	// Multiple orphan rules can append to this slice independently.
	OrphanReasons []string

	// DangerScore is a 0–100 composite score: 60% cost percentile + 40% orphan.
	// Computed by ComputeDangerScores after cost data is available.
	DangerScore int
}

// Edge is a typed directed relationship from one node to another.
type Edge struct {
	// FromID and ToID are collector.Resource.ID values (ARN-style).
	FromID string
	ToID   string

	// Relationship describes the semantic of this edge.
	Relationship Relationship
}

// ResourceGraph is the in-memory directed graph. It is built once per scan
// by graph/builder.go and treated as read-only after construction.
// All methods are safe for concurrent read access; write methods use a mutex.
type ResourceGraph struct {
	mu sync.RWMutex

	// nodes maps Resource.ID → *Node for O(1) lookup.
	nodes map[string]*Node

	// adjacency maps FromID → []Edge for forward (outbound) traversal.
	adjacency map[string][]Edge

	// reverseAdj maps ToID → []Edge for reverse (inbound) traversal,
	// enabling "who depends on this resource?" queries.
	reverseAdj map[string][]Edge
}

// New returns an empty ResourceGraph ready for population.
func New() *ResourceGraph {
	return &ResourceGraph{
		nodes:      make(map[string]*Node),
		adjacency:  make(map[string][]Edge),
		reverseAdj: make(map[string][]Edge),
	}
}

// AddNode inserts n into the graph, keyed by n.Resource.ID.
// A second call with the same ID overwrites the first.
func (g *ResourceGraph) AddNode(n *Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.nodes[n.Resource.ID] = n
}

// AddEdge inserts a directed edge and updates degree counters on both endpoints.
// It silently ignores edges whose FromID or ToID are not present as nodes.
func (g *ResourceGraph) AddEdge(e Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()

	from, fromOK := g.nodes[e.FromID]
	to, toOK := g.nodes[e.ToID]
	if !fromOK || !toOK {
		return
	}

	g.adjacency[e.FromID] = append(g.adjacency[e.FromID], e)
	g.reverseAdj[e.ToID] = append(g.reverseAdj[e.ToID], e)

	from.OutDegree++
	to.InDegree++
}

// Node returns the *Node for the given resource ID and a boolean indicating
// whether it was found.
func (g *ResourceGraph) Node(id string) (*Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

// Neighbours returns the nodes directly reachable from id (forward edges),
// sorted by resource ID for stable display.
func (g *ResourceGraph) Neighbours(id string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := g.adjacency[id]
	out := make([]*Node, 0, len(edges))
	for _, e := range edges {
		if n, ok := g.nodes[e.ToID]; ok {
			out = append(out, n)
		}
	}
	sortNodes(out)
	return out
}

// Dependents returns the nodes that point to id (reverse edges),
// i.e. "what other resources reference this one?".
func (g *ResourceGraph) Dependents(id string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	edges := g.reverseAdj[id]
	out := make([]*Node, 0, len(edges))
	for _, e := range edges {
		if n, ok := g.nodes[e.FromID]; ok {
			out = append(out, n)
		}
	}
	sortNodes(out)
	return out
}

// AllNodes returns every node in the graph in a stable (sorted by ID) order.
func (g *ResourceGraph) AllNodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sortNodes(out)
	return out
}

// FilterByType returns all nodes of the given ResourceType, sorted by ID.
func (g *ResourceGraph) FilterByType(rt collector.ResourceType) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var out []*Node
	for _, n := range g.nodes {
		if n.Resource.Type == rt {
			out = append(out, n)
		}
	}
	sortNodes(out)
	return out
}

// Orphans returns all nodes where IsOrphan == true, sorted by ID.
func (g *ResourceGraph) Orphans() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var out []*Node
	for _, n := range g.nodes {
		if n.IsOrphan {
			out = append(out, n)
		}
	}
	sortNodes(out)
	return out
}

// Len returns the total number of nodes in the graph.
func (g *ResourceGraph) Len() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

// EdgesFrom returns all outbound edges from the given node ID.
func (g *ResourceGraph) EdgesFrom(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return append([]Edge(nil), g.adjacency[id]...)
}

// ComputeDangerScores assigns a DangerScore (0–100) to every node.
// The score is: 40 points if IsOrphan, plus up to 60 points proportional
// to CostUSD relative to the highest-cost node in the graph.
// Call this after cost data has been populated.
func (g *ResourceGraph) ComputeDangerScores() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Find maximum cost to normalise the cost component.
	maxCost := 0.0
	for _, n := range g.nodes {
		if n.CostUSD > maxCost {
			maxCost = n.CostUSD
		}
	}

	for _, n := range g.nodes {
		score := 0
		if n.IsOrphan {
			score += 40
		}
		if maxCost > 0 {
			score += int((n.CostUSD / maxCost) * 60)
		}
		n.DangerScore = score
	}
}

// sortNodes sorts a []*Node slice in place by Resource.ID for stable output.
func sortNodes(nodes []*Node) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].Resource.ID < nodes[j].Resource.ID
	})
}
