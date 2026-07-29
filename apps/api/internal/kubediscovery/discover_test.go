package kubediscovery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"e2e-sentinel/apps/api/internal/kubeclient"
)

// fakeAPI implements API entirely from in-memory fixtures, the same
// approach dockerclient's own callers use for a fake DockerLister.
// Fixtures are built via unmarshalFixture (JSON) rather than Go struct
// literals, since several kubeclient types have anonymous nested struct
// fields that are awkward to construct directly.
type fakeAPI struct {
	namespaces   []kubeclient.Namespace
	deployments  []kubeclient.Deployment
	statefulSets []kubeclient.StatefulSet
	daemonSets   []kubeclient.DaemonSet
	jobs         []kubeclient.Job
	cronJobs     []kubeclient.CronJob
	services     []kubeclient.Service
	ingresses    []kubeclient.Ingress
	gatewaysErr  error
	configMaps   []kubeclient.ConfigMapSummary
	secrets      []kubeclient.SecretSummary
	pods         []kubeclient.Pod

	errFor map[string]error
}

func (f *fakeAPI) err(kind string) error {
	if f.errFor == nil {
		return nil
	}
	return f.errFor[kind]
}

func (f *fakeAPI) ListNamespaces(context.Context) ([]kubeclient.Namespace, error) {
	return f.namespaces, f.err("namespaces")
}
func (f *fakeAPI) ListDeployments(context.Context, string) ([]kubeclient.Deployment, error) {
	return f.deployments, f.err("deployments")
}
func (f *fakeAPI) ListStatefulSets(context.Context, string) ([]kubeclient.StatefulSet, error) {
	return f.statefulSets, f.err("statefulsets")
}
func (f *fakeAPI) ListDaemonSets(context.Context, string) ([]kubeclient.DaemonSet, error) {
	return f.daemonSets, f.err("daemonsets")
}
func (f *fakeAPI) ListJobs(context.Context, string) ([]kubeclient.Job, error) {
	return f.jobs, f.err("jobs")
}
func (f *fakeAPI) ListCronJobs(context.Context, string) ([]kubeclient.CronJob, error) {
	return f.cronJobs, f.err("cronjobs")
}
func (f *fakeAPI) ListServices(context.Context, string) ([]kubeclient.Service, error) {
	return f.services, f.err("services")
}
func (f *fakeAPI) ListIngresses(context.Context, string) ([]kubeclient.Ingress, error) {
	return f.ingresses, f.err("ingresses")
}
func (f *fakeAPI) ListGateways(context.Context, string) ([]kubeclient.Gateway, error) {
	return nil, f.gatewaysErr
}
func (f *fakeAPI) ListConfigMaps(context.Context, string) ([]kubeclient.ConfigMapSummary, error) {
	return f.configMaps, f.err("configmaps")
}
func (f *fakeAPI) ListSecrets(context.Context, string) ([]kubeclient.SecretSummary, error) {
	return f.secrets, f.err("secrets")
}
func (f *fakeAPI) ListPods(context.Context, string) ([]kubeclient.Pod, error) {
	return f.pods, f.err("pods")
}

// unmarshalFixture decodes a JSON literal into a slice of T, failing
// the test on any error — a compact way to build fixtures for types
// with anonymous nested struct fields.
func unmarshalFixture[T any](t *testing.T, raw string) []T {
	t.Helper()
	var out []T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshaling fixture: %v", err)
	}
	return out
}

func resourcesByKind(result Result, kind string) []Resource {
	var out []Resource
	for _, r := range result.Resources {
		if r.Kind == kind {
			out = append(out, r)
		}
	}
	return out
}

func TestDiscover_DeploymentHealthFromStatusFields(t *testing.T) {
	api := &fakeAPI{
		deployments: unmarshalFixture[kubeclient.Deployment](t, `[{
			"metadata": {"name": "web", "namespace": "default"},
			"spec": {"replicas": 3, "selector": {"matchLabels": {"app": "web"}}},
			"status": {"replicas": 3, "readyReplicas": 2}
		}]`),
		pods: unmarshalFixture[kubeclient.Pod](t, `[
			{"metadata": {"namespace": "default", "labels": {"app": "web"}},
			 "status": {"phase": "Running", "containerStatuses": [{"name": "web", "ready": true, "restartCount": 2}]}},
			{"metadata": {"namespace": "default", "labels": {"app": "web"}},
			 "status": {"phase": "Running", "containerStatuses": [{"name": "web", "ready": false, "restartCount": 5}]}}
		]`),
	}

	result, err := Discover(context.Background(), api, "default")
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	deployments := resourcesByKind(result, KindDeployment)
	if len(deployments) != 1 {
		t.Fatalf("len(deployments) = %d, want 1", len(deployments))
	}
	d := deployments[0]
	if *d.DesiredReplicas != 3 || *d.ReadyReplicas != 2 {
		t.Errorf("desired/ready = %d/%d, want 3/2", *d.DesiredReplicas, *d.ReadyReplicas)
	}
	if *d.RestartCount != 7 {
		t.Errorf("RestartCount = %d, want 7 (sum across both matched pods)", *d.RestartCount)
	}
	if d.Status != StatusDegraded {
		t.Errorf("Status = %q, want degraded (ready 2 < desired 3)", d.Status)
	}
}

func TestDiscover_DeploymentHealthyWhenReadyMeetsDesired(t *testing.T) {
	api := &fakeAPI{
		deployments: unmarshalFixture[kubeclient.Deployment](t, `[{
			"metadata": {"name": "web", "namespace": "default"},
			"spec": {"replicas": 2},
			"status": {"replicas": 2, "readyReplicas": 2}
		}]`),
	}
	result, _ := Discover(context.Background(), api, "default")
	if resourcesByKind(result, KindDeployment)[0].Status != StatusHealthy {
		t.Errorf("Status = %q, want healthy", resourcesByKind(result, KindDeployment)[0].Status)
	}
}

func TestDiscover_ZeroDesiredReplicasIsUnknownNotDegraded(t *testing.T) {
	api := &fakeAPI{
		deployments: unmarshalFixture[kubeclient.Deployment](t, `[{
			"metadata": {"name": "scaled-to-zero", "namespace": "default"},
			"spec": {"replicas": 0}
		}]`),
	}
	result, _ := Discover(context.Background(), api, "default")
	if resourcesByKind(result, KindDeployment)[0].Status != StatusUnknown {
		t.Errorf("Status = %q, want unknown for a deliberately-scaled-to-zero deployment", resourcesByKind(result, KindDeployment)[0].Status)
	}
}

func TestDiscover_DaemonSetUsesStatusFieldsDirectly(t *testing.T) {
	api := &fakeAPI{
		daemonSets: unmarshalFixture[kubeclient.DaemonSet](t, `[{
			"metadata": {"name": "node-agent", "namespace": "kube-system"},
			"spec": {"selector": {"matchLabels": {"app": "node-agent"}}},
			"status": {"desiredNumberScheduled": 3, "numberReady": 3}
		}]`),
	}
	result, _ := Discover(context.Background(), api, "kube-system")
	daemonSets := resourcesByKind(result, KindDaemonSet)
	if len(daemonSets) != 1 {
		t.Fatalf("len = %d, want 1", len(daemonSets))
	}
	if *daemonSets[0].DesiredReplicas != 3 || *daemonSets[0].ReadyReplicas != 3 {
		t.Errorf("desired/ready = %d/%d", *daemonSets[0].DesiredReplicas, *daemonSets[0].ReadyReplicas)
	}
	if daemonSets[0].Status != StatusHealthy {
		t.Errorf("Status = %q, want healthy", daemonSets[0].Status)
	}
}

func TestDiscover_JobStatusFromCounts(t *testing.T) {
	api := &fakeAPI{
		jobs: unmarshalFixture[kubeclient.Job](t, `[
			{"metadata": {"name": "migrate", "namespace": "default"}, "status": {"succeeded": 1}},
			{"metadata": {"name": "broken", "namespace": "default"}, "status": {"failed": 1}}
		]`),
	}
	result, _ := Discover(context.Background(), api, "default")
	jobs := resourcesByKind(result, KindJob)
	if len(jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(jobs))
	}
	byName := map[string]Resource{}
	for _, j := range jobs {
		byName[j.Name] = j
	}
	if byName["migrate"].Status != StatusHealthy {
		t.Errorf("migrate status = %q, want healthy", byName["migrate"].Status)
	}
	if byName["broken"].Status != StatusDegraded {
		t.Errorf("broken status = %q, want degraded", byName["broken"].Status)
	}
}

func TestDiscover_CronJobIsNotApplicableStatusWithScheduleMetadata(t *testing.T) {
	api := &fakeAPI{
		cronJobs: unmarshalFixture[kubeclient.CronJob](t, `[{
			"metadata": {"name": "nightly-report", "namespace": "default"},
			"spec": {"schedule": "0 2 * * *"}
		}]`),
	}
	result, _ := Discover(context.Background(), api, "default")
	cronJobs := resourcesByKind(result, KindCronJob)
	if len(cronJobs) != 1 {
		t.Fatalf("len = %d, want 1", len(cronJobs))
	}
	if cronJobs[0].Status != StatusNotApplicable {
		t.Errorf("Status = %q, want not_applicable", cronJobs[0].Status)
	}
	if cronJobs[0].Metadata["schedule"] != "0 2 * * *" {
		t.Errorf("schedule = %v", cronJobs[0].Metadata["schedule"])
	}
}

func TestDiscover_IngressLinksBackendServiceNames(t *testing.T) {
	api := &fakeAPI{
		ingresses: unmarshalFixture[kubeclient.Ingress](t, `[{
			"metadata": {"name": "web-ingress", "namespace": "default"},
			"spec": {"rules": [{"host": "example.com", "http": {"paths": [{"path": "/", "backend": {"service": {"name": "web"}}}]}}]}
		}]`),
	}
	result, err := Discover(context.Background(), api, "default")
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	ingresses := resourcesByKind(result, KindIngress)
	if len(ingresses) != 1 {
		t.Fatalf("len(ingresses) = %d, want 1", len(ingresses))
	}
	hosts, ok := ingresses[0].Metadata["hosts"].([]string)
	if !ok || len(hosts) != 1 || hosts[0] != "example.com" {
		t.Errorf("hosts = %v", ingresses[0].Metadata["hosts"])
	}
	backends, ok := ingresses[0].Metadata["backend_services"].([]string)
	if !ok || len(backends) != 1 || backends[0] != "web" {
		t.Errorf("backend_services = %v", ingresses[0].Metadata["backend_services"])
	}
}

func TestDiscover_ServiceRecordsTypeAndSelector(t *testing.T) {
	api := &fakeAPI{
		services: unmarshalFixture[kubeclient.Service](t, `[{
			"metadata": {"name": "web", "namespace": "default"},
			"spec": {"type": "ClusterIP", "clusterIP": "10.0.0.5", "selector": {"app": "web"}, "ports": [{"port": 80, "protocol": "TCP"}]}
		}]`),
	}
	result, _ := Discover(context.Background(), api, "default")
	services := resourcesByKind(result, KindService)
	if len(services) != 1 {
		t.Fatalf("len = %d, want 1", len(services))
	}
	if services[0].Metadata["type"] != "ClusterIP" {
		t.Errorf("type = %v", services[0].Metadata["type"])
	}
}

func TestDiscover_GatewayAPINotInstalledIsNotAWarning(t *testing.T) {
	api := &fakeAPI{gatewaysErr: kubeclient.ErrNotFound}
	result, err := Discover(context.Background(), api, "default")
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}
	for _, w := range result.Warnings {
		if stringContains(w, "gateway") {
			t.Errorf("Gateway API absence should not produce a warning, got: %v", result.Warnings)
		}
	}
}

func TestDiscover_ForbiddenKindProducesWarningNotFailure(t *testing.T) {
	api := &fakeAPI{errFor: map[string]error{"secrets": kubeclient.ErrForbidden}}
	result, err := Discover(context.Background(), api, "default")
	if err != nil {
		t.Fatalf("Discover() error: %v, want nil (a forbidden kind must not fail the whole discovery)", err)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected a warning recorded for the forbidden secrets list")
	}
}

func TestDiscover_SecretsNeverCarryValuesInMetadata(t *testing.T) {
	api := &fakeAPI{
		secrets: unmarshalFixture[kubeclient.SecretSummary](t, `[{"metadata": {"name": "db-creds", "namespace": "default"}, "type": "Opaque"}]`),
	}
	result, _ := Discover(context.Background(), api, "default")
	secrets := resourcesByKind(result, KindSecret)
	if len(secrets) != 1 {
		t.Fatalf("len(secrets) = %d, want 1", len(secrets))
	}
	if len(secrets[0].Metadata) != 1 {
		t.Errorf("secret metadata = %v, want only secret_type", secrets[0].Metadata)
	}
	if secrets[0].Metadata["secret_type"] != "Opaque" {
		t.Errorf("secret_type = %v", secrets[0].Metadata["secret_type"])
	}
}

func TestDiscover_ClusterWideListsNamespaces(t *testing.T) {
	api := &fakeAPI{namespaces: unmarshalFixture[kubeclient.Namespace](t, `[{"metadata": {"name": "default"}}, {"metadata": {"name": "prod"}}]`)}
	result, _ := Discover(context.Background(), api, "")
	if len(resourcesByKind(result, KindNamespace)) != 2 {
		t.Errorf("namespaces = %d, want 2", len(resourcesByKind(result, KindNamespace)))
	}
}

func TestDiscover_NamespaceScopedSkipsNamespaceListing(t *testing.T) {
	api := &fakeAPI{namespaces: unmarshalFixture[kubeclient.Namespace](t, `[{"metadata": {"name": "default"}}]`)}
	result, _ := Discover(context.Background(), api, "default")
	if len(resourcesByKind(result, KindNamespace)) != 0 {
		t.Error("a namespace-scoped discovery should not enumerate all namespaces")
	}
}

func TestDiscover_UnavailableAPIProducesWarningsNotError(t *testing.T) {
	unavailable := errors.Join(kubeclient.ErrUnavailable, errors.New("connection refused"))
	api := &fakeAPI{errFor: map[string]error{
		"deployments": unavailable, "statefulsets": unavailable, "daemonsets": unavailable,
		"jobs": unavailable, "cronjobs": unavailable, "services": unavailable,
		"ingresses": unavailable, "configmaps": unavailable, "secrets": unavailable, "pods": unavailable,
	}}
	result, err := Discover(context.Background(), api, "default")
	if err != nil {
		t.Fatalf("Discover() error: %v, want nil even when every kind is unreachable", err)
	}
	if len(result.Resources) != 0 {
		t.Errorf("Resources = %v, want none", result.Resources)
	}
	if len(result.Warnings) == 0 {
		t.Error("expected warnings recorded for every unreachable kind")
	}
}

func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
