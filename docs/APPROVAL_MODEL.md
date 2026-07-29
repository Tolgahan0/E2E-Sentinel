# Approval Model

Approvals must be action-specific, time-bounded (where meaningful),
audited, and visible — a general approval must never silently authorize
an unrelated future action (spec §2.3). Rather than a single generic
`Approval` entity mediating every mutating action, E2E Sentinel gates
each action at its own resource, with its own status field and its own
explicit endpoints — the pattern established in Phase 4 and reused as-is
in Phase 8. This keeps each gate simple to audit (one resource, one
status, one set of transitions) instead of introducing an indirection
layer every future phase would need to integrate with.

## Test case approval (Phase 4)

`test_cases.approval_status`: `pending` → `approved` | `rejected`.

`POST /tests/{id}/approve` is refused (403) for a mutating test case
(`is_mutating`) while any of the project's environments is classified
`production` or `unknown` (spec §2.6) — enforced server-side, not just
hidden in the UI, and covered by both a unit test and a live-API
integration test.

## Fix proposal approval (Phase 8)

`fix_proposals.approval_status`: `pending_review` → `approved` |
`rejected` | `revision_requested` (a rejected or revision-requested
proposal isn't reopened automatically — generate a new one).

```text
POST /bugs/{id}/fix-proposal        -> pending_review
POST /fix-proposals/{id}/approve    -> approved
POST /fix-proposals/{id}/reject     -> rejected
POST /fix-proposals/{id}/request-revision -> revision_requested
POST /fix-proposals/{id}/apply-repository -> requires approved; 403 otherwise
```

The AI itself never approves or applies anything — `internal/providers.
Completer` only returns text; a proposal is `pending_review` regardless
of whether it was AI-generated or manually authored, and only a
separate, explicit `POST /fix-proposals/{id}/approve` call changes that
(spec §3.3 "It must not apply patches", §15.2 acceptance: "Final
repository write requires explicit approval"). See
[docs/FIX_PROPOSALS.md](FIX_PROPOSALS.md) for the full workflow,
including the temporary-workspace step and the path-traversal
protections applied before every write.

## Every gate is audited

Both approval flows record an audit event at every transition
(`test.approved`/`test.rejected`, `fix_proposal.approved`/`.rejected`/
`.revision_requested`/`.applied_workspace`/`.applied_repository`) via
the same append-only `internal/audit` recorder used since Phase 0 — see
`GET /api/v1/audit-events`.

## Not yet implemented

A unified cross-resource "Approvals" inbox (spec's generic `Approval`
entity, §6.10, and the corresponding `GET/POST /approvals` endpoints) is
not built — each gate is reviewed on its own page (Test Inventory, Bugs,
Fix Proposals) instead. RBAC/role enforcement of *who* may approve
(spec §22's Tester/Developer/Approver/Administrator roles) lands in
Phase 9; today any caller of the API can approve anything, which is why
this deployment must not be exposed beyond a trusted local network until
then (see [docs/SECURITY_MODEL.md](SECURITY_MODEL.md)).
