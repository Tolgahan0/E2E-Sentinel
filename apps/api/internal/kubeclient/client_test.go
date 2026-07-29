package kubeclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// startFakeAPIServer starts an httptest TLS server and returns a Client
// already configured (InsecureSkipVerify — the test server's cert is
// self-signed) to talk to it, mirroring how dockerclient's tests drive a
// fake daemon over a real socket instead of mocking at the Go-interface
// level.
func startFakeAPIServer(t *testing.T, handler http.Handler) *Client {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)

	client, err := New(Config{ServerURL: server.URL, Token: "test-token", InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	return client
}

func TestNew_RequiresServerURL(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New() with empty ServerURL should error")
	}
}

func TestPing_Succeeds(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			t.Errorf("path = %q, want /version", r.URL.Path)
		}
		w.Write([]byte(`{"gitVersion":"v1.30.0"}`))
	}))
	if err := client.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error: %v", err)
	}
}

func TestPing_UnreachableReturnsErrUnavailable(t *testing.T) {
	client, err := New(Config{ServerURL: "https://127.0.0.1:1", Token: "x"})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if err := client.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), ErrUnavailable.Error()) {
		t.Fatalf("Ping() error = %v, want ErrUnavailable", err)
	}
}

func TestGet_ForbiddenMapsToErrForbidden(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	_, err := client.ListPods(context.Background(), "default")
	if err == nil || !strings.Contains(err.Error(), ErrForbidden.Error()) {
		t.Fatalf("error = %v, want ErrForbidden (least-privilege RBAC must surface clearly)", err)
	}
}

func TestListGateways_NotInstalledMapsToErrNotFound(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "the server could not find the requested resource", http.StatusNotFound)
	}))
	_, err := client.ListGateways(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), ErrNotFound.Error()) {
		t.Fatalf("error = %v, want ErrNotFound (Gateway API CRD not installed must be distinguishable from a real failure)", err)
	}
}

func TestListDeployments_ClusterWideVsNamespaced(t *testing.T) {
	var gotPath string
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"items": []Deployment{}})
	}))

	if _, err := client.ListDeployments(context.Background(), ""); err != nil {
		t.Fatalf("ListDeployments(\"\") error: %v", err)
	}
	if gotPath != "/apis/apps/v1/deployments" {
		t.Errorf("cluster-wide path = %q", gotPath)
	}

	if _, err := client.ListDeployments(context.Background(), "prod"); err != nil {
		t.Fatalf("ListDeployments(prod) error: %v", err)
	}
	if gotPath != "/apis/apps/v1/namespaces/prod/deployments" {
		t.Errorf("namespaced path = %q", gotPath)
	}
}

func TestListDeployments_DecodesReplicaHealth(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"metadata": {"name": "web", "namespace": "default", "labels": {"app": "web"}},
			"spec": {"replicas": 3, "selector": {"matchLabels": {"app": "web"}}},
			"status": {"replicas": 3, "readyReplicas": 2}
		}]}`))
	}))

	deployments, err := client.ListDeployments(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListDeployments() error: %v", err)
	}
	if len(deployments) != 1 {
		t.Fatalf("len = %d, want 1", len(deployments))
	}
	d := deployments[0]
	if d.Metadata.Name != "web" || *d.Spec.Replicas != 3 || d.Status.ReadyReplicas != 2 {
		t.Errorf("deployment = %+v", d)
	}
}

func TestListSecrets_NeverRetainsDataField(t *testing.T) {
	// The fake server responds with a realistic Secret payload including
	// base64-encoded "data" — proving SecretSummary has no field that
	// could capture it is the point of this test, not that the server
	// withheld it (a real cluster's API always includes it).
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"metadata": {"name": "db-credentials", "namespace": "default"},
			"type": "Opaque",
			"data": {"password": "c3VwZXItc2VjcmV0LXZhbHVl"}
		}]}`))
	}))

	secrets, err := client.ListSecrets(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListSecrets() error: %v", err)
	}
	if len(secrets) != 1 || secrets[0].Metadata.Name != "db-credentials" {
		t.Fatalf("secrets = %+v", secrets)
	}
	if secrets[0].Type != "Opaque" {
		t.Errorf("Type = %q, want Opaque", secrets[0].Type)
	}
	// SecretSummary has no Data field at all — this is a compile-time
	// guarantee, not a runtime one, but asserting on the raw struct's
	// JSON tags (via re-marshaling) proves no such field slipped in.
	reMarshaled, _ := json.Marshal(secrets[0])
	if strings.Contains(string(reMarshaled), "secret") || strings.Contains(string(reMarshaled), "c3VwZXI") {
		t.Errorf("secret value leaked into decoded struct: %s", reMarshaled)
	}
}

func TestListConfigMaps_NeverRetainsDataField(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"metadata": {"name": "app-config", "namespace": "default"},
			"data": {"LOG_LEVEL": "debug"}
		}]}`))
	}))

	configMaps, err := client.ListConfigMaps(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListConfigMaps() error: %v", err)
	}
	if len(configMaps) != 1 || configMaps[0].Metadata.Name != "app-config" {
		t.Fatalf("configMaps = %+v", configMaps)
	}
	reMarshaled, _ := json.Marshal(configMaps[0])
	if strings.Contains(string(reMarshaled), "LOG_LEVEL") {
		t.Errorf("configmap data leaked into decoded struct: %s", reMarshaled)
	}
}

func TestListPods_DecodesRestartCounts(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"metadata": {"name": "web-abc123", "namespace": "default", "labels": {"app": "web"}},
			"status": {"phase": "Running", "containerStatuses": [{"name": "web", "ready": true, "restartCount": 4}]}
		}]}`))
	}))

	pods, err := client.ListPods(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListPods() error: %v", err)
	}
	if len(pods) != 1 || pods[0].Status.ContainerStatuses[0].RestartCount != 4 {
		t.Fatalf("pods = %+v", pods)
	}
}

func TestPodLogs_ClampsTailLinesAndSetsContainerQueryParam(t *testing.T) {
	var gotQuery string
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte("log line 1\nlog line 2\n"))
	}))

	logs, err := client.PodLogs(context.Background(), "default", "web-abc123", "web", 999999)
	if err != nil {
		t.Fatalf("PodLogs() error: %v", err)
	}
	if logs != "log line 1\nlog line 2\n" {
		t.Errorf("logs = %q", logs)
	}
	if !strings.Contains(gotQuery, "tailLines=2000") {
		t.Errorf("query = %q, want tailLines clamped to %d", gotQuery, MaxLogTailLines)
	}
	if !strings.Contains(gotQuery, "container=web") {
		t.Errorf("query = %q, want container=web", gotQuery)
	}
}

func TestPodLogs_ForbiddenMapsToErrForbidden(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	_, err := client.PodLogs(context.Background(), "default", "web-abc123", "", 100)
	if err == nil || !strings.Contains(err.Error(), ErrForbidden.Error()) {
		t.Fatalf("error = %v, want ErrForbidden", err)
	}
}

func TestListNamespaces_Decodes(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/namespaces" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"prod"}}]}`))
	}))

	namespaces, err := client.ListNamespaces(context.Background())
	if err != nil {
		t.Fatalf("ListNamespaces() error: %v", err)
	}
	if len(namespaces) != 2 {
		t.Fatalf("len = %d, want 2", len(namespaces))
	}
}

func TestListIngresses_DecodesBackendServiceNames(t *testing.T) {
	client := startFakeAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{
			"metadata": {"name": "web-ingress", "namespace": "default"},
			"spec": {"rules": [{"host": "example.com", "http": {"paths": [{"path": "/", "backend": {"service": {"name": "web"}}}]}}]}
		}]}`))
	}))

	ingresses, err := client.ListIngresses(context.Background(), "default")
	if err != nil {
		t.Fatalf("ListIngresses() error: %v", err)
	}
	if len(ingresses) != 1 || ingresses[0].Spec.Rules[0].HTTP.Paths[0].Backend.Service.Name != "web" {
		t.Fatalf("ingresses = %+v", ingresses)
	}
}
