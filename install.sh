#!/usr/bin/env bash
# One-command installer for E2E Sentinel — no repository checkout, no
# Go/Node toolchain. Downloads a release compose file, .env.example, and
# the project-onboarding script, then pulls pre-built images and starts
# the stack. See docs/QUICKSTART.md ("Running a release").
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/Tolgahan0/E2E-Sentinel/main/install.sh | bash
#
# Environment variables:
#   SENTINEL_VERSION      Release tag to install (default: the latest
#                         published GitHub release, falling back to
#                         "main" if none exist yet)
#   SENTINEL_INSTALL_DIR  Directory to install into (default: ./e2e-sentinel)
set -euo pipefail

REPO="Tolgahan0/E2E-Sentinel"
RAW_BASE="https://raw.githubusercontent.com/${REPO}"
INSTALL_DIR="${SENTINEL_INSTALL_DIR:-./e2e-sentinel}"

if ! command -v docker >/dev/null 2>&1; then
  echo "!! Docker is required — install it from https://docs.docker.com/get-docker/ and re-run." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "!! 'docker compose' (v2, the 'docker compose' plugin, not standalone docker-compose) is required." >&2
  exit 1
fi

REF="${SENTINEL_VERSION:-}"
if [ -z "$REF" ]; then
  REF="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" 2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('tag_name',''))" 2>/dev/null || true)"
  REF="${REF:-main}"
fi
echo "==> Installing E2E Sentinel (${REF}) into ${INSTALL_DIR}"

mkdir -p "$INSTALL_DIR/scripts"
cd "$INSTALL_DIR"

fetch() {
  local path="$1"
  echo "==> Fetching ${path}"
  curl -fsSL "${RAW_BASE}/${REF}/${path}" -o "$path"
}

fetch "docker-compose.release.yml"
fetch ".env.example"
fetch "scripts/onboard.sh"
chmod +x scripts/onboard.sh

if [ ! -f .env ]; then
  echo "==> No .env found — creating one from .env.example"
  cp .env.example .env
  if command -v openssl >/dev/null 2>&1; then
    GENERATED_PASSWORD="$(openssl rand -hex 16)"
    sed -i.bak "s/^POSTGRES_PASSWORD=.*/POSTGRES_PASSWORD=${GENERATED_PASSWORD}/" .env
    rm -f .env.bak
    echo "==> Generated a POSTGRES_PASSWORD in .env"
  else
    echo "!! openssl not found — set POSTGRES_PASSWORD in .env yourself, then re-run." >&2
    exit 1
  fi
fi

mkdir -p workspace runner-workspaces
export SENTINEL_VERSION="$REF"
export SENTINEL_RUNNER_HOST_WORKSPACE_DIR="$(pwd)/runner-workspaces"

echo "==> Pulling images (${REF}) — no local build, this is the whole point"
docker compose -f docker-compose.release.yml pull

echo "==> Starting the stack"
docker compose -f docker-compose.release.yml up -d

echo "==> Waiting for sentinel-api to become healthy..."
ready=false
for _ in $(seq 1 60); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    ready=true
    break
  fi
  sleep 2
done
if [ "$ready" != true ]; then
  echo "!! sentinel-api did not become healthy in time — check 'docker compose -f docker-compose.release.yml logs sentinel-api'" >&2
  exit 1
fi

cat <<EOF

✓ E2E Sentinel is running.

  Panel: http://localhost:9090

Add your own project:
  cd ${INSTALL_DIR}
  ./scripts/onboard.sh https://github.com/you/your-app

Stop the stack:
  cd ${INSTALL_DIR} && docker compose -f docker-compose.release.yml down
EOF
