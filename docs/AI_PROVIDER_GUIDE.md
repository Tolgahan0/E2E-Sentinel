# AI Provider Guide

**Status: not yet implemented.** Lands in Phase 6. See spec §16.

## Planned support

Ollama, OpenAI, Anthropic, Google Gemini, Azure OpenAI, any
OpenAI-compatible endpoint, and a disabled/no-AI mode. Provider
configuration (base URL, model, API key, timeout, max tokens,
temperature) will live under a dedicated panel page, with per-task
routing (architecture analysis, test planning, test generation, failure
analysis, fix generation, report summarization) and a redaction pipeline
that strips secrets/tokens/cookies/PII before any context reaches a
provider.

## No-AI mode

E2E Sentinel must remain fully useful with AI disabled (deterministic
discovery, manual test creation, execution, artifacts, manual bug
reports). Phase 0 through Phase 5 already operate with zero AI
dependency, which is the baseline this mode preserves going forward.
