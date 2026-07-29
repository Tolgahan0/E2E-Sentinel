package kubediscovery

import (
	"context"
	"errors"
	"fmt"
	"time"

	"e2e-sentinel/apps/api/internal/kubeclient"
)

// API is the subset of *kubeclient.Client Discover uses, narrowed so
// tests can substitute a fake — the same pattern as
// internal/httpserver's DockerLister.
type API interface {
	ListNamespaces(ctx context.Context) ([]kubeclient.Namespace, error)
	ListDeployments(ctx context.Context, namespace string) ([]kubeclient.Deployment, error)
	ListStatefulSets(ctx context.Context, namespace string) ([]kubeclient.StatefulSet, error)
	ListDaemonSets(ctx context.Context, namespace string) ([]kubeclient.DaemonSet, error)
	ListJobs(ctx context.Context, namespace string) ([]kubeclient.Job, error)
	ListCronJobs(ctx context.Context, namespace string) ([]kubeclient.CronJob, error)
	ListServices(ctx context.Context, namespace string) ([]kubeclient.Service, error)
	ListIngresses(ctx context.Context, namespace string) ([]kubeclient.Ingress, error)
	ListGateways(ctx context.Context, namespace string) ([]kubeclient.Gateway, error)
	ListConfigMaps(ctx context.Context, namespace string) ([]kubeclient.ConfigMapSummary, error)
	ListSecrets(ctx context.Context, namespace string) ([]kubeclient.SecretSummary, error)
	ListPods(ctx context.Context, namespace string) ([]kubeclient.Pod, error)
}

// Result is Discover's output: the resources found, plus a list of
// warnings for kinds that couldn't be listed (a missing CRD, an RBAC
// restriction) — these never fail the whole call, per spec §7.5's
// "least-privilege RBAC" expectation that a read-only ClusterRole may
// legitimately not grant every kind.
type Result struct {
	Resources []Resource
	Warnings  []string
}

// Discover lists every spec §7.5 resource kind in namespace (cluster-
// wide when empty) and correlates pods to workloads by label selector
// (not ownerReferences — this avoids needing a ReplicaSet lookup for
// Deployments, at the cost of not distinguishing an old, scaled-down
// ReplicaSet's stray pods from the current one; acceptable for a
// discovery/health-overview feature, not a scheduler).
func Discover(ctx context.Context, api API, namespace string) (Result, error) {
	var result Result
	warn := func(kind string, err error) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("%s: %v", kind, err))
	}

	if namespace == "" {
		if namespaces, err := api.ListNamespaces(ctx); err != nil {
			warn(KindNamespace, err)
		} else {
			for _, ns := range namespaces {
				result.Resources = append(result.Resources, Resource{
					Namespace: ns.Metadata.Name, Kind: KindNamespace, Name: ns.Metadata.Name,
					Status: StatusNotApplicable, Metadata: map[string]any{}, LastSeenAt: now(),
				})
			}
		}
	}

	pods, err := api.ListPods(ctx, namespace)
	if err != nil {
		warn("pods (used for restart-count correlation)", err)
	}

	if deployments, err := api.ListDeployments(ctx, namespace); err != nil {
		warn(KindDeployment, err)
	} else {
		for _, d := range deployments {
			result.Resources = append(result.Resources, deploymentResource(d, pods))
		}
	}

	if statefulSets, err := api.ListStatefulSets(ctx, namespace); err != nil {
		warn(KindStatefulSet, err)
	} else {
		for _, s := range statefulSets {
			result.Resources = append(result.Resources, statefulSetResource(s, pods))
		}
	}

	if daemonSets, err := api.ListDaemonSets(ctx, namespace); err != nil {
		warn(KindDaemonSet, err)
	} else {
		for _, d := range daemonSets {
			result.Resources = append(result.Resources, daemonSetResource(d, pods))
		}
	}

	if jobs, err := api.ListJobs(ctx, namespace); err != nil {
		warn(KindJob, err)
	} else {
		for _, j := range jobs {
			result.Resources = append(result.Resources, jobResource(j))
		}
	}

	if cronJobs, err := api.ListCronJobs(ctx, namespace); err != nil {
		warn(KindCronJob, err)
	} else {
		for _, c := range cronJobs {
			result.Resources = append(result.Resources, cronJobResource(c))
		}
	}

	if services, err := api.ListServices(ctx, namespace); err != nil {
		warn(KindService, err)
	} else {
		for _, s := range services {
			result.Resources = append(result.Resources, serviceResource(s))
		}
	}

	if ingresses, err := api.ListIngresses(ctx, namespace); err != nil {
		warn(KindIngress, err)
	} else {
		for _, i := range ingresses {
			result.Resources = append(result.Resources, ingressResource(i))
		}
	}

	// Gateway API is a CRD; ErrNotFound means it's simply not installed
	// in this cluster, which is a normal, expected state — not
	// surfaced as a warning (every other kind's failures are, since
	// core/apps/batch/networking APIs always exist on any real
	// cluster, so their absence would be a genuine problem).
	if gateways, err := api.ListGateways(ctx, namespace); err != nil {
		if !errors.Is(err, kubeclient.ErrNotFound) {
			warn(KindGateway, err)
		}
	} else {
		for _, g := range gateways {
			result.Resources = append(result.Resources, Resource{
				Namespace: g.Metadata.Namespace, Kind: KindGateway, Name: g.Metadata.Name,
				Status:     StatusNotApplicable,
				Metadata:   map[string]any{"gateway_class_name": g.Spec.GatewayClassName},
				LastSeenAt: now(),
			})
		}
	}

	if configMaps, err := api.ListConfigMaps(ctx, namespace); err != nil {
		warn(KindConfigMap, err)
	} else {
		for _, c := range configMaps {
			result.Resources = append(result.Resources, Resource{
				Namespace: c.Metadata.Namespace, Kind: KindConfigMap, Name: c.Metadata.Name,
				Status: StatusNotApplicable, Metadata: map[string]any{}, LastSeenAt: now(),
			})
		}
	}

	if secrets, err := api.ListSecrets(ctx, namespace); err != nil {
		warn(KindSecret, err)
	} else {
		for _, s := range secrets {
			// Only the secret's name and type are ever recorded — spec
			// §7.5 "Secret names", never values; kubeclient.SecretSummary
			// has no field that could carry a value here even by mistake.
			result.Resources = append(result.Resources, Resource{
				Namespace: s.Metadata.Namespace, Kind: KindSecret, Name: s.Metadata.Name,
				Status: StatusNotApplicable, Metadata: map[string]any{"secret_type": s.Type}, LastSeenAt: now(),
			})
		}
	}

	return result, nil
}

func now() time.Time { return time.Now() }

// labelsMatch reports whether every key/value in selector is present in
// labels. An empty selector matches nothing here — Kubernetes treats an
// empty selector as "match everything" for some object kinds, but that's
// never useful for correlating a specific workload's pods, so we
// deliberately don't replicate it.
func labelsMatch(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// matchedPodStats aggregates restart counts across every pod in
// namespace whose labels satisfy selector.
func matchedPodStats(pods []kubeclient.Pod, namespace string, selector map[string]string) (restartCount int32, matchedCount int) {
	for _, p := range pods {
		if p.Metadata.Namespace != namespace || !labelsMatch(selector, p.Metadata.Labels) {
			continue
		}
		matchedCount++
		for _, cs := range p.Status.ContainerStatuses {
			restartCount += cs.RestartCount
		}
	}
	return restartCount, matchedCount
}

func replicaStatus(desired, ready int32) string {
	switch {
	case desired == 0:
		return StatusUnknown
	case ready >= desired:
		return StatusHealthy
	default:
		return StatusDegraded
	}
}

func containerMetadata(containers []kubeclient.ContainerSpec) []map[string]any {
	out := make([]map[string]any, 0, len(containers))
	for _, c := range containers {
		out = append(out, map[string]any{
			"name":                c.Name,
			"image":               c.Image,
			"resource_requests":   c.Resources.Requests,
			"resource_limits":     c.Resources.Limits,
			"has_liveness_probe":  c.LivenessProbe != nil,
			"has_readiness_probe": c.ReadinessProbe != nil,
		})
	}
	return out
}

func deploymentResource(d kubeclient.Deployment, pods []kubeclient.Pod) Resource {
	var desired int32
	if d.Spec.Replicas != nil {
		desired = *d.Spec.Replicas
	}
	ready := d.Status.ReadyReplicas
	restarts, _ := matchedPodStats(pods, d.Metadata.Namespace, d.Spec.Selector.MatchLabels)
	return Resource{
		Namespace: d.Metadata.Namespace, Kind: KindDeployment, Name: d.Metadata.Name,
		DesiredReplicas: &desired, ReadyReplicas: &ready, RestartCount: &restarts,
		Status:     replicaStatus(desired, ready),
		Metadata:   map[string]any{"containers": containerMetadata(d.Spec.Template.Spec.Containers)},
		LastSeenAt: now(),
	}
}

func statefulSetResource(s kubeclient.StatefulSet, pods []kubeclient.Pod) Resource {
	var desired int32
	if s.Spec.Replicas != nil {
		desired = *s.Spec.Replicas
	}
	ready := s.Status.ReadyReplicas
	restarts, _ := matchedPodStats(pods, s.Metadata.Namespace, s.Spec.Selector.MatchLabels)
	return Resource{
		Namespace: s.Metadata.Namespace, Kind: KindStatefulSet, Name: s.Metadata.Name,
		DesiredReplicas: &desired, ReadyReplicas: &ready, RestartCount: &restarts,
		Status:     replicaStatus(desired, ready),
		Metadata:   map[string]any{"containers": containerMetadata(s.Spec.Template.Spec.Containers)},
		LastSeenAt: now(),
	}
}

func daemonSetResource(d kubeclient.DaemonSet, pods []kubeclient.Pod) Resource {
	desired := d.Status.DesiredNumberScheduled
	ready := d.Status.NumberReady
	restarts, _ := matchedPodStats(pods, d.Metadata.Namespace, d.Spec.Selector.MatchLabels)
	return Resource{
		Namespace: d.Metadata.Namespace, Kind: KindDaemonSet, Name: d.Metadata.Name,
		DesiredReplicas: &desired, ReadyReplicas: &ready, RestartCount: &restarts,
		Status:     replicaStatus(desired, ready),
		Metadata:   map[string]any{"containers": containerMetadata(d.Spec.Template.Spec.Containers)},
		LastSeenAt: now(),
	}
}

func jobResource(j kubeclient.Job) Resource {
	status := StatusUnknown
	switch {
	case j.Status.Failed > 0:
		status = StatusDegraded
	case j.Status.Succeeded > 0 || j.Status.Active > 0:
		status = StatusHealthy
	}
	return Resource{
		Namespace: j.Metadata.Namespace, Kind: KindJob, Name: j.Metadata.Name,
		Status: status,
		Metadata: map[string]any{
			"active": j.Status.Active, "succeeded": j.Status.Succeeded, "failed": j.Status.Failed,
			"containers": containerMetadata(j.Spec.Template.Spec.Containers),
		},
		LastSeenAt: now(),
	}
}

func cronJobResource(c kubeclient.CronJob) Resource {
	suspended := c.Spec.Suspend != nil && *c.Spec.Suspend
	return Resource{
		Namespace: c.Metadata.Namespace, Kind: KindCronJob, Name: c.Metadata.Name,
		Status: StatusNotApplicable,
		Metadata: map[string]any{
			"schedule": c.Spec.Schedule, "suspended": suspended, "last_schedule_time": c.Status.LastScheduleTime,
		},
		LastSeenAt: now(),
	}
}

func serviceResource(s kubeclient.Service) Resource {
	ports := make([]map[string]any, 0, len(s.Spec.Ports))
	for _, p := range s.Spec.Ports {
		ports = append(ports, map[string]any{"name": p.Name, "port": p.Port, "protocol": p.Protocol})
	}
	return Resource{
		Namespace: s.Metadata.Namespace, Kind: KindService, Name: s.Metadata.Name,
		Status: StatusNotApplicable,
		Metadata: map[string]any{
			"type": s.Spec.Type, "cluster_ip": s.Spec.ClusterIP, "ports": ports, "selector": s.Spec.Selector,
		},
		LastSeenAt: now(),
	}
}

// ingressResource links an Ingress to the Service names its rules
// route to (spec §7.5 "Ingress and service mapping"). It records
// referenced service names even if no matching Service was actually
// discovered (e.g. it's in a different namespace or was filtered out) —
// that mismatch is itself useful information, not hidden.
func ingressResource(i kubeclient.Ingress) Resource {
	var hosts []string
	backendSeen := map[string]bool{}
	var backends []string
	for _, rule := range i.Spec.Rules {
		if rule.Host != "" {
			hosts = append(hosts, rule.Host)
		}
		for _, p := range rule.HTTP.Paths {
			name := p.Backend.Service.Name
			if name != "" && !backendSeen[name] {
				backendSeen[name] = true
				backends = append(backends, name)
			}
		}
	}
	return Resource{
		Namespace: i.Metadata.Namespace, Kind: KindIngress, Name: i.Metadata.Name,
		Status:     StatusNotApplicable,
		Metadata:   map[string]any{"hosts": hosts, "backend_services": backends},
		LastSeenAt: now(),
	}
}
