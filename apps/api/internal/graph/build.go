package graph

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"e2e-sentinel/apps/api/internal/routes"
	"e2e-sentinel/apps/api/internal/services"
)

// Build correlates extracted routes and discovered services into an
// Application Graph. root is the (already-validated) project repository
// root, needed to read page source files when looking for fetch/axios
// calls that reveal a "calls" edge to an API endpoint.
func Build(root string, extractedRoutes []routes.Route, discoveredServices []services.Service) ([]Node, []Edge) {
	var nodes []Node

	for _, r := range extractedRoutes {
		n := Node{
			NodeType:        r.Kind,
			Label:           routeLabel(r),
			SourceReference: r.SourcePath,
			Confidence:      r.Confidence,
			Metadata:        map[string]any{"method": r.Method, "path": r.Path},
		}
		nodes = append(nodes, n)
	}

	serviceNodeByName := map[string]Node{}
	for _, s := range discoveredServices {
		n := Node{
			NodeType:         "service",
			Label:            s.Name,
			SourceReference:  s.SourcePath,
			RuntimeReference: s.ContainerName,
			Confidence:       s.ConfidenceLevel,
			Metadata:         map[string]any{"kind": s.Kind, "image": s.Image},
		}
		nodes = append(nodes, n)
		serviceNodeByName[s.Name] = n
	}

	var edges []Edge

	// depends_on: explicit compose declaration, high confidence.
	for _, s := range discoveredServices {
		source, ok := serviceNodeByName[s.Name]
		if !ok {
			continue
		}
		for _, dep := range s.Dependencies {
			target, ok := serviceNodeByName[dep]
			if !ok {
				continue
			}
			edges = append(edges, Edge{
				SourceKey: source.Key(), TargetKey: target.Key(), RelationType: RelationDependsOn,
				Confidence: ConfidenceHigh,
				Evidence:   map[string]any{"source": "docker-compose depends_on"},
			})
		}
	}

	// served_by: only when exactly one "application" service exists —
	// otherwise which endpoint belongs to which service is genuinely
	// ambiguous, and a wrong guess is worse than no edge (spec §8.2).
	if appService, ok := singleApplicationService(discoveredServices); ok {
		serviceNode := serviceNodeByName[appService.Name]
		for _, r := range extractedRoutes {
			// A route with no HTTP method is a browser page, not an API
			// endpoint a service serves — regardless of its Kind
			// sub-classification (e.g. a login page is Kind=auth, not
			// Kind=page, but still has no method).
			if r.Method == "" {
				continue
			}
			edges = append(edges, Edge{
				SourceKey: routeLabelKey(r), TargetKey: serviceNode.Key(),
				RelationType: RelationServedBy, Confidence: ConfidenceMedium,
				Evidence: map[string]any{"reason": "exactly one application service discovered"},
			})
		}
	}

	// calls: page source scanned for literal fetch()/axios() URLs that
	// match a known route path — medium confidence, since string
	// matching can't fully resolve a templated URL.
	pathIndex := map[string][]routes.Route{}
	for _, r := range extractedRoutes {
		pathIndex[r.Path] = append(pathIndex[r.Path], r)
	}
	for _, page := range extractedRoutes {
		if page.Method != "" {
			continue // has a method, so it's an API endpoint, not a browser page
		}
		for _, calledPath := range extractCalledPaths(filepath.Join(root, page.SourcePath)) {
			for _, target := range pathIndex[calledPath] {
				if target.Method == "" {
					continue // don't link a page to another page
				}
				edges = append(edges, Edge{
					SourceKey: routeLabelKey(page), TargetKey: routeLabelKey(target),
					RelationType: RelationCalls, Confidence: ConfidenceMedium,
					Evidence: map[string]any{"file": page.SourcePath, "matched_path": calledPath},
				})
			}
		}
	}

	return nodes, edges
}

func routeLabel(r routes.Route) string {
	if r.Method == "" {
		return r.Path
	}
	return r.Method + " " + r.Path
}

func routeLabelKey(r routes.Route) string {
	return (Node{NodeType: r.Kind, Label: routeLabel(r)}).Key()
}

// singleApplicationService returns the sole non-infrastructure service
// (i.e. not a database/cache/queue/proxy), if there is exactly one.
func singleApplicationService(all []services.Service) (services.Service, bool) {
	infra := map[string]bool{
		services.KindDatabase: true, services.KindCache: true, services.KindQueue: true, services.KindProxy: true,
	}
	var candidates []services.Service
	for _, s := range all {
		if !infra[s.Kind] {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return services.Service{}, false
}

var fetchCallRe = regexp.MustCompile(`(?:fetch|axios(?:\.\w+)?)\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)

func extractCalledPaths(fullPath string) []string {
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range fetchCallRe.FindAllStringSubmatch(string(data), -1) {
		called := m[1]
		if strings.HasPrefix(called, "http://") || strings.HasPrefix(called, "https://") {
			continue // external URL, not this repository's own API
		}
		if idx := strings.IndexByte(called, '?'); idx >= 0 {
			called = called[:idx]
		}
		out = append(out, called)
	}
	return out
}
