package graph

import (
	"context"
	"fmt"
	"sync"
)

// MemoryStore is an in-process Store for tests. Not for production use.
type MemoryStore struct {
	mu    sync.Mutex
	nodes map[string][]Node // projectID -> nodes (with IDs assigned)
	edges map[string][]ResolvedEdge
	next  int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{nodes: map[string][]Node{}, edges: map[string][]ResolvedEdge{}}
}

func (s *MemoryStore) ReplaceGraph(_ context.Context, projectID string, nodes []Node, edges []Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	idByKey := map[string]string{}
	storedNodes := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		s.next++
		n.ID = fmt.Sprintf("mem-node-%d", s.next)
		n.ProjectID = projectID
		idByKey[n.Key()] = n.ID
		storedNodes = append(storedNodes, n)
	}

	byID := map[string]Node{}
	for _, n := range storedNodes {
		byID[n.ID] = n
	}

	storedEdges := make([]ResolvedEdge, 0, len(edges))
	for _, e := range edges {
		sourceID, sok := idByKey[e.SourceKey]
		targetID, tok := idByKey[e.TargetKey]
		if !sok || !tok {
			continue // referenced a node that wasn't in this build (defensive)
		}
		s.next++
		source := byID[sourceID]
		target := byID[targetID]
		storedEdges = append(storedEdges, ResolvedEdge{
			ID: fmt.Sprintf("mem-edge-%d", s.next), SourceNodeID: sourceID, TargetNodeID: targetID,
			SourceNodeType: source.NodeType, SourceLabel: source.Label,
			TargetNodeType: target.NodeType, TargetLabel: target.Label,
			RelationType: e.RelationType, Evidence: e.Evidence, Confidence: e.Confidence,
		})
	}

	s.nodes[projectID] = storedNodes
	s.edges[projectID] = storedEdges
	return nil
}

func (s *MemoryStore) Get(_ context.Context, projectID string) ([]Node, []ResolvedEdge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodes[projectID], s.edges[projectID], nil
}
