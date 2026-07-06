---
Title: Investigation diary
Ticket: LLM-PROXY-BYOK-PROD-READINESS
Status: active
Topics:
    - byok
    - llm-proxy
    - auth
    - security
    - metering
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: Chronological diary for the BYOK production-readiness ticket, starting from the assessment of the LLM-PROXY-BYOK implementation and the three high-priority gaps blocking real deployment.
LastUpdated: 2026-07-06T11:35:00-04:00
WhatFor: Preserve the investigation trail so future sessions can resume the production-readiness work without re-deriving context.
WhenToUse: Read before resuming work on this ticket; append an entry per work session.
---

# Investigation diary

## Goal

Track the high-priority production-readiness gaps: live Keycloak OIDC end-to-end verification, server-side session invalidation, and the meter circuit breaker.

## Step 1: Ticket creation from the BYOK assessment

This ticket was created during a post-implementation assessment of the LLM-PROXY-BYOK work. The assessment reviewed the implementation diaries (Step 5 "What warrants a second pair of eyes" and "What should be done in the future"), the PROJ note open questions, and the byok-host BYOK-KEYCLOAK-STORAGE diary (which proved the Keycloak pattern end-to-end that the llm-proxy OIDC code was ported from). Three high-priority gaps were identified as the blockers between "working BYOK" and "deployable BYOK."

### Prompt Context

**User prompt (verbatim):** "Create another ticket to handle the High issues."

**Assistant interpretation:** Create a docmgr ticket for the high-priority production-readiness gaps: (1) OIDC never driven against live Keycloak in this repo, (2) no server-side session invalidation, (3) meter.Recorder swallows store errors with no circuit breaker.

**Inferred user intent:** Separate the production-blocking gaps from the medium hardening so the deploy blockers are visible and pickable on their own.

### What I did
- Verified the OIDC code is ported and unit-tested but unverified against live Keycloak: `pkg/byok/web/oidc.go` exists, `pkg/byok/web/web_test.go` covers the logic, `deploy/docker-compose.yaml` + `deploy/keycloak/realm-byok.json` exist but were never run against the ported code in this repo.
- Confirmed the byok-host BYOK-KEYCLOAK-STORAGE diary (Steps 4–7) proved the pattern end-to-end (login, consent, PKCE, persistence across restart) — so the port is from working code, but the port itself is unverified.
- Confirmed the session cookie is stateless HMAC (`pkg/byok/web/session.go`) with no server-side store and no individual revocation.
- Confirmed `meter.Recorder` (`pkg/byok/meter/meter.go`) logs and swallows store errors with no circuit breaker.
- Created ticket `LLM-PROXY-BYOK-PROD-READINESS` with design doc and this diary.
- Wrote the design doc with one section per gap, each anchored to the diary step and PROJ note that flagged it, plus a proposed solution and alternatives.

### Why
- These three gaps are the difference between a working prototype and a deployable system. They were explicitly documented as open questions during the sprint; this ticket makes them actionable and sequenced.

### What worked
- The byok-host Keycloak diary gave strong evidence that the OIDC port is from working code, which de-risks the live-smoke task (likely small porting bugs, not architectural issues).

### What didn't work
- N/A. This was a ticket-creation and design-writing step.

### What I learned
- The session-store and meter-circuit-breaker decisions interact: both touch the hot path and both involve a tradeoff between safety and availability. The design doc keeps them as separate tasks but flags the shared "how aggressive should the failure handling be?" question.

### What was tricky to build
- Scoping the meter circuit breaker correctly: failing every request on a single transient `SQLITE_BUSY` would be too aggressive, but never tripping means unbounded spend under persistent failure. The design doc resolves this with a configurable consecutive-failure threshold that tolerates transient contention.

### What warrants a second pair of eyes
- The default meter-failure threshold and whether to distinguish transient (`SQLITE_BUSY`) from persistent (disk-full) failures.
- Whether the session store should also back the `--byok-dev-user` dev-login path or leave it stateless.

### What should be done in the future
- Run the OIDC live smoke first (Step 1 of the design doc plan) — it may surface porting bugs that affect session handling.

### Code review instructions
- Start with the design doc: `design-doc/01-byok-production-readiness-plan.md`.
- Verify the evidence: `pkg/byok/web/oidc.go` (ported), `deploy/docker-compose.yaml` (unrun), `pkg/byok/meter/meter.go` (swallows errors).

### Technical details
- byok-host proven Keycloak pattern: `2026-04-17--byok-host/ttmp/2026/04/17/BYOK-KEYCLOAK-STORAGE--.../reference/01-investigation-diary.md` Steps 4–7.
- llm-proxy OIDC port: `pkg/byok/web/oidc.go`, unit-tested in `pkg/byok/web/web_test.go`.
