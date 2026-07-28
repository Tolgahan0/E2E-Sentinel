# Security Policy

E2E Sentinel is a self-hosted testing platform designed around the
principle that it must never modify source code, infrastructure,
deployments, or data without explicit, action-specific, auditable
approval. See [docs/SECURITY_MODEL.md](docs/SECURITY_MODEL.md) for the
current implementation state.

## Reporting a vulnerability

If you find a security issue in E2E Sentinel itself:

1. Do not open a public issue.
2. Email the maintainer (see repository metadata / commit author) with a
   description, reproduction steps, and impact assessment.
3. Allow time for a fix before any public disclosure.

## Scope notes

- Vulnerabilities in E2E Sentinel's own code, containers, or default
  configuration are in scope.
- Findings produced *by* E2E Sentinel about a target project under test
  are not a vulnerability in E2E Sentinel — report those through the
  target project's own process.
- This repository currently ships development dependencies (ESLint
  tooling, Next.js's bundled `postcss`/`sharp`) with known low-impact
  advisories in their transitive tree that have no available non-breaking
  fix upstream at time of writing. These affect the build toolchain only,
  not the shipped runtime containers' request-handling paths. Re-run `npm
  audit` before each release and upgrade when fixes land.
