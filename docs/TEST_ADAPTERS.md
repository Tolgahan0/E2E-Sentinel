# Test Adapters (Phase 11)

Spec §25 Phase 11 lists eight tools under "Advanced Test Adapters":
WebSocket, Maestro, Detox, k6, ZAP, Nuclei, Schemathesis, Pact, Kafka.
The spec itself marks this phase "Later" — the lowest-priority, most
speculative item in the roadmap, deliberately broader and less specified
than every phase before it.

Consistent with this project's rule against overclaiming (spec §36):
**one of these eight — the WebSocket adapter — is fully implemented,
end-to-end, and live-verified.** The other seven are not implemented.
This document describes both honestly: what the WebSocket adapter
actually does, and the concrete extension pattern a future adapter would
follow, so "not implemented" means "designed and scoped", not "unknown
how".

## What "adapter" means in this codebase

Before Phase 11, `internal/runs.Runner` was already a generic interface
— `DockerPlaywrightRunner` was simply its only implementation, not
something baked into the interface's shape. Adding a second adapter
turned out to require exactly four changes, none of them to the `Runner`
interface itself:

1. **Route/finding detection** (`internal/routes`): a new `Kind` value
   plus an extractor that recognizes the adapter's target (a WebSocket
   URL literal, an OpenAPI-like schema, a Kafka topic reference — whatever
   is evidence-based and deterministic for that protocol).
2. **Test planning** (`internal/planning`): a rule in `testCasesForRoute`
   for the new `Kind`, and a `frameworkFor` mapping to a new framework
   string.
3. **Spec generation** (`internal/testgen`): a new generator function,
   dispatched on `TestCaseInput.Framework`, producing whatever the new
   runner actually executes (a script, a config file, a manifest).
4. **Execution** (`internal/runs` + `internal/httpserver`): a new
   `Runner` implementation, a new field on `Dependencies` (not a map —
   see below), and two one-line additions to `runnerFor`/`runnerByName`
   in `runs_handlers.go`.

`Dependencies` uses one named field per runner (`Runner`,
`WebSocketRunner`) rather than a generic `map[string]runs.Runner`
registry — consistent with every other optional capability in this
struct (`Docker`, `Secrets`, `Auth`, `Kube`: one field, nil when
unconfigured, a comment explaining what nil means). A third adapter
would add a third named field the same way.

## WebSocket adapter (implemented)

- **Detection** (`internal/routes.KindWebSocket`,
  `extractWebSocketURLs`): every scanned source file (`.js/.ts/.jsx/.tsx/
  .mjs/.cjs/.go/.py`) is checked for a `ws://`/`wss://` URL literal via
  regex — deliberately protocol-literal rather than per-client-API
  parsing (`new WebSocket(...)` in JS, `websockets.connect(...)` in
  Python, etc. all end up embedding the same kind of literal), so one
  extractor covers every language this project already scans. Medium
  confidence, same as every other regex-matched route — spec §9.4's
  confidence-level honesty applies here too.
- **Planning** (`internal/planning`): a `KindWebSocket` route produces
  one `CategoryConnectivity` test case — "connection succeeds and yields
  at least one message within a timeout" — with `Framework: "websocket"`.
  Non-mutating, production-safe, same as a health-check route.
- **Generation** (`internal/testgen.generateWebSocketSpec`): a plain
  Node.js script (no Playwright, no project-local `package.json`) using
  the globally-installed `ws` package. Connects, waits up to 5 seconds
  for any message, `process.exit(0)`/`process.exit(1)` accordingly — the
  same smoke-level honesty as the Playwright generators (message
  *content* is never asserted, since no schema/AI input exists).
- **Execution** (`internal/runs.DockerWebSocketRunner` +
  `deploy/docker/Dockerfile.runner-websocket`): the same disposable-
  container isolation model as `DockerPlaywrightRunner` (spec §11.1),
  pointed at a dedicated, much smaller image — plain
  `node:20-alpine` plus `ws`, no browser stack, since a WebSocket smoke
  test needs neither Chromium/Firefox/WebKit nor Playwright itself.
  `CollectArtifacts` is a no-op (no screenshots/videos/traces exist for
  a plain script) — stdout/stderr are still saved generically by
  `executeRunAsync`, same as every runner.
- **Selection**: `TestCase.Framework == "websocket"` routes to
  `Dependencies.WebSocketRunner` instead of `Dependencies.Runner`
  (`runs_handlers.go`'s `runnerFor`), and skips the environment
  `base_url` requirement entirely — a WebSocket route's `RoutePath` is
  already a complete `ws://`/`wss://` URL (see `routes.Route`'s doc
  comment), never something to join with an HTTP base URL.
- **Configuration**: `SENTINEL_WEBSOCKET_RUNNER_IMAGE` (optional,
  defaults to `e2e-sentinel-websocket-runner:latest`) — shares
  `SENTINEL_RUNNER_HOST_WORKSPACE_DIR` with the Playwright runner (same
  Docker-outside-of-Docker requirement, see
  [docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md)), so it's configured
  and unconfigured together with test execution generally, not as a
  separate opt-in switch.

## What's not implemented, and why

The remaining seven each require either a genuinely heavy external
runtime this project doesn't otherwise depend on, or a scope decision
(how deep to integrate) that spec §25's one-line mention doesn't answer
— building any of them with the same rigor as the WebSocket adapter
(real unit tests, a real live-verified run) means standing up that
external system first, which is a materially larger undertaking than
Phase 11's other seven words each suggest:

- **k6** (load testing): would need the `k6` binary in a dedicated
  runner image (straightforward, follows the WebSocket adapter's exact
  pattern) plus a *generator* that produces a meaningful k6 script from
  a `TestCase` — unlike a smoke test, a load-test script needs
  target RPS/duration/thresholds that don't exist anywhere in the
  current `TestCase` shape and would need new fields.
- **ZAP, Nuclei** (security scanning): both run as a scan against a
  *target*, not a generated script per test case — the natural
  integration point is closer to environment-level scanning ("scan this
  base_url") than to `internal/testgen`'s per-route model. That's a
  different shape of feature, not a drop-in fifth `Runner`.
- **Schemathesis** (API fuzzing from an OpenAPI spec): the closest fit to
  the existing per-route model (Phase 11's `internal/routes` already
  extracts OpenAPI paths), but real value requires actually invoking the
  Schemathesis CLI against the full spec document, not per-route — again
  a different shape than "one generated script per `TestCase`".
- **Pact** (contract testing): requires a Pact broker (a separate
  stateful service, not just a runner image) to publish/verify contracts
  against — meaningfully more infrastructure than a disposable container.
- **Kafka/event-stream testing**: requires a running Kafka broker to
  test against at all; there's no equivalent of "scan source for a
  literal URL" the way WebSocket's `ws://` detection works, since a
  topic name alone doesn't identify a target the way a URL does.
- **Maestro, Detox** (mobile UI testing): require a mobile emulator/
  simulator (Android or iOS) in the runner container — an entirely
  different, much heavier isolation environment than any adapter here,
  and arguably its own phase's worth of infrastructure work (device
  images, ADB/simulator plumbing) rather than a fifth `Runner`
  implementation.

None of these are stubbed, partially wired, or silently assumed to work
— `TestCase.Framework` has exactly two values today ("playwright"/"api",
pre-Phase-11) plus the new "websocket"; nothing in `internal/planning`,
`internal/testgen`, or `internal/runs` references k6/ZAP/Nuclei/
Schemathesis/Pact/Maestro/Detox/Kafka in any form. Section "What
'adapter' means in this codebase" above is what a future implementer
would actually follow.
