// Package graph builds and stores the Application Graph (spec §8):
// nodes for routes/endpoints/services, edges connecting them, each edge
// carrying evidence and a confidence level. Low-confidence edges are
// never presented as confirmed dependencies (spec §8.2).
package graph

import "context"

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
)

// Node is one Application Graph node (spec §6.4).
type Node struct {
	ID               string
	ProjectID        string
	NodeType         string // route kind (page/api/health/auth/admin/webhook) or "service"
	Label            string
	SourceReference  string
	RuntimeReference string
	Metadata         map[string]any
	Confidence       string
}

// Key identifies a node before it has a database ID — used to resolve
// edge endpoints at build time, when nodes haven't been inserted yet.
func (n Node) Key() string { return n.NodeType + "|" + n.Label }

// Edge is one Application Graph edge (spec §6.5). SourceKey/TargetKey
// are Node.Key() values, resolved to real node IDs by the Store when
// the graph is persisted.
type Edge struct {
	ID           string
	ProjectID    string
	SourceKey    string
	TargetKey    string
	RelationType string
	Evidence     map[string]any
	Confidence   string
}

// Relation types.
const (
	RelationCalls     = "calls"      // page/route -> api_endpoint
	RelationServedBy  = "served_by"  // api_endpoint -> service
	RelationDependsOn = "depends_on" // service -> service
)

// ResolvedNode and ResolvedEdge are what Get returns: real IDs, no keys.
type ResolvedEdge struct {
	ID             string
	SourceNodeID   string
	TargetNodeID   string
	SourceNodeType string
	SourceLabel    string
	TargetNodeType string
	TargetLabel    string
	RelationType   string
	Evidence       map[string]any
	Confidence     string
}

// Store persists the graph, replacing a project's full node/edge set on
// every write — repeated discovery must not accumulate stale or
// duplicate nodes (spec §7.1's idempotency principle, applied here).
type Store interface {
	ReplaceGraph(ctx context.Context, projectID string, nodes []Node, edges []Edge) error
	Get(ctx context.Context, projectID string) ([]Node, []ResolvedEdge, error)
}
