---
Title: BYOK production readiness plan
Ticket: LLM-PROXY-BYOK-PROD-READINESS
Status: active
Topics:
    - byok
    - llm-proxy
    - auth
    - security
    - metering
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/web/oidc.go
      Note: OIDC RP — ported from byok-host, unit-tested, NOT yet driven against live Keycloak in this repo
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/web/session.go
      Note: HMAC session cookie — no server-side invalidation / session store
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/meter/meter.go
      Note: UsageRecorder — swallows store errors (logs only), no circuit breaker
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/deploy/docker-compose.yaml
      Note: Keycloak dev compose — never run end-to-end against the ported OIDC code
    - Path: /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/web/web.go
      Note: Control plane assembly — session lifecycle wiring point
ExternalSources: []
Summary: High-priority production gaps from the LLM-PROXY-BYOK sprint: (1) the OIDC path is ported and unit-tested but never driven against live Keycloak in this repo; (2) no server-side session invalidation exists; (3) meter.Recorder swallows store errors with no circuit breaker. These are the blockers between "working BYOK" and "deployable BYOK".
LastUpdated: 2026-07-06T11:30:00-04:00
WhatFor: Track the high-priority production-readiness gaps that must close before real deployment.
WhenToUse: Read when preparing BYOK for a real deployment or a real IdP integration.
---

# BYOK production readiness plan

## Executive Summary

The LLM-PROXY-BYOK implementation (Phases 0–3) is functionally complete and tested, but three high-priority gaps stand between "working BYOK" and "deployable BYOK." Each is a documented open question from the diary or the PROJ note. This ticket tracks closing them.

1. **Live Keycloak OIDC end-to-end.** The OIDC relying-party code (`pkg/byok/web/oidc.go`) is ported from the byok-host Keycloak demo (which *was* validated end-to-end against Keycloak) and is unit-tested, but it has never been driven against a live Keycloak in this repo. The `deploy/docker-compose.yaml` + realm import exist but are unverified against the ported code.
2. **Server-side session invalidation.** Session cookies are HMAC-signed JSON payloads with no server-side store. Revoking a compromised browser session requires rotating `--byok-session-secret`, which invalidates *all* sessions. There is no way to revoke a single session.
3. **Meter circuit breaker.** `meter.Recorder` swallows store errors (logs only). If ledger writes fail persistently, budgets stop advancing silently — a caller could spend unbounded while the ledger is broken. There is no 503 circuit breaker on the data plane.

## Problem Statement

### 1. OIDC unverified against live Keycloak
The byok-host `BYOK-KEYCLOAK-STORAGE` ticket proved the Keycloak + OIDC + SQLite pattern end-to-end (diary Steps 4–7: login, consent, PKCE, persistence across restart). The llm-proxy `pkg/byok/web/oidc.go` was ported from that working code and is covered by unit tests (`web_test.go`), but the port has never been exercised against a real Keycloak instance in this repo. The `deploy/docker-compose.yaml` (Keycloak 26.2, client `llm-proxy-web`, alice/password123) and `deploy/keycloak/realm-byok.json` exist but are unverified. A porting bug (e.g. redirect URI, client ID, nonce handling) would only surface under a live IdP.

### 2. No server-side session invalidation
The session cookie is `base64url(payload).base64url(sig)` signed with HMAC-SHA256, verified with `hmac.Equal`, SameSite=Lax, HttpOnly. There is no session store: the cookie is self-contained and stateless. Consequences:
- A compromised browser session cannot be revoked individually; the only lever is rotating `--byok-session-secret`, which invalidates every active session.
- There is no session list, no "log out everywhere," no idle timeout enforcement server-side.

This is a documented property (diary Step 5, "What warrants a second pair of eyes"), but it is a real production gap.

### 3. Meter swallows store errors
`meter.Recorder.RecordInference` writes the ledger row + counter update via `context.WithoutCancel(ctx)` (so client disconnects still meter). If the store write fails, the error is logged and swallowed — the request still succeeds. Under persistent storage failure (disk full, DB locked, schema drift), budgets stop advancing: the counter pre-check reads stale or zero values, and a caller can spend unbounded while the ledger is broken. There is no circuit breaker that would 503 the data plane when metering is unhealthy.

## Proposed Solution

### 1. Live Keycloak OIDC end-to-end smoke
- Stand up `deploy/docker-compose.yaml` (Keycloak 26.2 with the auto-imported realm).
- Drive the full browser flow against the ported `pkg/byok/web/oidc.go`: dev-login disabled, OIDC login as alice, callback, session set, credential CRUD, token mint, token used against `/v1/*`.
- Verify the OIDC callback check order (state cookie → code exchange → ID-token signature → nonce) holds against a real IdP.
- Capture the exact commands in a playbook (`playbooks/01-live-keycloak-oidc-smoke.md`).
- Fix any porting bugs discovered (likely candidates: redirect URI exactness, `--byok-public-url` interaction with the callback, client secret wiring).

### 2. Server-side session store
- Add a `sessions` table to the SQLite store (session id, user id, created_at, last_seen_at, expires_at, revoked bool). Keep the HMAC cookie as the transport, but make it carry a session id that is looked up server-side.
- On every authenticated request, look up the session; reject if revoked or expired.
- Add a `POST /api/sessions/{id}/revoke` (or extend the existing logout) so a user can revoke individual sessions; add a session list endpoint.
- This makes `--byok-session-secret` rotation a defense-in-depth measure rather than the only revocation lever.
- Decide on idle vs absolute timeout policy.

### 3. Meter circuit breaker
- Track consecutive ledger-write failures in `meter.Recorder` (or a small health struct).
- After a configurable threshold of consecutive failures (e.g. `--byok-meter-failure-threshold`, default e.g. 10), trip a circuit breaker that makes the data plane return 503 for new requests until metering recovers.
- On a successful write, reset the failure counter and close the circuit.
- Document the tradeoff: a 503 data plane is safer (no unbounded spend) but less available. Make the threshold configurable and default to a value that tolerates transient SQLite contention (`SQLITE_BUSY`) without tripping.

## Design Decisions

- **Session store is SQLite, not Redis.** Keeps the single-binary/single-DB deploy story. SQLite handles session lookups fine on the hot path (indexed by session id).
- **Circuit breaker is opt-in via threshold, not on by default at threshold 0.** A threshold of 0 would trip on the first transient `SQLITE_BUSY`, which is too aggressive. Default to a small number that tolerates transient contention.
- **OIDC smoke is a playbook, not just a test.** A live Keycloak is not CI-friendly (Docker dependency); the unit tests cover the logic, and the playbook covers the live integration. Consider a CI job that runs the compose stack if Docker-in-CI is acceptable later.

## Alternatives Considered

- **Keep stateless sessions; rely on short cookie TTL.** Rejected: short TTL hurts UX and still cannot revoke a specific compromised session before expiry.
- **Fail the request when the ledger write fails (instead of a circuit breaker).** Rejected: a single transient `SQLITE_BUSY` would fail an otherwise-successful inference, and the upstream spend already happened. The circuit breaker thresholds persistent failure rather than failing per-request.
- **External session store (Redis).** Rejected for now: adds an operational dependency. Revisit if session cardinality or multi-instance deployment makes SQLite a bottleneck.

## Implementation Plan

1. **OIDC live smoke (do first — may surface porting bugs that affect the other two).** Stand up compose, drive the flow, write the playbook, fix bugs.
2. **Session store.** Add the `sessions` table + store methods; wire the cookie to carry a session id; add revocation/list endpoints; update tests.
3. **Meter circuit breaker.** Add failure tracking + threshold + 503 trip; add tests simulating persistent store failure.

## Open Questions

- Should the session store also back the `--byok-dev-user` dev-login path, or is that path intentionally stateless?
- What is the right default meter-failure threshold, and should it distinguish `SQLITE_BUSY` (transient) from disk-full (persistent)?
- Should the OIDC smoke be automated in CI with a Docker service container, or kept as a manual playbook?

## References

- LLM-PROXY-BYOK diary Step 5 (OIDC ported but unverified; session invalidation gap; meter swallows errors)
- PROJ note open questions (session invalidation, meter circuit breaker)
- byok-host BYOK-KEYCLOAK-STORAGE diary Steps 4–7 (the proven Keycloak pattern this code was ported from)
- Related ticket: LLM-PROXY-BYOK-HARDENING (medium-priority cleanup)
- Related ticket: LLM-PROXY-BYOK-GEPETTO (include_usage — complementary to metering observability)
