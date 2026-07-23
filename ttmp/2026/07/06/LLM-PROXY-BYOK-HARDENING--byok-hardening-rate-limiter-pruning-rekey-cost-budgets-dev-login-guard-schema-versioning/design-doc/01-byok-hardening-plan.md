---
Title: BYOK hardening plan
Ticket: LLM-PROXY-BYOK-HARDENING
Status: active
Topics:
    - byok
    - llm-proxy
    - security
    - metering
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/authmw/ratelimit.go
      Note: Fixed-window rate limiter — needs window pruning
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/vault/vault.go
      Note: AES-256-GCM vault with version byte — rekey target
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/store/sqlite/store.go
      Note: SQLite schema — needs schema_version table
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/web/web.go
      Note: Control plane — /dev-login hardening target
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/server/errors.go
      Note: Unexported OpenAI envelope — export a shared writer
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/authmw/middleware.go
      Note: Duplicates the error envelope; share with pkg/server
ExternalSources: []
Summary: Medium-priority hardening items deferred during the LLM-PROXY-BYOK Phase 0-3 sprint: rate-limiter window pruning, vault rekey command, cost-in-dollars budgets, /dev-login non-loopback guard, schema versioning, shared error-envelope writer, and GetUserByUsername confirmation.
LastUpdated: 2026-07-06T11:30:00-04:00
WhatFor: Track the medium-priority hardening work that is not blocking but should land before real deployment.
WhenToUse: Read when picking up incremental BYOK hardening after the production-readiness gaps are closed.
---

# BYOK hardening plan

## Executive Summary

> **Implementation update (2026-07-22):** The schema-versioning item is complete
> through `LLM-PROXY-BYOK-TINYIDP` Phase 0. `pkg/byok/store/sqlite/schema.go`
> now uses forward-only `PRAGMA user_version` migrations, validates legacy and
> current schema constraints, rejects future/malformed databases, and has
> transactional rollback tests. The remaining items in this ticket stay open.

The LLM-PROXY-BYOK implementation (Phases 0–3) is functionally complete and tested, but a set of medium-priority hardening items were explicitly deferred and recorded as "what should be done in the future" in the diary. None are blocking (they are documented properties or low-risk growth concerns), but they should land before real deployment. This ticket collects them into one scoped batch.

## Problem Statement

Each item below is a documented limitation from the LLM-PROXY-BYOK diary. They are grouped by area.

### Rate limiter
- The fixed-window rate limiter (`pkg/byok/authmw/ratelimit.go`) allows up to 2× the configured `rpm` across a window boundary (classic fixed-window artifact).
- Its per-token map is never pruned. This is only a concern at millions of distinct tokens per process, but it is unbounded growth.

### Vault
- There is no `byok rekey` command for master-key rotation. The vault blob version byte (`0x01`) already anticipates rotation, but the command to re-encrypt all secrets under a new master key does not exist.

### Budgets
- Budgets are token-count-only. There is no cost-in-dollars budget, so a caller cannot be capped by spend.

### Dev login
- `/dev-login` is gated only by the `--byok-dev-user` flag and is loudly logged, but there is no guard refusing to start when the flag is combined with a non-loopback `--listen`. A misconfigured production listener could accidentally expose the passwordless dev login.

### Schema versioning
- There is no `schema_version` pragma/table. Migrations are not possible yet, which blocks any future schema change to the six BYOK tables.

### Error envelope duplication
- `writeAPIError` in `pkg/byok/authmw` duplicates the OpenAI error envelope of `pkg/server` (which is unexported). If the envelope shape ever changes, both places must change in lockstep.

### Username lookup
- `GetUserByUsername` picks the oldest match (`ORDER BY created_at LIMIT 1`) because usernames are not unique in the schema (OIDC subject is the identity). This is fine for CLI convenience but was flagged for confirmation.

## Proposed Solution

### 1. Rate-limiter pruning + window comment
- Add a periodic sweep (or a lazy prune on access) that evicts stale per-token rate-limit windows older than the current window.
- Add a code comment documenting the fixed-window 2× boundary artifact as an accepted v1 property (or switch to a sliding-window log if exactness is later required).
- Add a test asserting pruning bounds the map size under synthetic token cardinality.

### 2. `byok rekey` command
- Add a Glazed `byok rekey` command that: reads the old master key, decrypts every credential, re-encrypts under a new master key, and writes the new ciphertexts in a single transaction.
- The blob version byte already supports versioned ciphertexts, so rekey can be done in place.
- Add a test asserting all secrets decrypt under the new key and fail under the old.

### 3. Cost-in-dollars budgets
- Add an optional `max_cost_usd` budget column to `tokens` (nullable, like the existing token/request budgets).
- Maintain a denormalized `total_cost_usd` in `token_counters`, updated in the same transaction as the ledger insert.
- Cost requires a price table (per-model per-million-token rates). Decide where the price table lives: a config file, a DB table, or profile metadata. Lean: a config file loaded at startup, with per-model overrides.
- Enforce the cost budget in the same pre-check as token/request budgets.

### 4. `/dev-login` non-loopback guard
- In `main.go`, when `--byok-dev-user` is set, refuse to start if `--listen` resolves to a non-loopback address. Log a loud error pointing at the flag.
- Add a test asserting the guard fires for a non-loopback listener.

### 5. Schema versioning
- Add a `schema_version` table (or `PRAGMA user_version`) to the SQLite schema, set at `ensureSchema` time.
- Add a migration runner that checks the current version and applies forward-only migrations.
- Document the migration contract (versioned, idempotent, tested against both backends where applicable).

### 6. Shared error-envelope writer
- Export the OpenAI error envelope writer from `pkg/server` (or a small shared `pkg/oaishape` package) and have `pkg/byok/authmw` call it instead of duplicating the envelope.
- Keep the structural `HTTPStatus() int` interface in `pkg/server/errors.go` unchanged.

### 7. `GetUserByUsername` confirmation
- Confirm the oldest-match behavior is acceptable for CLI convenience, or add a uniqueness constraint + a `UNIQUE` index on a normalized username if CLI ergonomics require it.
- Document the decision in code.

## Design Decisions

- **Batch these into one ticket** because they are independent, low-risk, and each is too small to justify its own ticket. They share a review surface (the byok packages).
- **Cost budget is the largest item** and may warrant its own sub-spike for the price-table design. If it grows, split it into a follow-on ticket.
- **Rekey is in-place** because the version byte supports it; no dual-write migration window is needed.

## Alternatives Considered

- **Sliding-window rate limiter** instead of pruning the fixed window. Rejected for now: the 2× boundary artifact is acceptable for v1 and documented; pruning addresses the unbounded-growth concern without changing the algorithm.
- **Session store for dev-login guard.** Not needed here; the loopback check is sufficient for the dev-login case. (Server-side session invalidation is tracked in LLM-PROXY-BYOK-PROD-READINESS.)

## Implementation Plan

1. Rate-limiter pruning + comment + test.
2. `byok rekey` command + test.
3. Schema versioning (prerequisite for any future schema change; do early).
4. Shared error-envelope writer export.
5. `/dev-login` non-loopback guard + test.
6. `GetUserByUsername` decision + documentation.
7. Cost-in-dollars budgets (largest; may split into a follow-on if the price-table design needs a spike).

## Open Questions

- Where should the cost price table live? (Config file vs DB table vs profile metadata.)
- Should `rekey` support a dry-run mode that reports how many secrets would be re-encrypted without writing?
- Is the fixed-window 2× artifact acceptable long-term, or should we plan a sliding-window migration?

## References

- LLM-PROXY-BYOK diary Steps 1–5 ("What should be done in the future" sections)
- PROJ note "Near-term next steps" (rekey, rate-limiter pruning, cost budgets)
- Related ticket: LLM-PROXY-BYOK-PROD-READINESS (session invalidation, meter circuit breaker)
