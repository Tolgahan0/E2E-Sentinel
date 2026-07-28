# Kubernetes Discovery

**Status: not yet implemented.** Deliberately deferred until after the
Docker MVP is stable (spec §7.5, Phase 10).

## Planned behavior

Read-only discovery of namespaces, Deployments, StatefulSets, DaemonSets,
Jobs, CronJobs, Services, Ingress/Gateway API, ConfigMaps, Secret *names*
(never values), Pods, containers, probes, replica health, restart counts,
resource requests/limits, events, and relevant logs — all via a
least-privilege, read-only `ClusterRole` (an example will ship alongside
this feature).

## What exists today

No code path in Phase 0 talks to a Kubernetes API server.
