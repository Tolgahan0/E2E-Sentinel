package graph

import (
	"context"
	"testing"
)

func TestMemoryStore_ReplaceGraphResolvesEdgesToRealIDs(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	nodes := []Node{
		{NodeType: "page", Label: "/login", Confidence: ConfidenceHigh},
		{NodeType: "api", Label: "POST /api/v1/auth/login", Confidence: ConfidenceHigh},
	}
	edges := []Edge{
		{SourceKey: nodes[0].Key(), TargetKey: nodes[1].Key(), RelationType: RelationCalls, Confidence: ConfidenceMedium, Evidence: map[string]any{"file": "page.tsx"}},
	}

	if err := store.ReplaceGraph(ctx, "proj-1", nodes, edges); err != nil {
		t.Fatalf("ReplaceGraph() error: %v", err)
	}

	gotNodes, gotEdges, err := store.Get(ctx, "proj-1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if len(gotNodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(gotNodes))
	}
	if len(gotEdges) != 1 {
		t.Fatalf("got %d edges, want 1", len(gotEdges))
	}

	edge := gotEdges[0]
	if edge.SourceNodeID == "" || edge.TargetNodeID == "" {
		t.Fatalf("edge has unresolved node IDs: %+v", edge)
	}
	if edge.SourceLabel != "/login" || edge.TargetLabel != "POST /api/v1/auth/login" {
		t.Errorf("edge labels = %+v, want source=/login target=POST /api/v1/auth/login", edge)
	}
}

func TestMemoryStore_ReplaceGraphIsIdempotentPerProject(t *testing.T) {
	store := NewMemoryStore()
	ctx := context.Background()

	nodes := []Node{{NodeType: "page", Label: "/home", Confidence: ConfidenceHigh}}
	if err := store.ReplaceGraph(ctx, "proj-1", nodes, nil); err != nil {
		t.Fatalf("first ReplaceGraph() error: %v", err)
	}
	if err := store.ReplaceGraph(ctx, "proj-1", nodes, nil); err != nil {
		t.Fatalf("second ReplaceGraph() error: %v", err)
	}

	gotNodes, _, err := store.Get(ctx, "proj-1")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if len(gotNodes) != 1 {
		t.Fatalf("got %d nodes after two replaces, want 1 (no accumulation)", len(gotNodes))
	}
}

func TestMemoryStore_GetUnknownProjectReturnsEmpty(t *testing.T) {
	store := NewMemoryStore()
	nodes, edges, err := store.Get(context.Background(), "does-not-exist")
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("expected empty graph for unknown project, got nodes=%v edges=%v", nodes, edges)
	}
}
