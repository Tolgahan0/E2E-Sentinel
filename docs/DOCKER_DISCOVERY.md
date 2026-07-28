# Docker Discovery

Implemented as of Phase 2. Two independent sources feed into a project's
discovered services:

1. **Compose file parsing** (`internal/compose`) — always available.
   Whenever repository discovery (Phase 1) finds a `docker-compose.yml` /
   `compose.yml`, its services, images, declared ports, `depends_on`,
   profiles, command/entrypoint, networks, volumes, and environment
   variable **names** (never values — spec §7.4) are parsed directly from
   the YAML. No subprocess is ever spawned to do this (no `docker compose
   config` shell-out): parsing the file ourselves avoids the
   command-injection surface a shell-out would introduce (spec §23.3) and
   works even though the API's container image has no `docker` CLI.
2. **Live Docker daemon status** (`internal/dockerclient`) — optional,
   best-effort. If reachable, running containers are matched to
   compose-declared services by the `com.docker.compose.service` label,
   enriching each service with its real container name, running state,
   and actual bound ports.

## Docker-unavailable is a normal state, not an error

`internal/dockerclient.Client` is a deliberately minimal, read-only
wrapper around exactly two Docker Engine API calls (`/_ping`,
`/containers/json`) over the Unix socket — not the full Docker SDK — to
keep the capability surface small (spec §7.3: "must not assume Docker
socket access is safe"). Every method returns `ErrUnavailable` when the
socket is missing or unreachable, and callers treat that as expected: a
service still gets recorded from the compose file with
`metadata.status = "unknown"`, rendered in the panel as **"not
observed"** — never as "not running". Those are different claims (spec
§9.4): one means "we don't know", the other would mean "we checked and
it's down", which would be false.

## Enabling live status: mounting the Docker socket

By default, `docker-compose.yml` does **not** mount the Docker socket
into `sentinel-api` — mounting it grants extensive host capability (spec
§24.4) and E2E Sentinel must not enable that without an explicit,
informed choice. To enable live container status:

```yaml
# docker-compose.yml, sentinel-api service
volumes:
  - ./migrations:/app/migrations:ro
  - ${SENTINEL_WORKSPACE:-./workspace}:/workspace:ro
  - /var/run/docker.sock:/var/run/docker.sock:ro   # opt-in — see docs/DOCKER_DISCOVERY.md
```

Prefer, in order:

1. **Don't mount it** — repository-declared services (image, ports,
   dependencies) are still fully discovered; you just won't see live
   running/health status.
2. **A restricted Docker socket proxy** (e.g.
   [docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy))
   exposing only `GET /containers/json` and `GET /_ping`, pointed at by
   `SENTINEL_DOCKER_SOCKET`/a proxied TCP address — matches this
   package's own capability surface, so nothing is lost.
3. **Direct socket mount** — only on hosts you fully trust, understanding
   that any code able to reach that socket can control every container
   on the host.

## Configuration

- `SENTINEL_DOCKER_SOCKET` (optional, default `/var/run/docker.sock`) —
  path to the Docker Engine Unix socket as seen *inside* the
  `sentinel-api` container.

## What's not implemented yet

Kubernetes discovery (Phase 10) is separate — see
[docs/KUBERNETES_DISCOVERY.md](KUBERNETES_DISCOVERY.md). Docker Swarm and
non-Compose container topologies are out of scope.
