package graph

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the production Store backed by graph_nodes/graph_edges.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// ReplaceGraph deletes the project's existing graph and inserts the new
// one, all in a single transaction — repeated discovery must never
// accumulate stale or duplicate nodes.
func (s *PostgresStore) ReplaceGraph(ctx context.Context, projectID string, nodes []Node, edges []Edge) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("graph: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM graph_nodes WHERE project_id = $1`, projectID); err != nil {
		return fmt.Errorf("graph: clearing previous graph: %w", err)
	}

	idByKey := make(map[string]string, len(nodes))
	for _, n := range nodes {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO graph_nodes (project_id, node_type, label, source_reference, runtime_reference, metadata, confidence)
			VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6, $7)
			RETURNING id
		`, projectID, n.NodeType, n.Label, n.SourceReference, n.RuntimeReference, n.Metadata, n.Confidence).Scan(&id)
		if err != nil {
			return fmt.Errorf("graph: inserting node %s/%s: %w", n.NodeType, n.Label, err)
		}
		idByKey[n.Key()] = id
	}

	for _, e := range edges {
		sourceID, sok := idByKey[e.SourceKey]
		targetID, tok := idByKey[e.TargetKey]
		if !sok || !tok {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO graph_edges (project_id, source_node_id, target_node_id, relation_type, evidence, confidence)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, projectID, sourceID, targetID, e.RelationType, e.Evidence, e.Confidence); err != nil {
			return fmt.Errorf("graph: inserting edge %s -> %s: %w", e.SourceKey, e.TargetKey, err)
		}
	}

	return tx.Commit(ctx)
}

func (s *PostgresStore) Get(ctx context.Context, projectID string) ([]Node, []ResolvedEdge, error) {
	nodeRows, err := s.pool.Query(ctx, `
		SELECT id, node_type, label, COALESCE(source_reference, ''), COALESCE(runtime_reference, ''), metadata, confidence
		FROM graph_nodes WHERE project_id = $1 ORDER BY node_type, label
	`, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("graph: listing nodes: %w", err)
	}
	defer nodeRows.Close()

	var nodes []Node
	for nodeRows.Next() {
		var n Node
		if err := nodeRows.Scan(&n.ID, &n.NodeType, &n.Label, &n.SourceReference, &n.RuntimeReference, &n.Metadata, &n.Confidence); err != nil {
			return nil, nil, fmt.Errorf("graph: scanning node: %w", err)
		}
		n.ProjectID = projectID
		nodes = append(nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, nil, err
	}

	edgeRows, err := s.pool.Query(ctx, `
		SELECT e.id, e.source_node_id, e.target_node_id,
		       sn.node_type, sn.label, tn.node_type, tn.label,
		       e.relation_type, e.evidence, e.confidence
		FROM graph_edges e
		JOIN graph_nodes sn ON sn.id = e.source_node_id
		JOIN graph_nodes tn ON tn.id = e.target_node_id
		WHERE e.project_id = $1
	`, projectID)
	if err != nil {
		return nil, nil, fmt.Errorf("graph: listing edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []ResolvedEdge
	for edgeRows.Next() {
		var e ResolvedEdge
		if err := edgeRows.Scan(&e.ID, &e.SourceNodeID, &e.TargetNodeID, &e.SourceNodeType, &e.SourceLabel, &e.TargetNodeType, &e.TargetLabel, &e.RelationType, &e.Evidence, &e.Confidence); err != nil {
			return nil, nil, fmt.Errorf("graph: scanning edge: %w", err)
		}
		edges = append(edges, e)
	}
	return nodes, edges, edgeRows.Err()
}
