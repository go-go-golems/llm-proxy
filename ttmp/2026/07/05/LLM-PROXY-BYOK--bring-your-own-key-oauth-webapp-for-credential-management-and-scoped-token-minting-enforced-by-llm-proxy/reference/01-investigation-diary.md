---
Title: Investigation diary
Ticket: LLM-PROXY-BYOK
Status: active
Topics:
    - byok
    - auth
    - security
    - metering
    - llm-proxy
DocType: reference
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: Chronological record of the LLM-PROXY-BYOK investigation, starting with the verification that the 2026-04-17 byok-host workspace is the prior BYOK broker work and the inventory of the current llm-proxy codebase.
LastUpdated: 2026-07-05T19:20:00-04:00
WhatFor: Preserve the investigation trail so future sessions can resume without re-deriving context.
WhenToUse: Read before resuming work on this ticket; append an entry per work session.
---

# Investigation diary

## Goal

Track, chronologically, what was investigated, what was concluded, and what remains open for the BYOK-on-llm-proxy effort.

## 2026-07-05 — Ticket creation and prior-art verification

**Question:** is `2026-04-17--byok-host` the earlier work on "OAuth webapp + credential management + scoped token minting + proxy enforcement"?

**Answer: yes.** Findings:

- `2026-04-17--byok-host` is a docs-only repo (`.ttmp.yaml` + `ttmp/`, no top-level source tree). All code is ticket-scoped prototypes under `ttmp/.../scripts/`.
- It holds three docmgr tickets, all dated 2026-04-17, all status `active`:
  1. **BYOK-BROKER** — "Brokered BYOK inference for browser LLM chat apps". Core design doc defines the delegated-broker trust model (site gets a narrow revocable capability, never the raw provider key), OpenAI-compatible broker API, scoped short-lived tokens, per-site quotas/allowlists, audit. Prototype `scripts/byok-smoke/` (Glazed CLI: broker + fake provider) validates the bearer-token boundary via tmux smoke tests.
  2. **BYOK-BROKER-WEB-UI** — full web UI: broker login, dashboard, credential management, delegated website consent/revocation, demo client site.
  3. **BYOK-KEYCLOAK-STORAGE** — Keycloak in Docker Compose as OIDC IdP replacing demo auth, plus a pluggable storage interface with SQLite and memory backends (`scripts/byok-keycloak-demo/internal/storage/`).
- Gaps in byok-host: no integration with a real inference path (fake provider only), no implemented metering/budgets, nothing promoted to a production repo.

**llm-proxy current state** (this workspace, `llm-proxy/`):

- OpenAI-compatible proxy backed by Geppetto; `model` field = Geppetto profile slug from static `--profiles` YAML.
- Endpoints: `/healthz`, `/v1/models`, `/v1/completions`, `/v1/chat/completions` (SSE streaming, tools, multimodal, thinking).
- `pkg/{server,profiles,runtime,openaichat,openaicompletions}`; **no auth, no per-user anything, no metering**.
- One prior ticket in its ttmp: `2026-06-04-llm-proxy-openai-compatible-geppetto-proxy` (base proxy design).

**Actions taken:**

- Added vocabulary topics `byok`, `auth`, `security`, `metering`.
- Created ticket **LLM-PROXY-BYOK** with index, design doc `design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md`, this diary, phased task list, and file relations.

**Key architectural conclusion:** don't build a second broker binary in front of llm-proxy (the byok-host prototype topology); instead add the control plane (OIDC login, credential vault, token minting) alongside and make llm-proxy itself the enforcement point via auth middleware + per-request credential resolution + usage ledger.

**Next steps:** Phase 0 schema design, then Phase 1 token middleware in llm-proxy (see tasks.md).

## 2026-07-05 (later) — Full intern design/implementation guide

Deep-dived both codebases to write `design-doc/02-intern-guide-byok-system-analysis-design-and-implementation.md`:

- Extracted the complete technical substance from byok-host: threat model, proposed API surfaces and scopes, the two-layer OAuth structure (broker as OIDC RP toward Keycloak *and* as its own authorization server toward client sites), storage interfaces + SQLite DDL, Keycloak compose/realm details, and the byok-smoke token-separation prototype. Confirmed gaps: plaintext `api_key` column (encryption designed but not shipped), no metering/budgets/rate limits, no real inference path, non-constant-time token compare in the smoke prototype.
- Mapped llm-proxy precisely: middleware insertion point `cmd/llm-proxy-server/main.go:134`; API keys live in `ResolvedProfileRuntime.Settings.API.APIKeys["<apitype>-api-key"]`; `EngineProvider` (`pkg/runtime/engine_provider.go:12`) is the seam for per-request credential injection; `result.Usage` from `RunInferenceWithResult` (`chat_service.go:49`, `:89` for streaming) is the authoritative metering source — wire chunks carry no usage.
- Key design calls made in the guide: one binary/two planes/one SQLite DB; opaque `llmp_` tokens stored SHA-256-hashed; AES-256-GCM vault with credential-id AAD; ledger + denormalized counters in one transaction; post-hoc budget enforcement (documented ≤1-request overshoot); delegated website OAuth deferred to Phase 4.

## Related

- `../design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md`
- byok-host workspace: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/2026-04-17--byok-host/`
