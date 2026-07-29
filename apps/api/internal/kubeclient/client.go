// Package kubeclient is a minimal, read-only Kubernetes API client,
// implementing only the operations Phase 10 discovery needs (spec §7.5)
// rather than pulling in k8s.io/client-go's much larger dependency tree
// (apimachinery, klog, cloud-provider auth plugins) — the same
// deliberately-small-capability-surface philosophy as
// internal/dockerclient. The Kubernetes API is plain HTTPS+JSON per
// resource type, so a hand-rolled client is structurally no harder than
// dockerclient's Unix-socket one.
//
// Every method is a GET. There is no create/update/delete/patch
// anywhere in this package — spec §2 explicitly forbids "Apply
// Kubernetes resources", and Phase 10 is discovery-only. Secret and
// ConfigMap values are never requested into memory: SecretSummary and
// ConfigMapSummary intentionally have no Data field, so encoding/json
// drops that part of the response during decode rather than us ever
// holding, inspecting, or logging it.
//
// Every method returns ErrUnavailable when the API server is
// unreachable, ErrForbidden when RBAC denies the request, and
// ErrNotFound when a resource type doesn't exist in the cluster (e.g.
// the Gateway API CRD isn't installed) — callers use these to degrade
// gracefully per resource kind rather than failing an entire discovery
// run over one missing or restricted kind.
package kubeclient

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrUnavailable is returned when the API server can't be reached at all.
var ErrUnavailable = errors.New("kubeclient: kubernetes api server unavailable")

// ErrForbidden is returned on a 403 — the configured credentials don't
// grant this request (spec §7.5 "use least-privilege RBAC": a
// restrictively-scoped read-only ClusterRole is expected to produce
// this for anything outside its rules).
var ErrForbidden = errors.New("kubeclient: forbidden (RBAC denied this request)")

// ErrNotFound is returned on a 404 — most often an optional API group
// (e.g. Gateway API) that isn't registered in this cluster at all.
var ErrNotFound = errors.New("kubeclient: resource type not found in this cluster")

// Client talks to a single Kubernetes API server over HTTPS.
type Client struct {
	httpClient *http.Client
	baseURL    string
	token      string
}

// New builds a Client from an already-resolved Config. Use LoadKubeconfig
// or LoadInCluster to build a Config, or Detect to pick automatically.
func New(cfg Config) (*Client, error) {
	if cfg.ServerURL == "" {
		return nil, fmt.Errorf("kubeclient: Config.ServerURL is empty")
	}
	tlsConfig := &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify} //nolint:gosec // explicit opt-in, documented in kubeconfig.go

	if len(cfg.CACert) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(cfg.CACert) {
			return nil, fmt.Errorf("kubeclient: certificate-authority-data did not contain a valid PEM certificate")
		}
		tlsConfig.RootCAs = pool
	}
	if len(cfg.ClientCert) > 0 && len(cfg.ClientKey) > 0 {
		cert, err := tls.X509KeyPair(cfg.ClientCert, cfg.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("kubeclient: parsing client certificate/key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return &Client{
		httpClient: &http.Client{
			Timeout:   15 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
		baseURL: cfg.ServerURL,
		token:   cfg.Token,
	}, nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("kubeclient: building request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return fmt.Errorf("%w: %s", ErrForbidden, path)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, path)
	default:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("kubeclient: %s returned %d: %s", path, res.StatusCode, string(body))
	}

	if out != nil {
		if err := json.NewDecoder(res.Body).Decode(out); err != nil {
			return fmt.Errorf("kubeclient: decoding response from %s: %w", path, err)
		}
	}
	return nil
}

// Ping checks API server reachability against /version, which every
// Kubernetes API server exposes unauthenticated-or-not without needing
// any specific resource permission.
func (c *Client) Ping(ctx context.Context) error {
	return c.get(ctx, "/version", nil)
}

// list decodes a Kubernetes List object's "items" array into out.
func list[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var body struct {
		Items []T `json:"items"`
	}
	if err := c.get(ctx, path, &body); err != nil {
		return nil, err
	}
	return body.Items, nil
}

// namespacedPath builds a cluster-wide path when namespace is empty, or
// a namespace-scoped one otherwise — matching the Kubernetes API's own
// convention of the same resource being listable both ways.
func namespacedPath(apiPrefix, namespace, resource string) string {
	if namespace == "" {
		return apiPrefix + "/" + resource
	}
	return apiPrefix + "/namespaces/" + url.PathEscape(namespace) + "/" + resource
}

func (c *Client) ListNamespaces(ctx context.Context) ([]Namespace, error) {
	return list[Namespace](ctx, c, "/api/v1/namespaces")
}

func (c *Client) ListDeployments(ctx context.Context, namespace string) ([]Deployment, error) {
	return list[Deployment](ctx, c, namespacedPath("/apis/apps/v1", namespace, "deployments"))
}

func (c *Client) ListStatefulSets(ctx context.Context, namespace string) ([]StatefulSet, error) {
	return list[StatefulSet](ctx, c, namespacedPath("/apis/apps/v1", namespace, "statefulsets"))
}

func (c *Client) ListDaemonSets(ctx context.Context, namespace string) ([]DaemonSet, error) {
	return list[DaemonSet](ctx, c, namespacedPath("/apis/apps/v1", namespace, "daemonsets"))
}

func (c *Client) ListJobs(ctx context.Context, namespace string) ([]Job, error) {
	return list[Job](ctx, c, namespacedPath("/apis/batch/v1", namespace, "jobs"))
}

func (c *Client) ListCronJobs(ctx context.Context, namespace string) ([]CronJob, error) {
	return list[CronJob](ctx, c, namespacedPath("/apis/batch/v1", namespace, "cronjobs"))
}

func (c *Client) ListServices(ctx context.Context, namespace string) ([]Service, error) {
	return list[Service](ctx, c, namespacedPath("/api/v1", namespace, "services"))
}

func (c *Client) ListIngresses(ctx context.Context, namespace string) ([]Ingress, error) {
	return list[Ingress](ctx, c, namespacedPath("/apis/networking.k8s.io/v1", namespace, "ingresses"))
}

// ListGateways is best-effort: the Gateway API is a CRD, not part of
// every cluster's core API surface. A caller should treat ErrNotFound
// here as "Gateway API not installed", not a discovery failure.
func (c *Client) ListGateways(ctx context.Context, namespace string) ([]Gateway, error) {
	return list[Gateway](ctx, c, namespacedPath("/apis/gateway.networking.k8s.io/v1", namespace, "gateways"))
}

// ListConfigMaps returns names/metadata only — ConfigMapSummary has no
// Data field, so its contents are dropped during JSON decode.
func (c *Client) ListConfigMaps(ctx context.Context, namespace string) ([]ConfigMapSummary, error) {
	return list[ConfigMapSummary](ctx, c, namespacedPath("/api/v1", namespace, "configmaps"))
}

// ListSecrets returns names/metadata/type only — never values (spec
// §7.5 "Secret names"). SecretSummary has no Data/StringData field.
func (c *Client) ListSecrets(ctx context.Context, namespace string) ([]SecretSummary, error) {
	return list[SecretSummary](ctx, c, namespacedPath("/api/v1", namespace, "secrets"))
}

func (c *Client) ListPods(ctx context.Context, namespace string) ([]Pod, error) {
	return list[Pod](ctx, c, namespacedPath("/api/v1", namespace, "pods"))
}

// ListEvents returns recent events for namespace (or cluster-wide if
// empty). The Kubernetes API doesn't offer server-side recency
// filtering beyond field/label selectors, so callers wanting "recent"
// should sort by LastTimestamp/EventTime themselves.
func (c *Client) ListEvents(ctx context.Context, namespace string) ([]Event, error) {
	return list[Event](ctx, c, namespacedPath("/api/v1", namespace, "events"))
}

// MaxLogTailLines caps PodLogs' tailLines parameter — this is a
// discovery/diagnostic aid, never a substitute for a real log
// aggregation pipeline, so requesting an unbounded tail is disallowed.
const MaxLogTailLines = 2000

// PodLogs fetches up to tailLines of a single container's log, read-only
// and non-streaming (no "follow"). tailLines is clamped to
// [1, MaxLogTailLines].
func (c *Client) PodLogs(ctx context.Context, namespace, pod, container string, tailLines int) (string, error) {
	if tailLines <= 0 {
		tailLines = 200
	}
	if tailLines > MaxLogTailLines {
		tailLines = MaxLogTailLines
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/log?tailLines=%d",
		url.PathEscape(namespace), url.PathEscape(pod), tailLines)
	if container != "" {
		path += "&container=" + url.QueryEscape(container)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return "", fmt.Errorf("kubeclient: building request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer res.Body.Close()

	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden:
		return "", fmt.Errorf("%w: pod logs %s/%s", ErrForbidden, namespace, pod)
	case http.StatusNotFound:
		return "", fmt.Errorf("%w: pod logs %s/%s", ErrNotFound, namespace, pod)
	default:
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return "", fmt.Errorf("kubeclient: pod logs %s/%s returned %d: %s", namespace, pod, res.StatusCode, string(body))
	}

	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		return "", fmt.Errorf("kubeclient: reading pod logs: %w", err)
	}
	return string(body), nil
}
