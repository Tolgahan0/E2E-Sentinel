# Runner Isolation

Implemented as of Phase 5. Every test run executes in its own disposable
Docker container, launched by `sentinel-api` itself over the mounted
Docker socket (Docker-outside-of-Docker) rather than a restricted proxy —
see [ADR-worthy trade-off](#why-a-direct-socket-mount) below.

## Architecture

```text
sentinel-api (in its own container)
  |
  |  Docker Engine API over /var/run/docker.sock (mounted, group_add)
  v
Host Docker daemon
  |
  +-- disposable "sentinel-run-<runID>" container
        image: e2e-sentinel-playwright-runner:latest (pre-built, never pulled at runtime)
        bind mount: <host>/runner-workspaces/<runID> -> /workspace
        memory/CPU limited, network "bridge", non-root ("pwuser")
```

`internal/dockerclient` implements only the container lifecycle calls the
runner needs (create, start, wait, stop, logs, remove) — not the full
Docker SDK — keeping the capability surface deliberately narrow (spec
§7.3, §11.1).

`internal/runs.Runner` is the interface (spec §11.2, adapted — `Prepare`
is folded into `Execute` since there is no scheduling queue yet):

```go
type Runner interface {
    Name() string
    Validate(ctx context.Context, input RunInput) error
    Execute(ctx context.Context, input RunInput) (*RunResult, error)
    CollectArtifacts(ctx context.Context, runID string) ([]ArtifactFile, error)
    Cancel(ctx context.Context, runID string) error
    Cleanup(ctx context.Context, runID string) error
}
```

`DockerPlaywrightRunner` handles the "playwright"/"api" frameworks.
`DockerWebSocketRunner` (Phase 11, spec §25 "WebSocket adapter") handles
"websocket" — the same container-lifecycle shape, pointed at a much
smaller image (`deploy/docker/Dockerfile.runner-websocket`: plain
Node.js + the `ws` package, no browser stack). `internal/httpserver`'s
`Dependencies` has one field per runner (`Runner`, `WebSocketRunner`),
selected by `TestCase.Framework` — see
[docs/TEST_ADAPTERS.md](TEST_ADAPTERS.md) for the full picture,
including which of spec §25 Phase 11's other tools (Maestro, Detox, k6,
ZAP, Nuclei, Schemathesis, Pact, Kafka) remain a designed-but-
unimplemented extension point.

## Why a direct socket mount

Spec §7.3/§24.4 recommend a restricted socket proxy over a direct mount.
For Phase 5, a direct mount was chosen deliberately (see the user
decision recorded when this phase was scoped): it's the simplest path to
a working disposable-runner setup, matching how most self-hosted CI
tools (GitLab Runner's Docker executor, Drone, etc.) operate by default.
This grants `sentinel-api` significant host capability — document this
clearly to anyone deploying E2E Sentinel beyond a single trusted
developer machine, and prefer a restricted proxy (e.g.
[docker-socket-proxy](https://github.com/Tecnativa/docker-socket-proxy))
for any shared or production host.

## Docker-outside-of-Docker: the host-path requirement

Because `sentinel-api` talks to the **host's** Docker daemon (not a
daemon inside its own container), any bind-mount source it gives that
daemon for a new sibling container must be a path *on the host* — the
daemon has no idea `sentinel-api` is itself containerized, and resolves
bind sources against its own (the host's) filesystem.

This is why there are two related settings:

- `SENTINEL_RUNNER_WORKSPACE_DIR` (default `/runner-workspaces`) —
  where `sentinel-api` itself reads/writes generated spec files, from
  its own container's point of view.
- `SENTINEL_RUNNER_HOST_WORKSPACE_DIR` — the **same** directory's path
  on the Docker host. `make up` computes this automatically
  (`$(pwd)/runner-workspaces`) and passes it through; there is no safe
  default, so `docker-compose.yml` fails fast with a clear message if
  it's unset and you invoke `docker compose` directly instead of `make
  up`.

Both are bind-mounted from the *same* host directory
(`./runner-workspaces`), so a file `sentinel-api` writes at
`/runner-workspaces/<runID>/spec.ts` becomes visible to the newly
created runner container at `/workspace/spec.ts`, because the daemon
resolves `<host>/runner-workspaces/<runID>` — a real path it can see —
as the bind source for both mounts.

## Two more Docker-Desktop-specific fixes worth knowing about

Found and fixed during Phase 5's live-stack verification (not caught by
unit tests, which don't touch a real daemon):

- **Socket permission**: Docker Desktop exposes `/var/run/docker.sock`
  as `root:root`, mode `660`, inside its Linux VM — a non-root container
  (`sentinel-api` runs as distroless's fixed `nonroot` uid/gid `65532`)
  can't open it by default. `docker-compose.yml` adds `sentinel-api` to
  group `0` (root) via `group_add`, which grants socket access through
  standard Unix group permissions without running the process as root
  or granting any other capability. On a Linux host with rootful Docker,
  the socket is usually `root:docker` instead — set `SENTINEL_DOCKER_GID`
  to that group's real GID there.
- **`NODE_PATH`**: the runner image installs `@playwright/test` globally
  so a bind-mounted workspace (no `package.json`/`node_modules` of its
  own) can still run `playwright test`. A global npm install does **not**
  put a package on Node's `require()` resolution path for an arbitrary
  working directory — that's a separate mechanism from the CLI being on
  `$PATH`. `Dockerfile.runner-playwright` sets
  `ENV NODE_PATH=/usr/lib/node_modules` so the bind-mounted
  `playwright.config.ts`'s `import ... from '@playwright/test'` resolves
  regardless of where it's mounted.

## Test generation

`internal/testgen` deterministically generates a runnable spec from a
`TestCase` — no AI involved (spec §16.6). Given no schema or AI input,
generated assertions are necessarily smoke-level:

- Page routes (no HTTP method): navigate, assert no console/page error.
- API routes: call the endpoint, assert the response isn't a 5xx.
- WebSocket routes (Phase 11): connect, assert a message arrives within
  a timeout — see [docs/TEST_ADAPTERS.md](TEST_ADAPTERS.md).

This ceiling is deliberate and documented, not hidden — see spec §36's
prohibition on overclaiming coverage.

## Artifacts and retention

`internal/artifacts.FileStore` stores stdout/stderr always, and
screenshot/video/trace on failure (Playwright's own
`screenshot: only-on-failure` / `video: retain-on-failure` /
`trace: retain-on-failure` config, generated per run). Metadata
(checksum, MIME type, size, retention window) lives in Postgres; bytes
live on the local filesystem (spec §4.1 MVP backend), under a Docker
volume owned by a one-shot `artifacts-init` container (distroless has no
shell to `chown` its own volume). Retention windows are computed at save
time (14 days default / 30 days failed / 7 days passed, spec §12); the
periodic deletion job itself is a job-system feature (spec §21) not yet
built.

## Cancellation

`POST /runs/{id}/cancel` stops the run's container by a **deterministic
name** (`sentinel-run-<runID>`) rather than tracking an in-memory
container ID — this means cancellation works correctly even if the HTTP
request that started the run and the request that cancels it are served
by different goroutines (always true here, since execution runs in a
background goroutine) or, in a future multi-replica deployment,
different processes entirely.
