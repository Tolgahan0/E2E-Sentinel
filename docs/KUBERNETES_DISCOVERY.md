# Kubernetes Discovery

Implemented as of Phase 10 (spec §7.5). Optional and off by default —
see [Configuration](#configuration) — every other feature is unaffected
when it's unconfigured.

## What's detected

Read-only, via `internal/kubeclient` (a hand-rolled minimal Kubernetes
API client, the same "small capability surface" philosophy as
`internal/dockerclient` — not the full `client-go` SDK) and correlated
into `kube_resources` rows by `internal/kubediscovery`:

- Namespaces
- Deployments, StatefulSets, DaemonSets (with desired/ready replica
  counts and aggregated container restart counts, matched to pods by
  label selector)
- Jobs, CronJobs
- Services and Ingresses (an Ingress records the Service names its
  rules route to — spec §7.5 "ingress and service mapping")
- Gateway API `Gateway` objects, best-effort — a cluster without the
  Gateway API CRD installed simply reports none, not an error
- ConfigMap and Secret **names only** — `internal/kubeclient`'s
  `ConfigMapSummary`/`SecretSummary` types have no `data`/`stringData`
  field at all, so a value can't be decoded into memory even by mistake
- Events and pod logs — fetched live on request (`GET
  /projects/{id}/kube/events`, `GET
  /projects/{id}/kube/pods/{pod}/logs`), never persisted; logs are
  capped and non-streaming (`internal/kubeclient.MaxLogTailLines`)

There is no create/update/patch/delete anywhere in this package — spec
§2's "must not apply Kubernetes resources" rule, same as Docker
discovery never mutates a container.

## Partial failures degrade gracefully

A read-only ClusterRole (see below) may legitimately not grant every
resource kind, and the Gateway API may not be installed. Neither fails
a whole discovery run: `kubediscovery.Discover` records a warning per
kind and returns everything it could still list. `POST
/projects/{id}/kube-discover` always returns `200` with whatever
succeeded, plus a `warnings` array — never a `500` for a partial,
expected gap.

## Least-privilege RBAC

[`deploy/k8s/read-only-clusterrole.yaml`](../deploy/k8s/read-only-clusterrole.yaml)
is a ready-to-apply example (spec §7.5 "provide a read-only ClusterRole
example"): a ServiceAccount plus a ClusterRole granting only `get`/`list`
on exactly the resource kinds above — no `watch`, no write verb, no
`secrets`/`configmaps` fields access beyond what the Kubernetes API
itself always returns for `get`/`list` (E2E Sentinel's own client simply
never decodes the value fields — RBAC can't restrict *which fields* of
an object come back, so the value-never-touched guarantee lives in
`internal/kubeclient`, not in the RBAC rules). To scope discovery to one
namespace instead of the whole cluster, use a `Role`/`RoleBinding` in
that namespace instead of the `ClusterRole`/`ClusterRoleBinding`, and set
`SENTINEL_KUBE_NAMESPACE`.

## Two ways to connect

1. **A kubeconfig file** (`SENTINEL_KUBE_CONFIG_PATH`) — the local-dev
   and "sentinel-api runs outside the cluster it discovers" case. Only
   plain bearer-token and client-certificate authentication are
   supported; a kubeconfig using an `exec`/`auth-provider` plugin (cloud
   CLI credential helpers) produces a clear startup error rather than a
   silent partial connection.
2. **In-cluster ServiceAccount** (auto-detected when
   `SENTINEL_KUBE_CONFIG_PATH` is unset and the standard
   `KUBERNETES_SERVICE_HOST`/`_PORT` env vars and
   `/var/run/secrets/kubernetes.io/serviceaccount/` mount are present) —
   the real deployment case: run sentinel-api itself as a pod using the
   `sentinel-discovery` ServiceAccount from the example manifest.

## Configuration

- `SENTINEL_KUBE_CONFIG_PATH` (optional, empty by default = disabled) —
  path to a kubeconfig file, as seen *inside* the `sentinel-api`
  container.
- `SENTINEL_KUBE_NAMESPACE` (optional, empty = cluster-wide) — scope
  discovery to one namespace. Set this if your ClusterRole is actually a
  namespace-scoped `Role`/`RoleBinding` — a cluster-wide list against a
  namespace-scoped grant just 403s.

`docker-compose.yml` mounts `${SENTINEL_KUBE_CONFIG_HOST_PATH:-/dev/null}`
(a harmless placeholder when unset) to `/kube/config:ro`; set that host
variable plus `SENTINEL_KUBE_CONFIG_PATH=/kube/config` together to test
against a real cluster locally (e.g. a throwaway `kind` cluster).

## What's not implemented

Helm deployment of E2E Sentinel itself (spec §24.5) — a separate,
later concern from *discovering* a cluster, which is this phase's
actual scope. Live log streaming/follow and cluster-side mutation
(scaling, restarting a workload, editing a manifest) are deliberately
out of scope — this is a read-only discovery and diagnostic feature,
not a cluster management console.
