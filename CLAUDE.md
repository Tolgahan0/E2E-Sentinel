# Claude Code entry point

Full orientation for this repository — what it is, where everything
lives, the conventions that must be followed, and how to verify a
change before calling it done — lives in [`AGENTS.md`](AGENTS.md) at
the repo root. Read it first; it is written for exactly this purpose
(an AI coding agent picking up this codebase cold) and is kept
tool-agnostic on purpose so it also applies outside Claude Code.

The one Claude-Code-specific note: this project was built end-to-end
by Claude Code across all 11 spec phases, following the phase-gated
discipline `AGENTS.md` describes (implement → unit test → live-verify →
document → commit). Continue in that same discipline rather than
reverting to a lighter one.
