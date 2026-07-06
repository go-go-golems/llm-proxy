---
Title: 'BYOK for llm-proxy: prior art analysis and architecture proposal'
Ticket: LLM-PROXY-BYOK
Status: active
Topics:
    - byok
    - auth
    - security
    - metering
    - llm-proxy
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: ../../../../../../../2026-04-17--byok-host/ttmp/2026/04/17/BYOK-BROKER--brokered-byok-inference-for-browser-llm-chat-apps/design-doc/01-delegated-byok-broker-design-and-implementation-guide.md
      Note: Prior-art delegated broker security model and implementation guide
    - Path: ../../../../../../../2026-04-17--byok-host/ttmp/2026/04/17/BYOK-BROKER-WEB-UI--full-web-ui-for-broker-login-credential-management-and-delegated-website-auth/design-doc/01-web-ui-and-delegated-auth-flow-design.md
      Note: Prior-art web UI and delegated auth flow design
    - Path: ../../../../../../../2026-04-17--byok-host/ttmp/2026/04/17/BYOK-KEYCLOAK-STORAGE--integrate-keycloak-in-docker-compose-and-add-pluggable-storage-with-sqlite/design-doc/01-keycloak-integration-and-pluggable-storage-design.md
      Note: Prior-art Keycloak OIDC and pluggable SQLite storage design
    - Path: pkg/profiles
      Note: Profile registry that must become per-user and token-scoped
    - Path: pkg/server/server.go
      Note: llm-proxy HTTP server where the auth/metering middleware will be added
ExternalSources:
    - https://oauth.net/2/browser-based-apps/
    - https://www.ietf.org/archive/id/draft-ietf-oauth-browser-based-apps-26.html
Summary: Confirms that the 2026-04-17 byok-host workspace is the prior BYOK broker work, inventories its three tickets and prototypes, analyzes the current llm-proxy codebase, and proposes an architecture where an OAuth webapp manages provider credentials and mints scoped tokens that llm-proxy validates, scopes, and meters.
LastUpdated: 2026-07-05T19:10:00-04:00
WhatFor: Orient implementers before adding BYOK credential brokering and scoped-token enforcement to llm-proxy.
WhenToUse: Read this before designing or implementing the BYOK webapp, token minting, or the llm-proxy auth/metering middleware.
---


# BYOK for llm-proxy: prior art analysis and architecture proposal

## Executive Summary

We want to add **Bring Your Own Key (BYOK)** functionality around llm-proxy:

1. A **webapp** where a user logs in via OAuth/OIDC and manages LLM provider credentials (OpenAI, Anthropic, Gemini, …).
2. A **token-minting** facility where the user creates broker tokens with explicit restrictions — token/usage budgets, model allowlists, expiry, rate limits.
3. **llm-proxy** as the enforcement point: clients call its OpenAI-compatible API with a minted token; the proxy validates the token, applies its scope, meters usage against the budget, and performs the actual upstream inference through Geppetto using the stored provider credential — which is never revealed to the client.

**Prior-art verification result:** the `2026-04-17--byok-host` workspace is indeed this exact idea. It is a doc-first greenfield workspace containing three docmgr tickets (BYOK-BROKER, BYOK-BROKER-WEB-UI, BYOK-KEYCLOAK-STORAGE) that define the delegated BYOK broker model, a web UI for login/credential-management/consent, and a Keycloak + pluggable-SQLite-storage foundation. It contains runnable Go prototypes, but only as ticket-scoped smoke-test harnesses under `ttmp/.../scripts/` — there is no production broker codebase. Meanwhile, llm-proxy is a working OpenAI-compatible proxy with **no auth layer at all**. This ticket merges the two: the byok-host design becomes the auth/credential/token layer, llm-proxy becomes the inference data plane.

## Problem Statement

llm-proxy today (see `pkg/server/server.go`, `cmd/llm-proxy-server/main.go`) resolves the OpenAI `model` field to a Geppetto profile slug from a static `--profiles` YAML and runs inference. It has:

- no authentication — anyone who can reach the listener can use it,
- no per-user provider credentials — API keys live in profile YAML / environment on the server,
- no scoping — every caller sees every profile via `/v1/models`,
- no metering — no token counting, quotas, or rate limits per caller.

Browser-direct BYOK (pasting provider keys into third-party sites) is unacceptable: the site and any XSS on it can steal the key, and provider CORS gets in the way. The byok-host BYOK-BROKER design doc lays this threat model out in detail. What we need is the **delegated broker** model: the user parks credentials with us once, and hands out narrow, revocable, metered capabilities instead.

## Prior Art: the 2026-04-17 byok-host workspace

Location: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/2026-04-17--byok-host` (docs-only repo, `ttmp/` + docmgr config; all code lives inside ticket `scripts/` directories).

### Ticket 1 — BYOK-BROKER (brokered BYOK inference for browser LLM chat apps)

- Core design doc: `design-doc/01-delegated-byok-broker-design-and-implementation-guide.md`.
- Defines the trust model: third-party sites get a **narrow broker capability, never the raw provider key**; broker does policy enforcement, provider routing, upstream credential use, and audit.
- Requirements already match this ticket: connect provider credentials, mint short-lived audience-bound tokens, OpenAI-compatible broker API, per-site allowlists and quotas, revocation independent of the provider account.
- Prototype: `scripts/byok-smoke/` — Glazed CLI with a **broker** and a **fake provider** validating the bearer-token boundary end to end via tmux smoke tests.
- Points at reusable OAuth/OIDC reference code in `go-go-mcp/pkg/auth/oidc` (authorization-code + PKCE, discovery, introspection, SQLite persistence).

### Ticket 2 — BYOK-BROKER-WEB-UI (full web UI)

- Broker login UI, dashboard, credential-management screens, delegated website consent/revocation flow, and a demo client website calling the broker from the browser.
- Design doc: `design-doc/01-web-ui-and-delegated-auth-flow-design.md`; runnable tmux web demo.

### Ticket 3 — BYOK-KEYCLOAK-STORAGE (Keycloak + pluggable storage)

- Replaces demo auth with **Keycloak in Docker Compose** as the OIDC identity provider, and introduces a **pluggable storage interface with SQLite** as first backend (see `scripts/byok-keycloak-demo/internal/storage/{interfaces,models}.go`, `sqlite/store.go`, `memory/store.go`).
- This is the production-oriented answer to "login with OAuth": we are an OIDC relying party against Keycloak (or any OIDC IdP), not our own password store.

### What byok-host did NOT do

- No integration with llm-proxy or Geppetto — its broker prototypes forward to a fake provider, not real engines.
- No real metering (token counting per request) or model-allowlist enforcement; quotas are designed but not implemented.
- No production repo; everything is ticket-scoped prototype code meant to be promoted later.

## Current llm-proxy state (verified 2026-07-05)

- `cmd/llm-proxy-server/main.go` — single `serve` command, `--profiles`, `--listen`.
- `pkg/server/` — HTTP server, SSE streaming, error mapping; **no middleware chain for auth**.
- `pkg/profiles/` — Geppetto engine-profile registry; the OpenAI `model` field is a profile slug.
- `pkg/openaichat/`, `pkg/openaicompletions/` — request/response mapping to Geppetto `turns.Turn`.
- `pkg/runtime/` — engine creation from resolved profiles.
- Endpoints: `/healthz`, `/v1/models`, `/v1/completions`, `/v1/chat/completions` (streaming + non-streaming, tools, multimodal, thinking).

Key architectural fit: Geppetto profiles already encapsulate "provider + model + settings + API key". BYOK means profiles (or their api-key material) become **per-user, database-backed, and resolved per-request from the validated token** instead of loaded once from a static YAML file.

## Proposed Solution

Two cooperating components:

### 1. BYOK control plane (webapp + API)

- **Login:** OIDC relying party against Keycloak (per BYOK-KEYCLOAK-STORAGE); session for the management UI.
- **Credential vault:** CRUD for provider credentials (provider type, label, secret), encrypted at rest (age/AES-GCM envelope; KMS/HSM out of scope for v1). Raw secrets are write-only through the UI — never returned by any API.
- **Token minting:** the user creates broker tokens bound to:
  - one or more provider credentials,
  - a **model allowlist** (profile slugs / patterns),
  - a **usage budget** (max prompt+completion tokens, and/or request count),
  - expiry, optional rate limit (req/min), optional audience (origin) restriction.
- Tokens are opaque high-entropy strings stored hashed (like GitHub PATs); alternatively short-lived JWTs + introspection later. Revocation = delete/disable row.
- Storage: the pluggable-storage/SQLite design from BYOK-KEYCLOAK-STORAGE (users, credentials, tokens, grants, usage ledger).

### 2. llm-proxy data plane (enforcement)

Per-request flow in llm-proxy:

1. `Authorization: Bearer <minted-token>` → auth middleware validates against the token store (shared DB or introspection endpoint on the control plane).
2. Scope check: requested `model` must be in the token's allowlist; `/v1/models` lists only allowed models.
3. Budget/rate pre-check: reject with OpenAI-style `429`/`403` errors when exhausted.
4. Profile resolution: build the Geppetto engine using the **user's stored provider credential** attached to the token, not server-global keys.
5. Metering: count prompt/completion tokens from the upstream response (and streamed usage chunks), append to a usage ledger, decrement budget atomically.
6. Audit log: token id, user, model, token counts, decision.

```
client (SDK/website) ──bearer token──▶ llm-proxy ──user's provider key──▶ provider
                                          │  ▲
                                validate/meter  credentials+tokens
                                          ▼  │
                                   BYOK control plane (OAuth login, vault, minting UI)
                                          │
                                       Keycloak (OIDC)
```

## Design Decisions

- **llm-proxy is the enforcement point, not a new broker binary.** The byok-host broker prototype duplicated an OpenAI-compatible surface; llm-proxy already has a good one with streaming/tools/multimodal. We add middleware, not a second proxy.
- **Keycloak (OIDC) for user login** rather than home-grown auth — per BYOK-KEYCLOAK-STORAGE rationale.
- **Opaque hashed tokens for v1**, DB lookup on each request. Simple, revocable, and we need the DB hit anyway for budget accounting. JWT/introspection can come later for multi-node.
- **Pluggable storage with SQLite first**, reusing the byok-host storage interface design.
- **Metering from provider usage data** (usage fields in responses / SSE usage chunks), with estimated pre-checks; exact accounting is post-hoc and budgets are soft-realtime.

## Alternatives Considered

- **Browser-direct BYOK** (site holds the key): rejected — XSS/key-theft and CORS problems; the whole point of the broker model (BYOK-BROKER design doc §threat model).
- **Standalone broker in front of llm-proxy** (byok-host prototype topology): rejected — double request mapping, two OpenAI surfaces to maintain, streaming passthrough complexity.
- **Per-user static profile YAML files**: rejected — no revocation, no budgets, secrets on disk in plaintext, doesn't scale past one user.
- **JWT-only stateless tokens**: deferred — cannot enforce remaining-budget or instant revocation without state anyway.

## Implementation Plan

- **Phase 0 — Promote design**: extract the byok-host designs into this ticket (done here); define the DB schema (users, credentials, tokens, scopes, usage_ledger).
- **Phase 1 — Token enforcement in llm-proxy**: auth middleware, token store (SQLite), model allowlist, static test tokens minted via CLI (`llm-proxy-server token mint ...`). No UI yet.
- **Phase 2 — Per-user credentials**: credential vault table + encryption; profile resolution swaps in the token's provider credential; usage ledger + budget enforcement (non-streaming first, then SSE usage accounting).
- **Phase 3 — Control-plane webapp**: OIDC login against Keycloak (Docker Compose from BYOK-KEYCLOAK-STORAGE), credential CRUD UI, token minting/revocation UI, usage dashboard — porting the BYOK-BROKER-WEB-UI flows.
- **Phase 4 — Delegated website flow (optional follow-up)**: third-party site registration + consent flow from BYOK-BROKER, audience-bound tokens.
- Each phase gets tmux/smoke-test playbooks in the style of the byok-host tickets.

## Open Questions

- Single binary (llm-proxy serves the control-plane UI too) vs two services sharing a DB?
- Token counting for streaming when a provider omits usage in SSE — estimate via tokenizer, or buffer-and-count?
- Should scopes reference Geppetto **profile slugs** or raw provider/model pairs? (Profiles are the natural unit here.)
- Where does the control-plane code live — this repo or a new `byok-host` production repo?
- Keycloak mandatory, or pluggable OIDC issuer from day one?

## References

- byok-host BYOK-BROKER design: `2026-04-17--byok-host/ttmp/2026/04/17/BYOK-BROKER--brokered-byok-inference-for-browser-llm-chat-apps/design-doc/01-delegated-byok-broker-design-and-implementation-guide.md`
- byok-host web UI design: `.../BYOK-BROKER-WEB-UI--full-web-ui-for-broker-login-credential-management-and-delegated-website-auth/design-doc/01-web-ui-and-delegated-auth-flow-design.md`
- byok-host Keycloak/storage design: `.../BYOK-KEYCLOAK-STORAGE--integrate-keycloak-in-docker-compose-and-add-pluggable-storage-with-sqlite/design-doc/01-keycloak-integration-and-pluggable-storage-design.md`
- llm-proxy base design ticket: `ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/`
- OAuth reference code: `go-go-mcp/pkg/auth/oidc` (authorization-code + PKCE + introspection + SQLite)
