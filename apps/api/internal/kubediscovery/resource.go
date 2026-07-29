// Package kubediscovery turns a Kubernetes cluster's live state (via
// internal/kubeclient) into Resource records for a project — the
// Kubernetes analogue of internal/services' Docker Compose discovery
// (spec §7.5). It never mutates the cluster: Discover issues only the
// read-only List calls kubeclient exposes.
package kubediscovery

import (
	"context"
	"time"
)

// Kind values, per spec §7.5's detection list.
const (
	KindNamespace   = "namespace"
	KindDeployment  = "deployment"
	KindStatefulSet = "statefulset"
	KindDaemonSet   = "daemonset"
	KindJob         = "job"
	KindCronJob     = "cronjob"
	KindService     = "service"
	KindIngress     = "ingress"
	KindGateway     = "gateway"
	KindConfigMap   = "configmap"
	KindSecret      = "secret"
)

// Status values. Only kinds with a meaningful replica/pod health concept
// (Deployment, StatefulSet, DaemonSet, Job) get Healthy/Degraded — every
// other kind is StatusNotApplicable, never guessed at.
const (
	StatusHealthy       = "healthy"
	StatusDegraded      = "degraded"
	StatusUnknown       = "unknown"
	StatusNotApplicable = "not_applicable"
)

// Resource is one discovered Kubernetes object.
type Resource struct {
	ID              string
	ProjectID       string
	Namespace       string
	Kind            string
	Name            string
	DesiredReplicas *int32
	ReadyReplicas   *int32
	RestartCount    *int32
	Status          string
	Metadata        map[string]any
	LastSeenAt      time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Store persists discovered Kubernetes resources, upserting by
// (project_id, namespace, kind, name) so repeated discovery updates in
// place — the same idempotency convention as internal/services.Store.
type Store interface {
	Upsert(ctx context.Context, resource Resource) (Resource, error)
	ListByProject(ctx context.Context, projectID string) ([]Resource, error)
}
