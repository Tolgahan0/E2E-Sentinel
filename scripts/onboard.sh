#!/usr/bin/env bash
# Integrate an external repository with E2E Sentinel in one command — no
# AI coding assistant required. See docs/QUICKSTART.md.
#
# Usage:
#   ./scripts/onboard.sh <git-url-or-local-path> [project-name]
#
# Examples:
#   ./scripts/onboard.sh https://github.com/acme/checkout-service
#   ./scripts/onboard.sh ../my-app "My App"
#
# What it does:
#   1. Creates .env (with a generated POSTGRES_PASSWORD) if missing.
#   2. Brings up the Docker Compose stack if it isn't already reachable.
#   3. Clones (git URL) or symlinks (local path) the target repo under
#      ./workspace, since that's the directory sentinel-api can read
#      inside its container (see docs/LOCAL_DEVELOPMENT.md).
#   4. Registers the project and runs discovery via the API.
#   5. Prints the panel URL to open next.
#
# Environment variables:
#   SENTINEL_URL       Base URL of the running API (default http://localhost:8080)
#   SENTINEL_API_TOKEN Bearer token to send, only needed if RBAC
#                      (SENTINEL_AUTH_ENABLED) is turned on
set -euo pipefail

SOURCE="${1:?Usage: $0 <git-url-or-local-path> [project-name]}"
NAME="${2:-}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

SENTINEL_URL="${SENTINEL_URL:-http://localhost:8080}"
WORKSPACE_DIR="$REPO_ROOT/workspace"

api_curl() {
  # Shared curl flags for every API call this script makes: fail on
  # non-2xx (-f), silent progress (-s), the CSRF header every mutating
  # route requires once RBAC is on (harmless no-op otherwise), and an
  # optional bearer token if the caller set one.
  local args=(-sf -H "X-Sentinel-Csrf: 1")
  if [ -n "${SENTINEL_API_TOKEN:-}" ]; then
    args+=(-H "Authorization: Bearer ${SENTINEL_API_TOKEN}")
  fi
  curl "${args[@]}" "$@"
}

echo "==> E2E Sentinel onboarding: ${SOURCE}"

# --- 1. .env -----------------------------------------------------------
if [ ! -f .env ]; then
  echo "==> No .env found — creating one from .env.example"
  cp .env.example .env
  if command -v openssl >/dev/null 2>&1; then
    GENERATED_PASSWORD="$(openssl rand -hex 16)"
    # BSD sed (macOS) and GNU sed (Linux) both accept -i with an explicit
    # (possibly empty) backup suffix argument, unlike a bare -i.
    sed -i.bak "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${GENERATED_PASSWORD}/" .env
    rm -f .env.bak
    echo "==> Generated a POSTGRES_PASSWORD in .env"
  else
    echo "!! openssl not found — set POSTGRES_PASSWORD in .env yourself, then re-run this script"
    exit 1
  fi
fi

# --- 2. bring the stack up if it isn't reachable ------------------------
if ! curl -sf "${SENTINEL_URL}/health" >/dev/null 2>&1; then
  echo "==> Stack not reachable at ${SENTINEL_URL} — running 'make up' (the first run can take a few minutes)"
  make -C "$REPO_ROOT" up
  echo "==> Waiting for sentinel-api to become healthy..."
  ready=false
  for _ in $(seq 1 60); do
    if curl -sf "${SENTINEL_URL}/health" >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 2
  done
  if [ "$ready" != true ]; then
    echo "!! sentinel-api did not become healthy in time — check 'docker compose logs sentinel-api'"
    exit 1
  fi
fi

# --- 3. materialize the target repo under ./workspace -------------------
mkdir -p "$WORKSPACE_DIR"

if [ -e "$SOURCE" ]; then
  IS_GIT_URL=false
elif [[ "$SOURCE" =~ ^(https?://|git@|ssh://) ]]; then
  IS_GIT_URL=true
else
  echo "!! '${SOURCE}' is neither an existing local path nor something that looks like a git URL (https://, git@, ssh://)"
  exit 1
fi

# slugify turns a display name into a filesystem-safe directory name
# (lowercase, spaces/anything-non-alnum collapsed to a single hyphen).
# The *directory* under ./workspace must never contain a space: a Docker
# Desktop bind mount (virtiofs) can fail to resolve a symlink-eval'd path
# with a space in it from inside the container ("readlink: invalid
# argument") even though the exact same path resolves fine natively on
# the host — confirmed live while building this script. The project's
# human-readable *name* (sent to the API, shown in the panel) is
# unaffected and keeps spaces/punctuation as given.
slugify() {
  echo "$1" | tr '[:upper:]' '[:lower:]' | sed -E 's/[^a-z0-9]+/-/g; s/^-+|-+$//g'
}

if [ "$IS_GIT_URL" = true ]; then
  DEFAULT_NAME="$(basename "${SOURCE%.git}")"
  PROJECT_NAME="${NAME:-$DEFAULT_NAME}"
  DIR_NAME="$(slugify "$PROJECT_NAME")"
  DEST="${WORKSPACE_DIR}/${DIR_NAME}"
  if [ -d "$DEST" ]; then
    echo "==> ${DEST} already exists — pulling latest instead of re-cloning"
    git -C "$DEST" pull --ff-only
  else
    echo "==> Cloning ${SOURCE} into ${DEST}"
    git clone --depth 1 "$SOURCE" "$DEST"
  fi
else
  ABS_SOURCE="$(cd "$SOURCE" && pwd)"
  DEFAULT_NAME="$(basename "$ABS_SOURCE")"
  PROJECT_NAME="${NAME:-$DEFAULT_NAME}"
  DIR_NAME="$(slugify "$PROJECT_NAME")"
  DEST="${WORKSPACE_DIR}/${DIR_NAME}"
  if [ -e "$DEST" ]; then
    echo "==> ${DEST} already exists — leaving it as-is"
  else
    # A symlink to a path outside ./workspace would NOT resolve inside
    # sentinel-api's container: only ./workspace itself is bind-mounted,
    # so the container's filesystem has no route to an arbitrary host
    # path a symlink might point at. A real copy is the only thing that
    # actually works here.
    echo "==> Copying ${ABS_SOURCE} -> ${DEST}"
    cp -R "$ABS_SOURCE" "$DEST"
  fi
fi

CONTAINER_PATH="/workspace/${DIR_NAME}"

# --- 4. register (or reuse) + discover via the API -----------------------
# POST /projects always creates a new row — running this script again for
# the same repository (e.g. to pull the latest commit and re-discover)
# would otherwise pile up duplicate projects. Look for one already
# pointed at this exact container path first and reuse it instead.
EXISTING_ID="$(api_curl "${SENTINEL_URL}/api/v1/projects" | python3 -c "
import json, sys
target = sys.argv[1]
data = json.load(sys.stdin)
for p in data.get('projects') or []:
    if p.get('repository_path') == target:
        print(p['id'])
        break
" "$CONTAINER_PATH")"

if [ -n "$EXISTING_ID" ]; then
  echo "==> Project already registered at ${CONTAINER_PATH} — reusing it"
  PROJECT_ID="$EXISTING_ID"
else
  echo "==> Registering project '${PROJECT_NAME}' (${CONTAINER_PATH})"
  CREATE_BODY="$(python3 -c "import json,sys; print(json.dumps({'name': sys.argv[1], 'repository_path': sys.argv[2]}))" "$PROJECT_NAME" "$CONTAINER_PATH")"
  CREATE_RESPONSE="$(api_curl -X POST "${SENTINEL_URL}/api/v1/projects" -H 'Content-Type: application/json' -d "$CREATE_BODY")"
  PROJECT_ID="$(python3 -c "import json,sys; print(json.load(sys.stdin).get('id',''))" <<<"$CREATE_RESPONSE")"

  if [ -z "$PROJECT_ID" ]; then
    echo "!! Failed to create the project. Response:"
    echo "$CREATE_RESPONSE"
    exit 1
  fi
fi

echo "==> Running discovery"
api_curl -X POST "${SENTINEL_URL}/api/v1/projects/${PROJECT_ID}/discover" >/dev/null

WEB_URL="${SENTINEL_WEB_URL:-http://localhost:9090}"
cat <<EOF

✓ '${PROJECT_NAME}' is registered and discovered.

  Panel:      ${WEB_URL}/discovery?project=${PROJECT_ID}
  Project ID: ${PROJECT_ID}

Next: set that project's environment base_url (Environments page — if
the target runs locally in another container, use
http://host.docker.internal:<port>, not a Docker Compose service name;
see docs/RUNNER_ISOLATION.md), generate a test plan (Test Inventory
page), and approve what you want to run.
EOF
