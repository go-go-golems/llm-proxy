---
Title: Investigation diary
Ticket: LLM-PROXY-BYOK-GEPETTO
Status: active
Topics:
    - byok
    - geppetto
    - llm-proxy
    - security
    - inference
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: Chronological diary for the geppetto-dependencies ticket, starting from the assessment of the LLM-PROXY-BYOK implementation and the two geppetto-side blockers it surfaced.
LastUpdated: 2026-07-06T11:35:00-04:00
WhatFor: Preserve the investigation trail so future sessions can resume the geppetto SSRF escape hatch and include_usage work without re-deriving context.
WhenToUse: Read before resuming work on this ticket; append an entry per work session.
---

# Investigation diary

## Goal

Track the geppetto-side dependencies that block or limit the BYOK effort: the SSRF guard that prevents local-provider testing, and the missing `stream_options.include_usage` streaming support.

## Step 1: Ticket creation from the BYOK assessment

This ticket was created during a post-implementation assessment of the LLM-PROXY-BYOK work. The assessment reviewed the byok-host prior-art tickets, the llm-proxy implementation diaries (Steps 1–6), the PROJ note, and the current code state (builds clean, tests green, 0 TODOs, ~3,800 LOC, branch `task/llm-proxy-byok` local-only). Two geppetto-side concerns were identified as worth their own ticket because they span both the geppetto and llm-proxy repos and are not pure llm-proxy changes.

### Prompt Context

**User prompt (verbatim):** "create a geppetto ticket to address the issues with using geppetto packaging to test, and for include_usage maybe?"

**Assistant interpretation:** Create a docmgr ticket (in the llm-proxy workspace) tracking the geppetto-side blockers: the SSRF guard that blocks local plain-HTTP fake providers in tests, and the missing `stream_options.include_usage` streaming support.

**Inferred user intent:** Separate the geppetto-cross-cutting concerns from the llm-proxy-internal hardening so each can be scoped and picked up independently.

### What I did
- Verified the geppetto SSRF guard source: `pkg/security/outbound_url.go` defines `ValidateOutboundURL(rawURL, OutboundURLOptions{AllowHTTP, AllowLocalNetworks})` — the primitive is well-designed and supports opt-in.
- Confirmed every LLM provider call site hard-codes `AllowHTTP: false`: `pkg/steps/ai/claude/api/completion.go:91,:152`, `pkg/steps/ai/openai_responses/engine.go:104`, `pkg/steps/ai/openai_responses/token_count.go:72`.
- Confirmed `pkg/embeddings/ollama.go:52` already opts into `AllowHTTP: true, AllowLocalNetworks: true` — proving the mechanism is reachable, just not exposed for LLM providers.
- Created ticket `LLM-PROXY-BYOK-GEPETTO` with design doc and this diary.
- Wrote the design doc covering both issues with file/line-anchored evidence, proposed profile-level opt-in for the SSRF hatch, and the llm-proxy-side `include_usage` implementation plan.

### Why
- These two concerns are not pure llm-proxy changes: the SSRF hatch requires a geppetto change (threading `OutboundURLOptions` from settings), and `include_usage` spans the geppetto streaming result and the llm-proxy SSE frame. They deserve a focused ticket rather than being buried in a generic hardening list.

### What worked
- The geppetto source confirmed the diagnosis from the LLM-PROXY-BYOK diary Step 3 verbatim: the primitive supports opt-in, the callers just hard-code `false`.

### What didn't work
- N/A. This was a ticket-creation and design-writing step.

### What I learned
- The Ollama embeddings provider is the existing precedent for opting into local HTTP — the fix is "do what Ollama does, but make it profile-configurable for LLM providers."

### What was tricky to build
- Scoping the `include_usage` work correctly: the usage data already reaches llm-proxy via `result.Usage`, so the gap is purely llm-proxy's SSE frame emission, not a geppetto streaming change. Getting that boundary right keeps the geppetto side of this ticket small (verify only).

### What warrants a second pair of eyes
- Whether the SSRF opt-in should be two separate booleans (`AllowHTTP`, `AllowLocalNetworks`) matching the existing struct, or a single `dev: true` profile flag. The design doc leans separate to match the primitive.

### What should be done in the future
- Implement the geppetto-side settings threading (Step 1 of the design doc plan).
- Consider filing a geppetto GitHub issue for the SSRF escape hatch if the geppetto maintainers want to track it upstream (the glazed issues were filed; geppetto was kept as a docmgr ticket per the user's request).

### Code review instructions
- Start with the design doc: `design-doc/01-geppetto-dependencies-ssrf-escape-hatch-and-streaming-include-usage.md`.
- Verify the call-site evidence: `rg -n "AllowHTTP" geppetto/pkg/`.

### Technical details
- Geppetto repo: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/geppetto` (remote `go-go-golems/geppetto`).
- llm-proxy streaming plumbing point: `pkg/runtime/chat_service.go` (the goroutine that owns `result.Usage`).
