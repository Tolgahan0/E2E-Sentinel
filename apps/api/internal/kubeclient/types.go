package kubeclient

// These types are a deliberately narrow subset of each Kubernetes
// resource's real schema — only the fields Phase 10 discovery actually
// reads (spec §7.5). Resource quantities (CPU/memory requests/limits)
// always marshal as JSON strings in the real API (e.g. "500m", "128Mi"),
// so a plain map[string]string round-trips them without needing
// apimachinery's resource.Quantity type.

// ObjectMeta is the common "metadata" block on every Kubernetes object.
type ObjectMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace"`
	Labels            map[string]string `json:"labels"`
	CreationTimestamp string            `json:"creationTimestamp"`
}

// Namespace is a cluster namespace.
type Namespace struct {
	Metadata ObjectMeta `json:"metadata"`
}

// LabelSelector is a workload's pod selector (spec.selector on
// Deployment/StatefulSet/DaemonSet/Service) — matchLabels only; the
// less common matchExpressions form is not supported, matching this
// package's read-only-discovery-aid scope, not full API fidelity.
type LabelSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

// ContainerResources is a container's resource requests/limits.
type ContainerResources struct {
	Requests map[string]string `json:"requests"`
	Limits   map[string]string `json:"limits"`
}

// ContainerSpec is one container in a pod template.
type ContainerSpec struct {
	Name           string             `json:"name"`
	Image          string             `json:"image"`
	Resources      ContainerResources `json:"resources"`
	LivenessProbe  map[string]any     `json:"livenessProbe"`
	ReadinessProbe map[string]any     `json:"readinessProbe"`
}

// PodTemplateSpec is the pod template embedded in every workload kind.
type PodTemplateSpec struct {
	Spec struct {
		Containers []ContainerSpec `json:"containers"`
	} `json:"spec"`
}

// Deployment (apps/v1).
type Deployment struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Replicas *int32          `json:"replicas"`
		Selector LabelSelector   `json:"selector"`
		Template PodTemplateSpec `json:"template"`
	} `json:"spec"`
	Status struct {
		Replicas      int32 `json:"replicas"`
		ReadyReplicas int32 `json:"readyReplicas"`
	} `json:"status"`
}

// StatefulSet (apps/v1).
type StatefulSet struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Replicas *int32          `json:"replicas"`
		Selector LabelSelector   `json:"selector"`
		Template PodTemplateSpec `json:"template"`
	} `json:"spec"`
	Status struct {
		Replicas      int32 `json:"replicas"`
		ReadyReplicas int32 `json:"readyReplicas"`
	} `json:"status"`
}

// DaemonSet (apps/v1) has no desired "replicas" concept — its desired
// count is however many nodes match its scheduling constraints.
type DaemonSet struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Selector LabelSelector   `json:"selector"`
		Template PodTemplateSpec `json:"template"`
	} `json:"spec"`
	Status struct {
		DesiredNumberScheduled int32 `json:"desiredNumberScheduled"`
		NumberReady            int32 `json:"numberReady"`
	} `json:"status"`
}

// Job (batch/v1).
type Job struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Template PodTemplateSpec `json:"template"`
	} `json:"spec"`
	Status struct {
		Active    int32 `json:"active"`
		Succeeded int32 `json:"succeeded"`
		Failed    int32 `json:"failed"`
	} `json:"status"`
}

// CronJob (batch/v1).
type CronJob struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Schedule string `json:"schedule"`
		Suspend  *bool  `json:"suspend"`
	} `json:"spec"`
	Status struct {
		LastScheduleTime string `json:"lastScheduleTime"`
	} `json:"status"`
}

// ServicePort is one exposed port on a Service.
type ServicePort struct {
	Name       string `json:"name"`
	Port       int32  `json:"port"`
	TargetPort any    `json:"targetPort"` // int or named port string
	Protocol   string `json:"protocol"`
}

// Service (core/v1) — a Kubernetes Service, not an E2E Sentinel
// DiscoveredService.
type Service struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Type      string            `json:"type"`
		ClusterIP string            `json:"clusterIP"`
		Selector  map[string]string `json:"selector"`
		Ports     []ServicePort     `json:"ports"`
	} `json:"spec"`
}

// IngressBackend names the Service an Ingress path routes to.
type IngressBackend struct {
	Service struct {
		Name string `json:"name"`
	} `json:"service"`
}

// IngressRule is one host's routing rules.
type IngressRule struct {
	Host string `json:"host"`
	HTTP struct {
		Paths []struct {
			Path    string         `json:"path"`
			Backend IngressBackend `json:"backend"`
		} `json:"paths"`
	} `json:"http"`
}

// Ingress (networking.k8s.io/v1).
type Ingress struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		Rules []IngressRule `json:"rules"`
	} `json:"spec"`
}

// Gateway (gateway.networking.k8s.io) — best-effort only; the Gateway
// API is a CRD, absent from clusters that don't install it.
type Gateway struct {
	Metadata ObjectMeta `json:"metadata"`
	Spec     struct {
		GatewayClassName string `json:"gatewayClassName"`
	} `json:"spec"`
}

// ConfigMapSummary intentionally omits ConfigMap's "data"/"binaryData"
// fields — discovery only ever needs to know a ConfigMap exists (spec
// §7.5 "ConfigMaps"), never its contents.
type ConfigMapSummary struct {
	Metadata ObjectMeta `json:"metadata"`
}

// SecretSummary intentionally omits Secret's "data"/"stringData" fields
// — spec §7.5 says "Secret names", never values. Type (e.g.
// "kubernetes.io/tls", "Opaque") is metadata about the secret's shape,
// not its content, so it's safe to keep.
type SecretSummary struct {
	Metadata ObjectMeta `json:"metadata"`
	Type     string     `json:"type"`
}

// ContainerStatus is one container's runtime status within a Pod.
type ContainerStatus struct {
	Name         string `json:"name"`
	Ready        bool   `json:"ready"`
	RestartCount int32  `json:"restartCount"`
}

// Pod (core/v1).
type Pod struct {
	Metadata ObjectMeta `json:"metadata"`
	Status   struct {
		Phase             string            `json:"phase"`
		ContainerStatuses []ContainerStatus `json:"containerStatuses"`
	} `json:"status"`
}

// Event (core/v1) — a cluster event, e.g. a failed probe or an
// unschedulable pod (spec §7.5 "Events").
type Event struct {
	Metadata       ObjectMeta `json:"metadata"`
	InvolvedObject struct {
		Kind      string `json:"kind"`
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"involvedObject"`
	Reason        string `json:"reason"`
	Message       string `json:"message"`
	Type          string `json:"type"` // "Normal" or "Warning"
	Count         int32  `json:"count"`
	LastTimestamp string `json:"lastTimestamp"`
}
