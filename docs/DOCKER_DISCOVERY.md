# Docker Discovery

**Status: not yet implemented.** Lands in Phase 2. See spec §7.3–§7.4.

## Planned behavior

Two supported modes: direct Docker socket, or a restricted Docker proxy
(recommended in documentation once available). Discovery reads container
lists, inspects containers, parses labels/networks/ports/health/mounts,
and reads Compose project metadata (services, profiles, depends-on,
volumes, env var *names* — never values) to build app-to-service
relationships. Docker socket access is never assumed safe; a
Docker-unavailable state must degrade gracefully rather than error the
whole discovery run.

## What exists today

No code path in Phase 0 talks to the Docker daemon or reads
`docker-compose.yml` of a target project. E2E Sentinel's own
`docker-compose.yml` (this repo's) is unrelated to that future feature.
