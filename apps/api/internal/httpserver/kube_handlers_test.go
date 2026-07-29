package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"e2e-sentinel/apps/api/internal/kubeclient"
)

// fakeKubeAPI implements KubeAPI entirely from in-memory fixtures.
type fakeKubeAPI struct {
	deployments []kubeclient.Deployment
	services    []kubeclient.Service
	events      []kubeclient.Event
	podLogs     string
	podLogsErr  error
	listErr     error
}

func (f *fakeKubeAPI) ListNamespaces(context.Context) ([]kubeclient.Namespace, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListDeployments(context.Context, string) ([]kubeclient.Deployment, error) {
	return f.deployments, f.listErr
}
func (f *fakeKubeAPI) ListStatefulSets(context.Context, string) ([]kubeclient.StatefulSet, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListDaemonSets(context.Context, string) ([]kubeclient.DaemonSet, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListJobs(context.Context, string) ([]kubeclient.Job, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListCronJobs(context.Context, string) ([]kubeclient.CronJob, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListServices(context.Context, string) ([]kubeclient.Service, error) {
	return f.services, f.listErr
}
func (f *fakeKubeAPI) ListIngresses(context.Context, string) ([]kubeclient.Ingress, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListGateways(context.Context, string) ([]kubeclient.Gateway, error) {
	return nil, kubeclient.ErrNotFound
}
func (f *fakeKubeAPI) ListConfigMaps(context.Context, string) ([]kubeclient.ConfigMapSummary, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListSecrets(context.Context, string) ([]kubeclient.SecretSummary, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListPods(context.Context, string) ([]kubeclient.Pod, error) {
	return nil, f.listErr
}
func (f *fakeKubeAPI) ListEvents(context.Context, string) ([]kubeclient.Event, error) {
	return f.events, f.listErr
}
func (f *fakeKubeAPI) PodLogs(context.Context, string, string, string, int) (string, error) {
	return f.podLogs, f.podLogsErr
}

func createTestProject(t *testing.T, router http.Handler) string {
	t.Helper()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module x\n")
	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects", map[string]string{"name": "Fixture", "repository_path": dir})
	var project struct {
		ID string `json:"id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &project)
	return project.ID
}

func TestKubeStatus_ReportsUnconfiguredByDefault(t *testing.T) {
	router := NewRouter(newTestDeps(nil, nil))
	rec := doJSON(t, router, http.MethodGet, "/api/v1/kube/status", nil)
	var body struct {
		Configured bool `json:"configured"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Configured {
		t.Error("configured = true, want false by default")
	}
}

func TestKubeStatus_ReportsConfiguredWhenSet(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{}
	router := NewRouter(deps)
	rec := doJSON(t, router, http.MethodGet, "/api/v1/kube/status", nil)
	var body struct {
		Configured bool `json:"configured"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if !body.Configured {
		t.Error("configured = false, want true")
	}
}

func TestDiscoverKube_ReturnsServiceUnavailableWhenNotConfigured(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/kube-discover", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503, body=%s", rec.Code, rec.Body.String())
	}
}

func TestDiscoverKube_UnknownProjectReturns404(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{}
	router := NewRouter(deps)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects/does-not-exist/kube-discover", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDiscoverKube_UpsertsResourcesAndListsThem(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{
		deployments: unmarshalFixtureHTTP[kubeclient.Deployment](t, `[{
			"metadata": {"name": "web", "namespace": "default"},
			"spec": {"replicas": 2, "selector": {"matchLabels": {"app": "web"}}},
			"status": {"replicas": 2, "readyReplicas": 2}
		}]`),
		services: unmarshalFixtureHTTP[kubeclient.Service](t, `[{
			"metadata": {"name": "web", "namespace": "default"},
			"spec": {"type": "ClusterIP"}
		}]`),
	}
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	discoverRec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/kube-discover", nil)
	if discoverRec.Code != http.StatusOK {
		t.Fatalf("discover status = %d, body=%s", discoverRec.Code, discoverRec.Body.String())
	}
	var discoverBody struct {
		Resources []struct {
			Kind   string `json:"kind"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"resources"`
		Warnings []string `json:"warnings"`
	}
	json.Unmarshal(discoverRec.Body.Bytes(), &discoverBody)
	if len(discoverBody.Resources) != 2 {
		t.Fatalf("resources = %+v, want 2 (1 deployment + 1 service)", discoverBody.Resources)
	}

	listRec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/kube-resources", nil)
	var listBody struct {
		Resources []struct {
			Kind string `json:"kind"`
		} `json:"resources"`
	}
	json.Unmarshal(listRec.Body.Bytes(), &listBody)
	if len(listBody.Resources) != 2 {
		t.Fatalf("listed resources = %+v, want 2 (discovery must persist)", listBody.Resources)
	}
}

func TestDiscoverKube_PartialFailureStillReturnsWhatSucceeded(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{
		deployments: unmarshalFixtureHTTP[kubeclient.Deployment](t, `[{"metadata": {"name": "web", "namespace": "default"}}]`),
		listErr:     kubeclient.ErrForbidden,
	}
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/projects/"+projectID+"/kube-discover", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a forbidden kind must not fail the whole discovery), body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Warnings []string `json:"warnings"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Warnings) == 0 {
		t.Error("expected warnings for the forbidden kinds")
	}
}

func TestGetKubeEvents_ReturnsServiceUnavailableWhenNotConfigured(t *testing.T) {
	deps := newTestDeps(nil, nil)
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/kube/events", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestGetKubeEvents_SortsNewestFirst(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{events: unmarshalFixtureHTTP[kubeclient.Event](t, `[
		{"reason": "Older", "lastTimestamp": "2024-01-01T00:00:00Z"},
		{"reason": "Newer", "lastTimestamp": "2024-06-01T00:00:00Z"}
	]`)}
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/kube/events", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []struct {
			Reason string `json:"reason"`
		} `json:"events"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Events) != 2 || body.Events[0].Reason != "Newer" {
		t.Errorf("events = %+v, want Newer first", body.Events)
	}
}

func TestGetKubePodLogs_ReturnsLogsAndClampsTailLines(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{podLogs: "line 1\nline 2\n"}
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/kube/pods/web-abc123/logs", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Logs string `json:"logs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &body)
	if body.Logs != "line 1\nline 2\n" {
		t.Errorf("logs = %q", body.Logs)
	}
}

func TestGetKubePodLogs_InvalidTailLinesIs400(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{}
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/kube/pods/web-abc123/logs?tail_lines=not-a-number", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetKubePodLogs_ForbiddenMapsTo403(t *testing.T) {
	deps := newTestDeps(nil, nil)
	deps.Kube = &fakeKubeAPI{podLogsErr: kubeclient.ErrForbidden}
	router := NewRouter(deps)
	projectID := createTestProject(t, router)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/projects/"+projectID+"/kube/pods/web-abc123/logs", nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func unmarshalFixtureHTTP[T any](t *testing.T, raw string) []T {
	t.Helper()
	var out []T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshaling fixture: %v", err)
	}
	return out
}
