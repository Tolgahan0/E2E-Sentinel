package httpserver

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type graphNodeResponse struct {
	ID               string         `json:"id"`
	NodeType         string         `json:"node_type"`
	Label            string         `json:"label"`
	SourceReference  string         `json:"source_reference"`
	RuntimeReference string         `json:"runtime_reference"`
	Metadata         map[string]any `json:"metadata"`
	Confidence       string         `json:"confidence"`
}

type graphEdgeResponse struct {
	ID             string         `json:"id"`
	SourceNodeID   string         `json:"source_node_id"`
	TargetNodeID   string         `json:"target_node_id"`
	SourceNodeType string         `json:"source_node_type"`
	SourceLabel    string         `json:"source_label"`
	TargetNodeType string         `json:"target_node_type"`
	TargetLabel    string         `json:"target_label"`
	RelationType   string         `json:"relation_type"`
	Evidence       map[string]any `json:"evidence"`
	Confidence     string         `json:"confidence"`
}

func handleGetGraph(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodes, edges, err := deps.Graph.Get(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("getting application graph failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		nodeOut := make([]graphNodeResponse, 0, len(nodes))
		for _, n := range nodes {
			nodeOut = append(nodeOut, graphNodeResponse{
				ID: n.ID, NodeType: n.NodeType, Label: n.Label, SourceReference: n.SourceReference,
				RuntimeReference: n.RuntimeReference, Metadata: n.Metadata, Confidence: n.Confidence,
			})
		}

		edgeOut := make([]graphEdgeResponse, 0, len(edges))
		for _, e := range edges {
			edgeOut = append(edgeOut, graphEdgeResponse{
				ID: e.ID, SourceNodeID: e.SourceNodeID, TargetNodeID: e.TargetNodeID,
				SourceNodeType: e.SourceNodeType, SourceLabel: e.SourceLabel,
				TargetNodeType: e.TargetNodeType, TargetLabel: e.TargetLabel,
				RelationType: e.RelationType, Evidence: e.Evidence, Confidence: e.Confidence,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodeOut, "edges": edgeOut})
	}
}
