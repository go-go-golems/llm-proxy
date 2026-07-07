---
Title: Investigation diary
Ticket: LLM-PROXY-BYOK-HARDENING
Status: active
Topics:
    - byok
    - llm-proxy
    - security
    - metering
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: Chronological diary for the BYOK hardening ticket, starting from the assessment of the LLM-PROXY-BYOK implementation and the medium-priority items deferred during the Phase 0-3 sprint.
LastUpdated: 2026-07-06T11:35:00-04:00
WhatFor: Preserve the investigation trail so future sessions can resume the hardening work without re-deriving context.
WhenToUse: Read before resuming work on this ticket; append an entry per work session.
---

# Investigation diary

## Goal

Track the medium-priority hardening items deferred during the LLM-PROXY-BYOK Phase 0–3 sprint: rate-limiter pruning, vault rekey, cost budgets, dev-login guard, schema versioning, shared error writer, and username-lookup confirmation.

## Step 1: Ticket creation from the BYOK assessment

This ticket was created during a post-implementation assessment of the LLM-PROXY-BYOK work. The assessment reviewed the implementation diaries (Steps 1–5 "What should be done in the future" sections) and the PROJ note's near-term next steps, and collected the medium-priority items into one scoped batch. None are blocking; all should land before real deployment.

### Prompt Context

**User prompt (verbatim):** "Create a ticket to address all the hardening."

**Assistant interpretation:** Create a docmgr ticket collecting the medium-priority hardening items (rate-limiter pruning, rekey, cost budgets, dev-login guard, schema versioning, shared error writer, username confirmation) into one scoped batch.

**Inferred user intent:** Separate the incremental hardening work from the high-priority production-readiness gaps so each can be picked up independently.

### What I did
- Reviewed the LLM-PROXY-BYOK diary "What should be done in the future" sections across Steps 1–5 and the PROJ note near-term next steps.
- Verified the relevant source files exist and match the diary descriptions: `pkg/byok/authmw/ratelimit.go` (no pruning), `pkg/byok/vault/vault.go` (version byte, no rekey), `pkg/byok/store/sqlite/store.go` (no schema_version), `pkg/byok/web/web.go` (dev-login), `pkg/server/errors.go` + `pkg/byok/authmw/middleware.go` (duplicated envelope).
- Created ticket `LLM-PROXY-BYOK-HARDENING` with design doc and this diary.
- Wrote the design doc with one section per item, each anchored to the diary step that flagged it.

### Why
- These items are independent, low-risk, and each too small for its own ticket. Batching them shares a review surface (the byok packages) and keeps the ticket count manageable.

### What worked
- Every item mapped cleanly to a documented "future" note in the existing diaries, so no new investigation was needed — only scoping and sequencing.

### What didn't work
- N/A. This was a ticket-creation and design-writing step.

### What I learned
- The cost-in-dollars budget is the largest item and hinges on a price-table design decision (config file vs DB table vs profile metadata) that may warrant its own spike. Flagged in the design doc as a candidate for splitting into a follow-on.

### What was tricky to build
- Keeping the boundary clean between this ticket (medium hardening) and LLM-PROXY-BYOK-PROD-READINESS (high-priority gaps). The split: session invalidation and the meter circuit breaker are high-priority (production-blocking) and went to PROD-READINESS; the rate-limiter pruning and dev-login guard are medium and stayed here.

### What warrants a second pair of eyes
- Whether the cost-budget price table should live in a config file (lean) or a DB table. This decision shapes the largest task in the ticket.

### What should be done in the future
- Implement the items in the design doc's plan order (schema versioning early, since it is a prerequisite for any future schema change).

### Code review instructions
- Start with the design doc: `design-doc/01-byok-hardening-plan.md`.
- Verify the source anchors: `rg -n "TODO|FIXME" pkg/byok/` (currently empty — these are design-level, not code-marked).

### Technical details
- All items are documented in LLM-PROXY-BYOK diary Steps 1–5 "What should be done in the future" sections.
