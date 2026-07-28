# Runner Isolation

**Status: not yet implemented.** Lands in Phase 5. See spec §11.

## Planned behavior

Every test run executes in a disposable Docker container: CPU/memory/time
limits, a read-only mount of the target repository, a separate writable
temporary workspace, a dedicated artifact directory, no Docker socket
inside the runner, no host root filesystem access, and a non-root user.
The `Runner` interface (`Name`, `Validate`, `Prepare`, `Execute`, `Cancel`,
`CollectArtifacts`, `Cleanup`) will live in `internal/runners`.

## Relationship to this repo's own containers

E2E Sentinel's *own* `sentinel-api`/`sentinel-web` containers (distroless
non-root / non-root Node) already follow the same non-root, minimal-image
principles this feature will apply to test runners — see
[docs/SECURITY_MODEL.md](SECURITY_MODEL.md).
