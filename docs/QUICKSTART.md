# Quickstart: integrate a project in one command

No AI coding assistant required — this is a plain shell script anyone
can run.

There are two ways to get the E2E Sentinel stack itself running. Pick
one, then `onboard.sh` (below) is identical either way.

## Running a release (no repo checkout)

If you just want to *use* E2E Sentinel — not develop it — you don't
need to clone this repository or have Go/Node installed. Every
component is published as a pre-built image on each release tag; the
installer downloads only what's needed to run them:

```bash
curl -fsSL https://raw.githubusercontent.com/Tolgahan0/E2E-Sentinel/main/install.sh | bash
```

This creates `./e2e-sentinel/` (override with `SENTINEL_INSTALL_DIR`),
writes a `.env` with a generated `POSTGRES_PASSWORD`, pulls the images
(`SENTINEL_VERSION` picks a tag; defaults to the latest GitHub release,
or `main` if none exist yet), and starts the stack via
`docker-compose.release.yml`. When it's done, the panel is at
`http://localhost:9090` and `./e2e-sentinel/scripts/onboard.sh` is
ready to use exactly as described below.

The same `SENTINEL_VERSION` is also passed into the running
`sentinel-api` container, so it knows its own version — `GET /version`
compares it against GitHub Releases and surfaces "an update is
available" on the panel's Dashboard and from `scripts/onboard.sh`
(read-only, opt-out via `SENTINEL_UPDATE_CHECK_ENABLED=false`). See
[README.md#staying-up-to-date](../README.md#staying-up-to-date).

To stop it: `cd e2e-sentinel && docker compose -f docker-compose.release.yml down`
(add `-v` only if you also want to delete the Postgres data).

## Running from source (for developing E2E Sentinel itself)

```bash
git clone <this-repo>
cd e2e-sentinel
make up
```

## Now onboard a project

Same command either way — `onboard.sh` detects which of the two setups
above it's sitting next to and brings the stack up itself if it isn't
already running:

```bash
./scripts/onboard.sh https://github.com/you/your-app
# or, for a repo you already have checked out locally:
./scripts/onboard.sh ../your-app

# equivalently, via make (source checkout only):
make onboard SOURCE=https://github.com/you/your-app
make onboard SOURCE=../your-app NAME="Your App"
```

## What it does

1. Creates `.env` (with a generated `POSTGRES_PASSWORD`) if you don't
   have one yet.
2. Brings up the Docker Compose stack if it isn't already reachable at
   `http://localhost:8080` — `make up` in a source checkout, or
   `docker compose -f docker-compose.release.yml up -d` in a release
   install.
3. Gets your repository into `./workspace`, since that's the directory
   `sentinel-api` can actually read inside its container:
   - a **git URL** is cloned with `git clone --depth 1` (re-running the
     script later `git pull`s instead of re-cloning);
   - a **local path** is `cp -R`'d in (not symlinked — a symlink
     pointing outside `./workspace` doesn't resolve inside the
     container, since only `./workspace` itself is bind-mounted; a
     symlink *inside* `./workspace` that points further outside it
     hits the same wall).
4. Registers the project via `POST /api/v1/projects` and runs discovery
   — or reuses the existing project if you've already onboarded this
   same path before, so running the script again to pick up new commits
   never creates a duplicate.
5. Prints the exact panel URL to open next.

## After it finishes

The script gets you to "discovered", not "running tests" — the rest
still needs your judgment:

1. Open the printed panel URL.
2. **Environments** page: set that project's `base_url` — see the
   script's own next-steps output for the `host.docker.internal` note
   if your app runs locally in another container
   ([docs/RUNNER_ISOLATION.md](RUNNER_ISOLATION.md) has the full
   explanation of why a Compose service name doesn't work here).
3. **Test Inventory** page: generate a plan, review what it suggests.
4. **Approvals** page: approve what you actually want to run.

See [README.md](../README.md) for the full step-by-step walkthrough and
the pipeline diagram.

## Options

- `NAME` (second argument / `make onboard NAME=...`): the project's
  display name in the panel. Defaults to the repo's name. The actual
  directory created under `./workspace` is always a lowercase,
  hyphenated slug of this name — never containing a space — regardless
  of what you pass, since a space in that path can fail to resolve from
  inside the container on some Docker Desktop setups (confirmed on
  macOS/virtiofs) even though the identical path resolves fine on the
  host directly.
- `SENTINEL_URL` (env var): defaults to `http://localhost:8080`. Set
  this if the API is reachable somewhere other than the default.
- `SENTINEL_API_TOKEN` (env var): only needed if you've turned on RBAC
  (`SENTINEL_AUTH_ENABLED=true`) — sent as a bearer token on every
  request the script makes.
