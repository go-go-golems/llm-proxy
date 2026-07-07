---
Title: 'Analysis and implementation guide: React UI on @go-go-golems/rag-evaluation-site'
Ticket: LLM-PROXY-BYOK-REACT-UI
Status: active
Topics:
    - byok
    - frontend
    - llm-proxy
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources:
    - https://www.npmjs.com/package/@go-go-golems/rag-evaluation-site
Summary: Future-work analysis for replacing the vanilla-JS BYOK dashboard with a React app built on the published @go-go-golems/rag-evaluation-site widget library (v0.1.16 on npm) — package inventory, integration options, API surface it must cover, and a phased implementation sketch to expand when the work starts.
LastUpdated: 2026-07-05T21:00:00-04:00
WhatFor: Preserve the analysis needed to start the React control-plane UI once the BYOK core is settled.
WhenToUse: Read when picking up the React UI work; expand into a full design before implementing.
---

# Analysis and implementation guide: React UI on @go-go-golems/rag-evaluation-site

## Executive Summary

The BYOK control plane currently ships a deliberately minimal dashboard: one embedded HTML page plus a small fetch layer (`pkg/byok/web/static/{index.html,app.js}`), server-rendered from the llm-proxy binary. This ticket captures the plan to replace it with a proper React application built on **`@go-go-golems/rag-evaluation-site`** — the React Widget-IR renderer and site shell developed in `/home/manuel/workspaces/2026-07-03/improve-rag-evaluation-system/rag-evaluation-system/packages/rag-evaluation-site`. This is queued work: it starts once the BYOK core (ticket LLM-PROXY-BYOK, phases 0–3 done) has settled.

**Package status (verified 2026-07-05):** the library IS published on npm as `@go-go-golems/rag-evaluation-site`, `dist-tags.latest = 0.1.16`, matching the workspace version. It can be consumed as a plain npm dependency; no workspace/link setup is required in llm-proxy.

## Problem Statement

The vanilla-JS dashboard was the right call for proving the control plane end to end, but it will not scale to the UI the BYOK product wants: usage charts over the ledger, model pickers fed by profile metadata, multi-step mint flows, per-token drill-downs, and eventually the Phase 4 consent screens. Hand-rolled DOM code for that scope becomes unmaintainable, and the go-go-golems ecosystem already has a themed, storybook-covered widget system for exactly this kind of data-heavy UI.

## What @go-go-golems/rag-evaluation-site provides

From the package source (`packages/rag-evaluation-site/src/`):

- `widgets/` — the Widget IR renderer (`WidgetRenderer`), a widget **registry** (`registry.ts`, `defaultRegistry.ts`), IR types (`ir.ts`), action dispatch (`actions.ts`), cell renderers, and extensive Storybook coverage (forms, tables, CMS, diagrams, course-handout scenarios). UI screens can be described as IR documents and rendered generically, or composed from the exported React components directly.
- `app/` + `context/` + `hooks/` — the default site shell (layout, navigation, theming via `styles.css`/`theme.css`) and React context/hooks plumbing.
- `cms/` — content-page machinery, useful for docs/help pages inside the dashboard.
- Peer stack observed in the repo's own consumer (`web/package.json`): React 19, `@reduxjs/toolkit` (rtk-query), Vite, Tailwind 4, Storybook 10, TypeScript, pnpm, biome.

Two consumption modes are available and should be decided at kickoff:

1. **Widget-IR-first** — llm-proxy's UI screens are IR documents (JSON) rendered by `WidgetRenderer`; custom BYOK widgets (usage bar, token card, mint form) register into the widget registry. Pro: server can evolve screens without redeploying the frontend; consistent with the library's design center. Con: form-heavy flows (minting) may fight the IR abstraction.
2. **Component-first** — use the shell, theme, and exported components as an ordinary React component library; write BYOK pages as plain TSX with rtk-query against `/api/*`. Pro: simplest path. Con: less reuse of the IR/action machinery.

Recommendation to validate at kickoff: component-first for the CRUD flows, Widget-IR for the usage/reporting surfaces (tables and dashboards are exactly what the IR renderer is good at).

## The API surface the UI must cover

Already implemented and stable (see LLM-PROXY-BYOK `pkg/byok/web/api.go`):

- Session: OIDC login redirect flow (`/login`, `/auth/callback`, `POST /logout`), cookie-authenticated; `GET /api/me`.
- Credentials: `GET/POST /api/credentials`, `DELETE /api/credentials/{id}` (secret write-only; `secret_last4` display).
- Tokens: `GET/POST /api/tokens` (mint response carries plaintext exactly once), `POST /api/tokens/{id}/revoke`; budgets/limits/expiry fields; live `used_tokens`/`used_requests` counters.
- Usage: `GET /api/usage?token_id=&since=` (ledger rows, RFC3339 `since`).
- CSRF posture: SameSite=Lax cookie plus same-origin check on mutations — a same-origin SPA needs no extra headers; a dev server on another port must proxy `/api` (Vite `server.proxy`) rather than call cross-origin.

Likely API additions for the UI (create as tasks when work starts): per-model usage aggregates, a models endpoint for the mint form (profile slugs + display names — the data-plane `/v1/models` requires a bearer token, so the control plane needs its own session-authed listing), and pagination on the ledger.

## Integration shape (sketch)

```
llm-proxy/
  web/                        # new: pnpm + Vite + React 19 + TS app
    package.json              # deps: @go-go-golems/rag-evaluation-site@^0.1.16,
    vite.config.ts            #       react, react-dom, @reduxjs/toolkit
    src/
      api/                    # rtk-query slice over /api/*
      pages/                  # Credentials, Tokens, Usage, (later: Consent)
      widgets/                # BYOK widget registrations (usage bar, token card)
  pkg/byok/web/               # Go side unchanged; serves built assets
```

Build/embed follows the repo's established Go+SPA conventions: `go generate` builds the frontend and copies `web/dist` into an embedded FS served at `/app` + `/static/` (replacing today's two static files), keeping the single-binary deploy story. The go-web-dagger-pnpm-build and go-web-frontend-embed skills document the exact pattern used across go-go-golems repos.

## Implementation plan (to expand at kickoff)

1. **Spike (½ day):** scaffold `web/` with Vite + the npm package; render the site shell with a hardcoded token list; confirm theming and bundle size are acceptable.
2. **API client (½ day):** rtk-query slice for `/api/me`, credentials, tokens, usage; Vite dev proxy for same-origin cookies.
3. **Port existing screens (1–2 days):** credentials CRUD, mint flow (with the one-time plaintext reveal treated as a modal with copy button), token list with usage bars, revoke.
4. **Usage/reporting (1–2 days):** ledger table + per-model aggregates, ideally as Widget-IR documents rendered by `WidgetRenderer`.
5. **Embed + CI (½ day):** `go generate` build, embedded assets, CI check that generated assets are current.
6. **Later:** Phase 4 consent screens reuse the same shell.

## Open questions

- Widget-IR vs component-first split (see recommendation above) — validate against the actual mint-form ergonomics in the spike.
- Version policy: pin `@go-go-golems/rag-evaluation-site` minor (0.1.x is pre-1.0; breaking changes possible) and decide who bumps it.
- Does the library's Tailwind 4 setup coexist with being embedded under llm-proxy's `/static/` path prefix (asset base path in Vite config)?
- Storybook for BYOK widgets: in llm-proxy's `web/` or contributed upstream to the rag-evaluation-site package?

## References

- Library source: `/home/manuel/workspaces/2026-07-03/improve-rag-evaluation-system/rag-evaluation-system/packages/rag-evaluation-site/`
- Reference consumer: `/home/manuel/workspaces/2026-07-03/improve-rag-evaluation-system/rag-evaluation-system/web/`
- Current dashboard to replace: `pkg/byok/web/static/` (llm-proxy)
- BYOK API handlers: `pkg/byok/web/api.go` (llm-proxy)
- npm: https://www.npmjs.com/package/@go-go-golems/rag-evaluation-site (0.1.16)
