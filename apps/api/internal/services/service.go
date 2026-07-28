// Package services manages the DiscoveredService entity (spec §6.3):
// services found via Docker Compose file parsing, optionally enriched
// with live status from the Docker daemon when it's reachable.
package services

import (
	"context"
	"strings"
	"time"

	"e2e-sentinel/apps/api/internal/compose"
)

// Kind values, per spec §6.3. Left open-ended (plain strings) so a
// heuristic miss never blocks storing the underlying evidence.
const (
	KindWeb      = "web"
	KindAPI      = "api"
	KindWorker   = "worker"
	KindDatabase = "database"
	KindCache    = "cache"
	KindQueue    = "queue"
	KindProxy    = "proxy"
	KindUnknown  = "unknown"
)

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
)

// Service is a discovered service, matching spec §6.3.
type Service struct {
	ID              string
	ProjectID       string
	Name            string
	Kind            string
	Runtime         string
	SourcePath      string
	ContainerName   string
	Image           string
	Ports           []string
	Dependencies    []string
	Metadata        map[string]any
	ConfidenceLevel string
	LastSeenAt      time.Time
}

// Store persists discovered services, upserting by (project_id, name)
// so repeated discovery updates in place rather than duplicating (spec
// §7.1 idempotency expectation, applied here the same way Phase 1 does
// for repository findings).
type Store interface {
	Upsert(ctx context.Context, svc Service) (Service, error)
	ListByProject(ctx context.Context, projectID string) ([]Service, error)
}

// imageKindMarkers maps a substring of an image name to the kind it
// implies. Checked in order; first match wins.
var imageKindMarkers = []struct {
	marker string
	kind   string
}{
	{"postgres", KindDatabase}, {"mysql", KindDatabase}, {"mariadb", KindDatabase}, {"mongo", KindDatabase},
	{"redis", KindCache}, {"memcached", KindCache},
	{"kafka", KindQueue}, {"rabbitmq", KindQueue}, {"nats", KindQueue},
	{"nginx", KindProxy}, {"traefik", KindProxy}, {"envoy", KindProxy}, {"haproxy", KindProxy},
}

// ClassifyKind infers a service's kind from its compose declaration.
// Image-name matches are high confidence (an explicit, well-known image
// name is strong evidence); everything else is a coarse, medium-
// confidence guess based only on whether a port is exposed — never
// presented as more certain than that (spec §8.2, §9.4).
func ClassifyKind(svc compose.Service) (kind, confidence string) {
	lowerImage := strings.ToLower(svc.Image)
	for _, m := range imageKindMarkers {
		if strings.Contains(lowerImage, m.marker) {
			return m.kind, ConfidenceHigh
		}
	}

	if len(svc.Ports) > 0 {
		return KindAPI, ConfidenceMedium
	}
	if svc.HasBuild || svc.Image != "" {
		return KindWorker, ConfidenceMedium
	}
	return KindUnknown, ConfidenceMedium
}

// FromCompose builds a Service from a parsed compose.Service, without
// any live Docker daemon information.
func FromCompose(projectID, sourcePath string, svc compose.Service) Service {
	kind, confidence := ClassifyKind(svc)
	return Service{
		ProjectID:       projectID,
		Name:            svc.Name,
		Kind:            kind,
		Runtime:         "docker",
		SourcePath:      sourcePath,
		Image:           svc.Image,
		Ports:           svc.Ports,
		Dependencies:    svc.DependsOn,
		ConfidenceLevel: confidence,
		Metadata: map[string]any{
			"env_var_names": svc.EnvVarNames,
			"profiles":      svc.Profiles,
			"has_build":     svc.HasBuild,
			"networks":      svc.Networks,
			"status":        "unknown", // overwritten by ApplyRuntimeStatus when the daemon is reachable
		},
	}
}

// ApplyRuntimeStatus enriches svc with live container information,
// matched by the Docker Compose service label. Called only when the
// Docker daemon was reachable; svc.Metadata["status"] stays "unknown"
// otherwise, which the UI must render as "not observed", never as "not
// running" — those are different claims (spec §9.4 distinguishes
// discovered vs. observed coverage).
func ApplyRuntimeStatus(svc Service, containerName, state, statusText string, ports []string) Service {
	svc.ContainerName = containerName
	svc.Metadata["status"] = state
	svc.Metadata["status_text"] = statusText
	if len(ports) > 0 {
		svc.Ports = ports
	}
	svc.LastSeenAt = time.Now()
	return svc
}
