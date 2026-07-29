package httpserver

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"

	"github.com/go-chi/chi/v5"

	"e2e-sentinel/apps/api/internal/audit"
	"e2e-sentinel/apps/api/internal/kubeclient"
	"e2e-sentinel/apps/api/internal/kubediscovery"
	"e2e-sentinel/apps/api/internal/projects"
)

// KubeAPI is everything the HTTP layer needs from a Kubernetes
// connection: kubediscovery.API's list methods (for the discovery pass)
// plus live, non-persisted event/log reads. *kubeclient.Client
// implements this; tests substitute a fake, the same pattern as
// DockerLister.
type KubeAPI interface {
	kubediscovery.API
	ListEvents(ctx context.Context, namespace string) ([]kubeclient.Event, error)
	PodLogs(ctx context.Context, namespace, pod, container string, tailLines int) (string, error)
}

// handleKubeStatus is always reachable, unauthenticated — mirrors
// handleAuthStatus so the web panel can decide whether to show
// Kubernetes discovery UI at all.
func handleKubeStatus(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"configured": deps.Kube != nil,
			"namespace":  deps.KubeNamespace,
		})
	}
}

type kubeResourceResponse struct {
	ID              string         `json:"id"`
	Namespace       string         `json:"namespace"`
	Kind            string         `json:"kind"`
	Name            string         `json:"name"`
	DesiredReplicas *int32         `json:"desired_replicas"`
	ReadyReplicas   *int32         `json:"ready_replicas"`
	RestartCount    *int32         `json:"restart_count"`
	Status          string         `json:"status"`
	Metadata        map[string]any `json:"metadata"`
	LastSeenAt      string         `json:"last_seen_at"`
}

func toKubeResourceResponse(r kubediscovery.Resource) kubeResourceResponse {
	out := kubeResourceResponse{
		ID: r.ID, Namespace: r.Namespace, Kind: r.Kind, Name: r.Name,
		DesiredReplicas: r.DesiredReplicas, ReadyReplicas: r.ReadyReplicas, RestartCount: r.RestartCount,
		Status: r.Status, Metadata: r.Metadata,
	}
	if !r.LastSeenAt.IsZero() {
		out.LastSeenAt = r.LastSeenAt.Format(timeFormat)
	}
	return out
}

// handleDiscoverKube runs kubediscovery.Discover against the configured
// cluster and upserts every resource found, scoped to this project — the
// Kubernetes analogue of handleDiscoverProject's Docker Compose service
// discovery. A partial failure on one resource kind (RBAC-restricted, a
// missing CRD) never fails the whole request; it's surfaced as a warning.
func handleDiscoverKube(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")

		if _, err := deps.Projects.Get(r.Context(), projectID); errors.Is(err, projects.ErrNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "project_not_found"})
			return
		}

		if deps.Kube == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error":  "kubernetes_not_configured",
				"detail": "SENTINEL_KUBE_CONFIG_PATH is unset and the process is not running in-cluster",
			})
			return
		}

		result, err := kubediscovery.Discover(r.Context(), deps.Kube, deps.KubeNamespace)
		if err != nil {
			deps.Logger.Error().Err(err).Msg("kubernetes discovery failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}

		out := make([]kubeResourceResponse, 0, len(result.Resources))
		for _, res := range result.Resources {
			res.ProjectID = projectID
			stored, err := deps.KubeResources.Upsert(r.Context(), res)
			if err != nil {
				deps.Logger.Error().Err(err).Str("kind", res.Kind).Str("name", res.Name).Msg("upserting kube resource failed")
				continue
			}
			out = append(out, toKubeResourceResponse(stored))
		}

		if err := deps.Audit.Record(r.Context(), audit.Event{
			ActionType: "kubernetes.discovered", ResourceType: "project", ResourceID: projectID,
			Actor: "user", Metadata: map[string]any{"resource_count": len(out), "warning_count": len(result.Warnings)},
		}); err != nil {
			deps.Logger.Error().Err(err).Msg("recording kubernetes.discovered audit event failed")
		}

		writeJSON(w, http.StatusOK, map[string]any{"resources": out, "warnings": result.Warnings})
	}
}

func handleListKubeResources(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list, err := deps.KubeResources.ListByProject(r.Context(), chi.URLParam(r, "projectID"))
		if err != nil {
			deps.Logger.Error().Err(err).Msg("listing kube resources failed")
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		out := make([]kubeResourceResponse, 0, len(list))
		for _, res := range list {
			out = append(out, toKubeResourceResponse(res))
		}
		writeJSON(w, http.StatusOK, map[string]any{"resources": out})
	}
}

type kubeEventResponse struct {
	Namespace     string `json:"namespace"`
	InvolvedKind  string `json:"involved_kind"`
	InvolvedName  string `json:"involved_name"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Type          string `json:"type"`
	Count         int32  `json:"count"`
	LastTimestamp string `json:"last_timestamp"`
}

// handleGetKubeEvents proxies live cluster events (spec §7.5 "Events")
// without persisting them — events are inherently high-volume and
// ephemeral, unlike the discovery snapshot in kube_resources.
func handleGetKubeEvents(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Kube == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes_not_configured"})
			return
		}
		namespace := r.URL.Query().Get("namespace")
		if namespace == "" {
			namespace = deps.KubeNamespace
		}

		events, err := deps.Kube.ListEvents(r.Context(), namespace)
		if err != nil {
			writeKubeAPIError(w, deps, err, "listing kubernetes events failed")
			return
		}
		sort.Slice(events, func(i, j int) bool { return events[i].LastTimestamp > events[j].LastTimestamp })

		out := make([]kubeEventResponse, 0, len(events))
		for _, e := range events {
			out = append(out, kubeEventResponse{
				Namespace: e.InvolvedObject.Namespace, InvolvedKind: e.InvolvedObject.Kind, InvolvedName: e.InvolvedObject.Name,
				Reason: e.Reason, Message: e.Message, Type: e.Type, Count: e.Count, LastTimestamp: e.LastTimestamp,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": out})
	}
}

const defaultPodLogTailLines = 200

// handleGetKubePodLogs proxies a single container's recent log lines,
// read-only and capped (spec §7.5 "Read-only logs") — never a live
// stream/follow.
func handleGetKubePodLogs(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Kube == nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes_not_configured"})
			return
		}
		namespace := r.URL.Query().Get("namespace")
		if namespace == "" {
			namespace = deps.KubeNamespace
		}
		container := r.URL.Query().Get("container")

		tailLines := defaultPodLogTailLines
		if raw := r.URL.Query().Get("tail_lines"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_tail_lines"})
				return
			}
			tailLines = parsed
		}

		logs, err := deps.Kube.PodLogs(r.Context(), namespace, chi.URLParam(r, "podName"), container, tailLines)
		if err != nil {
			writeKubeAPIError(w, deps, err, "fetching pod logs failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"logs": logs})
	}
}

// writeKubeAPIError maps kubeclient's sentinel errors to HTTP statuses
// that reflect what actually happened against the cluster, rather than
// a blanket 500 — a caller can distinguish "cluster unreachable" from
// "RBAC denied this" from "this resource doesn't exist".
func writeKubeAPIError(w http.ResponseWriter, deps Dependencies, err error, logMsg string) {
	switch {
	case errors.Is(err, kubeclient.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "kubernetes_rbac_denied", "detail": err.Error()})
	case errors.Is(err, kubeclient.ErrNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "kubernetes_resource_not_found"})
	case errors.Is(err, kubeclient.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "kubernetes_api_unreachable"})
	default:
		deps.Logger.Error().Err(err).Msg(logMsg)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}
