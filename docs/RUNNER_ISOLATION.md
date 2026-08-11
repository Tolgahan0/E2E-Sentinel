# Runner Isolation

Implemented as of Phase 5. By default, every test run executes in its
own disposable Docker container, launched by `sentinel-api` itself over
the mounted Docker socket (Docker-outside-of-Docker) rather than a
restricted proxy — see [ADR-worthy trade-off](#why-a-direct-socket-mount)
below. `SENTINEL_EXECUTION_MODE=local` (or `auto` falling back to it
when Docker isn't reachable) trades that isolation guarantee for
running with no container runtime at all — see
[Local process execution mode](#local-process-execution-mode).

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

## Local process execution mode

Docker was never meant to be a hard requirement to *use* E2E Sentinel —
only to get the strongest isolation guarantee for test execution.
`SENTINEL_EXECUTION_MODE` (default `auto`) picks between two materially
different trust models:

- **`docker`** — always the disposable-container runners above. Never
  falls back to anything else; if `SENTINEL_RUNNER_HOST_WORKSPACE_DIR`
  is unset or the daemon doesn't answer a ping at startup, test
  execution is simply unconfigured (`POST /tests/{id}/run` returns
  `503`) rather than silently running with weaker isolation.
- **`local`** — `LocalPlaywrightRunner`/`LocalWebSocketRunner`
  (`internal/runs/local_*.go`) run the generated spec as a plain host
  process (`playwright test` / `node <script>`) instead of inside a
  container. **No per-run isolation**: the test process runs with
  `sentinel-api`'s own privileges, on the same filesystem and network
  namespace as wherever `sentinel-api` itself is running — a materially
  weaker boundary than the Docker mode's disposable, resource-limited,
  non-root container. This is the trade a machine with no Docker
  installed at all is making, not an oversight.
- **`auto`** (the default) — uses `docker` when
  `SENTINEL_RUNNER_HOST_WORKSPACE_DIR` is set and the daemon actually
  answers a ping at startup; otherwise falls back to `local`
  automatically. Falling back silently is intentional here — Docker was
  never a hard requirement for this mode specifically, unlike `docker`
  requested by name above.

This decision is made once at process startup (logged either way), the
same as every other optional-capability field in this codebase (Docker
discovery, Kubernetes discovery, AI providers) — it is not re-evaluated
per run. Restart `sentinel-api` to pick up a Docker daemon that's since
become reachable.

**Requirements for local mode**: `playwright` and `node` resolved on
`sentinel-api`'s own `$PATH` — install globally exactly like the runner
images do (`npm install -g @playwright/test && npx playwright install
--with-deps` for Playwright tests; `npm install -g ws` for WebSocket
tests), since a per-run workspace has no `package.json`/`node_modules`
of its own. Missing tools surface as a clear, actionable error from
`Validate` (wrapping `ErrLocalToolMissing`) rather than a cryptic
process-spawn failure. In practice this means local mode is realistic
when `sentinel-api` runs as a bare host process
([docs/LOCAL_DEVELOPMENT.md](LOCAL_DEVELOPMENT.md)) — the default
`sentinel-api` Docker image is a `distroless` base with no shell, let
alone Node.js, so local mode inside that same container will always
fail its own tool check.

**Cancellation is per-process, not per-daemon**: `DockerPlaywrightRunner
.Cancel` stops a container by a deterministic name via the Docker
daemon — external state that any process can reach. A local process has
no equivalent external authority holding it; `LocalPlaywrightRunner`/
`LocalWebSocketRunner` track each run's cancel function in an in-memory
registry instead, so `Cancel` only works from the same `sentinel-api`
process that started the run. Not a new limitation in practice — there
is no multi-replica `sentinel-api` deployment today regardless — but
worth knowing if that ever changes.

**Mutating/load tests deserve extra caution in this mode**: the
"disposable container, so a runaway or mutating test is contained" 
assumption that spec §11.1 leans on doesn't hold once execution is a
plain host process — see [docs/THREAT_MODEL.md](THREAT_MODEL.md)'s
"Generated test code execution" row.

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

### What an environment's `base_url` needs to actually be reachable

Every runner container (`DockerPlaywrightRunner`/`DockerWebSocketRunner`)
is created with `NetworkMode: "bridge"` — Docker's default bridge
network, which has **no** built-in DNS resolution for container names
(unlike a `docker-compose.yml`-created custom network, e.g.
`e2e-sentinel_default`, where `sentinel-web` resolves to that
container's IP automatically). Setting an environment's `base_url` to a
Compose service name (`http://sentinel-web:9090`) will fail with
`getaddrinfo ENOTFOUND` from inside the runner — this looks like a
system bug the first time you hit it, but it's the expected behavior of
the default bridge network, not a defect.

Point `base_url` at whatever the target actually publishes on the host
instead — `http://host.docker.internal:<published-port>` on
Docker Desktop (macOS/Windows), the same convention already used for
`SENTINEL_OLLAMA_AUTODETECT_URL` (see
[docs/AI_PROVIDER_GUIDE.md](AI_PROVIDER_GUIDE.md)) — or a real external
URL if the target isn't local at all.

## Artifacts and retention

`internal/artifacts.FileStore` stores stdout/stderr always; screenshots
on *every* run (`screenshot: { mode: 'on', fullPage: true }` — needed
by internal/visualdiff's baseline diffing, see
[docs/VISUAL_REGRESSION.md](VISUAL_REGRESSION.md)); video/trace only on
failure (`video: retain-on-failure` / `trace: retain-on-failure`).
Metadata
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
