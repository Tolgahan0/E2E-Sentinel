# AI Provider Guide

E2E Sentinel's AI integration is entirely optional (spec §16.6 "No-AI
Mode"). Every feature through Phase 6 — discovery, the application graph,
test planning, test execution, artifact capture — works with zero AI
providers configured. This guide covers configuring one anyway, ahead of
later phases (7+) that start using it for failure analysis and fix
proposals.

## Supported providers

| Type | `type` value | Needs an API key? |
|---|---|---|
| Ollama (local) | `ollama` | No (unless your instance requires one) |
| OpenAI | `openai` | Yes |
| Anthropic | `anthropic` | Yes |
| Google Gemini | `gemini` | Yes |
| Azure OpenAI | `azure_openai` | Yes |
| OpenAI-compatible endpoint | `openai_compatible` | Usually |

## Configuring a provider

Via the web panel: **AI Providers** page — fill in type, display name,
base URL, model, and API key (if the provider needs one), then **Add
provider**.

Via the API:

```bash
curl -X POST http://localhost:8080/api/v1/providers \
  -H 'Content-Type: application/json' \
  -d '{
    "type": "ollama",
    "name": "Local Ollama",
    "base_url": "http://host.docker.internal:11434",
    "model": "llama3",
    "is_local": true
  }'
```

`host.docker.internal:11434` is the conventional local Ollama endpoint
(spec §16.2) — reachable from inside `sentinel-api`'s container on Docker
Desktop without any extra network configuration. E2E Sentinel never
scans arbitrary networks looking for a provider; this is the one
conventional address it's aware of, and only as a suggested default, not
something probed automatically at startup.

An external provider (e.g. OpenAI) additionally needs an API key:

```bash
curl -X POST http://localhost:8080/api/v1/providers \
  -H 'Content-Type: application/json' \
  -d '{"type": "openai", "name": "OpenAI", "model": "gpt-4o", "api_key": "sk-..."}'
```

## Keys are encrypted at rest and never returned

Every `GET`/`POST`/`PATCH /providers*` response includes `has_api_key:
true|false` — never the key itself, and never the internal
`secret_reference_id` it maps to. Storing a key requires
`SENTINEL_SECRET_ENCRYPTION_KEY` to be set (see below); without it,
creating or patching a provider with an `api_key` returns `503
secret_encryption_not_configured`. A keyless provider (a local Ollama
instance with no auth) works regardless.

To rotate a key: `PATCH /providers/{id}` with a new `api_key`. To remove
one: `PATCH /providers/{id}` with `"clear_api_key": true`.

### `SENTINEL_SECRET_ENCRYPTION_KEY`

Generate one with:

```bash
openssl rand -base64 32
```

Set it in `.env` before running `make up`. It decrypts AES-256-GCM
ciphertext stored in the `secret_references` table
(`migrations/0007_ai_providers.sql`); losing it makes every previously
stored key permanently unrecoverable — back it up like any other secret,
separately from the database itself (a database backup alone is useless
without this key, and vice versa).

Key management by deployment:

- **Local development**: put it in `.env` (gitignored).
- **Docker Compose**: pass it as an environment variable to
  `sentinel-api` (already wired via `docker-compose.yml`); avoid baking
  it into the image.
- **Kubernetes** (Phase 10): a Secret, mounted as an environment variable
  — never a ConfigMap.

## Testing a connection

`POST /providers/{id}/test` performs a live, lightweight reachability
check against the provider's own "list models"-style endpoint — the
smallest request that proves both connectivity and (if a key is stored)
that the key is accepted:

| Type | Endpoint probed |
|---|---|
| `ollama` | `GET {base_url}/api/tags` |
| `openai` / `openai_compatible` | `GET {base_url}/models` (Bearer token) |
| `anthropic` | `GET {base_url}/v1/models` (`x-api-key` header) |
| `azure_openai` | `GET {base_url}/openai/models?api-version=2024-02-01` (`api-key` header) |
| `gemini` | `GET {base_url}/v1beta/models?key=...` |

No repository content, test content, or failure data is ever sent by a
connection test — only the credential-bearing request above. The result
(`ok` or `error`, plus a message that never contains the key) is recorded
on the provider as `health_status`/`last_checked_at`, visible in both the
API response and the AI Providers page.

## Task routing

Once later phases add real AI consumers, each task type can be routed to
a different provider (spec §16.4):

- `architecture_analysis`
- `test_planning`
- `test_generation`
- `failure_analysis`
- `fix_generation`
- `report_summarization`

```bash
curl -X PATCH http://localhost:8080/api/v1/providers/routing \
  -H 'Content-Type: application/json' \
  -d '{"routes": {"failure_analysis": "<provider-id>"}}'
```

Setting a route to an empty string clears it. An unrouted task simply
has no AI assistance available for it — there is no default provider a
task silently falls back to.

## The redaction pipeline

Before any content reaches an AI provider (once Phase 7+ starts sending
context), `internal/redaction` runs over it first (spec §16.5):

1. Detects and redacts secrets (API keys, AWS access keys, PEM private
   key blocks).
2. Detects and redacts tokens (JWTs, bearer tokens).
3. Detects and redacts credentials (password assignments, `user:pass@`
   embedded in URLs).
4. Redacts `Authorization` header lines outright.
5. Redacts `Cookie`/`Set-Cookie` header lines outright.
6. (Personal data redaction is not yet implemented — tracked for a later
   phase alongside real AI context assembly.)
7. A path allowlist (`redaction.PathAllowed`) restricts which repository
   files can ever be included.
8. A file-size limit (`redaction.WithinSizeLimit`, 200 KB by default)
   bounds any single file's contribution.
9. `Redact` returns which categories were found (`secret`, `token`,
   `credential`, `authorization_header`, `cookie`) — never the matched
   values themselves — so a caller can audit what was redacted without
   the audit log ever holding a fragment of the secret.

This is a self-contained package with its own test suite (96.7%
coverage) — it can be exercised and verified independent of any AI
provider actually being configured.

## No-AI mode

E2E Sentinel remains fully useful with zero providers configured:
deterministic discovery, manual test creation, execution, artifacts, and
(once Phase 7 lands) manual bug reports all work independent of AI.
Phases 0 through 5 already operated with zero AI dependency; Phase 6
adds provider configuration without introducing an AI dependency
anywhere else — no phase before 7 calls a provider at all.
