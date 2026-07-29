# Quickstart: integrate a project in one command

No AI coding assistant required — this is a plain shell script anyone
can run.

```bash
git clone <this-repo>
cd e2e-sentinel

./scripts/onboard.sh https://github.com/you/your-app
# or, for a repo you already have checked out locally:
./scripts/onboard.sh ../your-app

# equivalently, via make:
make onboard SOURCE=https://github.com/you/your-app
make onboard SOURCE=../your-app NAME="Your App"
```

## What it does

1. Creates `.env` (with a generated `POSTGRES_PASSWORD`) if you don't
   have one yet.
2. Brings up the Docker Compose stack with `make up`, if it isn't
   already reachable at `http://localhost:8080`.
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
