---
Title: 'Intern guide: BYOK system analysis, design, and implementation'
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
    - Path: ../../../../../../../2026-04-17--byok-host/ttmp/2026/04/17/BYOK-KEYCLOAK-STORAGE--integrate-keycloak-in-docker-compose-and-add-pluggable-storage-with-sqlite/scripts/byok-keycloak-demo/internal/storage/interfaces.go
      Note: Prior storage interface design the guide's data model extends
    - Path: cmd/llm-proxy-server/main.go
      Note: Middleware insertion point and server wiring described in the guide
    - Path: pkg/profiles/resolver.go
      Note: ResolvedProfileRuntime and API-key location
    - Path: pkg/runtime/engine_provider.go
      Note: EngineProvider seam for per-request credential injection
ExternalSources:
    - https://oauth.net/2/browser-based-apps/
    - https://www.ietf.org/archive/id/draft-ietf-oauth-browser-based-apps-26.html
    - https://cheatsheetseries.owasp.org/cheatsheets/HTML5_Security_Cheat_Sheet.html
Summary: Complete intern-facing analysis, design, and implementation guide for the BYOK system — threat model, current llm-proxy internals with file/line references, prior byok-host work, target architecture (control plane + data plane), data model and DDL, token design, API reference, pseudocode for middleware/engine-provider/metering, and a phased implementation plan with tests.
LastUpdated: 2026-07-05T20:30:00-04:00
WhatFor: Give a new engineer everything needed to understand and build the BYOK credential vault, token minting webapp, and llm-proxy enforcement layer.
WhenToUse: Read start-to-finish before writing any BYOK code; use Part V as a working reference during implementation.
---


# Intern guide: BYOK system analysis, design, and implementation

> **Audience.** You are a new engineer joining this project. This document assumes you know Go, HTTP, and roughly what OAuth is, but assumes **zero** knowledge of this codebase, Geppetto, or the prior BYOK prototypes. It is intentionally long: it is the primary onboarding artifact. Read Parts I–III before writing code; keep Parts IV–V open while implementing.

---

## Part I — What we are building and why

### 1.1 The product in one paragraph

Users log into a web application with OAuth (OpenID Connect, backed by Keycloak). There they store their **own** LLM provider API keys — an Anthropic key, an OpenAI key, a Gemini key — in an encrypted credential vault. From those credentials they **mint tokens**: opaque bearer strings carrying explicit restrictions such as *"may only call `sonnet` and `gpt-4o-mini`, may spend at most 2,000,000 tokens, expires in 30 days, at most 60 requests/minute"*. They hand those minted tokens to scripts, teammates, CI jobs, or third-party websites. Those consumers then talk to **llm-proxy** — our existing OpenAI-compatible proxy — using the minted token as their API key. llm-proxy validates the token, restricts what it can see and do, **meters** every request against the token's budget, and performs the actual upstream inference using the user's stored provider credential. The raw provider key never leaves our server.

### 1.2 Why not just paste API keys into apps? (the threat model)

This is the core security argument, taken from the prior BYOK-BROKER design work (see §2.4). The naive alternative — "browser-direct BYOK", where a website asks you to paste your OpenAI key and stores it in `localStorage` — fails because of how browsers actually work:

- The browser sandbox protects the **operating system from the page**. It does *not* protect **secrets from JavaScript running in the same origin**. Any script in the page (including injected XSS, a compromised dependency, or a malicious browser extension with page access) can read in-memory variables, DOM, `localStorage`, `sessionStorage`, IndexedDB, and non-HttpOnly cookies.
- Long-lived provider keys are high-value: they are usually account-wide, hard to scope, and revocation is provider-specific and disruptive.
- Direct browser→provider calls also require provider CORS support, which is inconsistent.

The comparison table from the prior design:

| Threat | Browser-direct BYOK | Brokered (our design) |
|---|---|---|
| Website sees raw provider key | Yes | **No** |
| Site XSS can steal long-lived provider key | High risk | Avoided — site only holds a short-lived, scoped token |
| Provider CORS required | Yes | No — server-to-server upstream |
| Per-site revocation | Hard, provider-specific | One click at the broker |
| Per-site quotas / model allowlists | Hard | Native feature |
| Unified audit log | Weak | Strong |
| Broker sees prompt traffic | N/A | **Yes** (honest residual risk) |

The last row matters: we must never oversell this as "end-to-end encrypted". The proxy sees prompts and completions. What we protect is the **credential**, and what we add is **scoping, metering, revocation, and audit**.

### 1.3 Actors and trust boundaries

```
                 TRUSTED (our infrastructure)                    UNTRUSTED-ish
 ┌────────────────────────────────────────────────────┐
 │                                                    │      ┌──────────────┐
 │  ┌──────────────┐   session   ┌─────────────────┐  │      │  User's      │
 │  │  Keycloak    │◀───OIDC────▶│  Control plane  │◀─┼──────│  browser     │
 │  │  (identity)  │             │  (webapp + API) │  │      │  (login, UI) │
 │  └──────────────┘             └───────┬─────────┘  │      └──────────────┘
 │                                       │ shared DB
 │                               ┌───────▼─────────┐  │      ┌──────────────┐
 │   provider keys, tokens,      │   SQLite store  │  │      │  Token       │
 │   budgets, usage ledger  ───▶ │  (vault+ledger) │  │      │  consumer    │
 │                               └───────┬─────────┘  │      │ (script, CI, │
 │                                       │            │      │  website,    │
 │                               ┌───────▼─────────┐  │      │  OpenAI SDK) │
 │  upstream calls with the      │    llm-proxy    │◀─┼──────│              │
 │  USER's stored key  ────────▶ │  (data plane)   │  │      └──────────────┘
 │                               └───────┬─────────┘  │        presents only
 │                                       │            │        a minted token
 └───────────────────────────────────────┼────────────┘
                                         ▼
                              ┌─────────────────────┐
                              │ Providers (Anthropic│
                              │ OpenAI, Gemini, …)  │
                              └─────────────────────┘
```

Four actors, four trust levels:

- **User** — owns provider credentials; authenticates via Keycloak; mints and revokes tokens.
- **Token consumer** — anything holding a minted token. Sees: the token, its own prompts/outputs, the filtered model list. Must never see: the provider key, other users' anything, unscoped model lists.
- **Our system** (control plane + llm-proxy + store) — sees everything; must encrypt credentials at rest, never log secrets, and enforce policy honestly.
- **Provider** — receives inference calls authenticated with the user's stored key; knows nothing about our tokens.

### 1.4 Glossary

| Term | Meaning |
|---|---|
| **BYOK** | Bring Your Own Key — users supply their own provider API keys. |
| **Credential / connection** | A stored provider API key + metadata (provider type, label). "Connection" is the prior work's word; we use **credential**. |
| **Minted token** | Opaque bearer string we issue, bound to one user, ≥1 credentials, and a set of restrictions. |
| **Scope** | The restriction set on a token: model allowlist, budgets, expiry, rate limit. |
| **Control plane** | The webapp + management API: login, vault, minting, dashboards. |
| **Data plane** | llm-proxy's `/v1/*` inference endpoints, now enforcing tokens. |
| **Geppetto** | go-go-golems inference library llm-proxy is built on; owns providers, engines, profiles. |
| **Profile** | A named Geppetto configuration (provider + model + settings + API key). The OpenAI `model` field of a proxy request *is* a profile slug. |
| **Usage ledger** | Append-only record of tokens spent per request, used to enforce budgets. |
| **OIDC RP** | OpenID Connect Relying Party — us, when we delegate login to Keycloak. |

---

## Part II — The existing pieces you must understand

Two bodies of prior work feed this project. You need a working mental model of both.

### 2.1 llm-proxy today: what it is

Repo: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy`, module `github.com/go-go-golems/llm-proxy` (Go 1.26). Key dependencies: `geppetto v0.13.3` (inference), `glazed v1.3.6` (CLI framework), `cobra`.

It is an OpenAI-compatible HTTP proxy. A client speaks the standard OpenAI API to it; the proxy translates each request into a Geppetto inference run and translates the result back. Crucially, the OpenAI `model` field is interpreted as a **Geppetto profile slug**, resolved from a static YAML file passed via `--profiles`.

Endpoints today (`pkg/server/server.go:69-76`, Go 1.22 method-prefixed mux patterns):

| Method | Path | Handler |
|---|---|---|
| GET | `/healthz` | `handleHealthz` |
| GET | `/v1/models` | `handleModels` — lists profiles as models |
| POST | `/v1/completions` | `handleCompletions` — streaming + non-streaming |
| POST | `/v1/chat/completions` | `handleChatCompletions` — streaming + non-streaming, tools, multimodal |

**There is no auth, no rate limiting, no metering, and no middleware chain.** A grep for `Authorization`/`middleware`/`context.WithValue` across `pkg/` finds nothing security-related. Anyone who can reach the listener can use every configured profile — and hence every server-side API key. That is the gap this project closes.

### 2.2 llm-proxy request lifecycle (the spine you will modify)

Follow one non-streaming `POST /v1/chat/completions` end to end. File references are to the llm-proxy repo.

1. `cmd/llm-proxy-server/main.go:117-150` — `runServer` builds a `profiles.NewYAMLResolver(opts.Profiles)`, wires it into `runtime.GeppettoChatCompletionService`, constructs `server.New(server.Options{...})` (line 131), and starts `&http.Server{Handler: srv.Handler()}` (line 134). **⬅ BYOK middleware wraps `srv.Handler()` here.**
2. `pkg/server/server.go:74` — mux routes to `handleChatCompletions` (line 124): body capped by `http.MaxBytesReader` (1 MiB default), decoded by `openaichat.DecodeChatCompletionRequest` (`pkg/openaichat/types.go:56`), errors returned via `writeOpenAIError` (`pkg/server/errors.go:23`) in OpenAI's `{"error":{message,type,param,code}}` shape.
3. `pkg/runtime/chat_service.go:24-54` — `Complete`:
   - `s.Profiles.ResolveProfile(ctx, req.Model)` (line 32) → `pkg/profiles/resolver.go:62`: parses the slug, resolves it in the Geppetto registry, merges inference settings, and returns a `ResolvedProfileRuntime{RegistrySlug, ProfileSlug, Settings *settings.InferenceSettings, Metadata}`. **⬅ the provider API key lives inside `Settings.API.APIKeys` (a `map[string]string` keyed like `claude-api-key`, `openai-api-key`).**
   - `s.Engines.EngineForProfile(ctx, profile)` (line 36) → `pkg/runtime/engine_provider.go:20`: `factory.CreateEngine(profile.Settings)` — Geppetto's factory reads the API key out of the settings *here*. **⬅ per-user key injection must happen before this call.**
   - `s.Mapper.RequestToTurn(req)` (line 40) → `pkg/openaichat/mapper.go:15`: OpenAI messages → Geppetto `turns.Turn` blocks.
   - `geppettoengine.RunInferenceWithResult(runCtx, eng, turn)` (line 49): the actual provider HTTP call. Returns `(out *turns.Turn, result *InferenceResult, err)`.
   - `s.Mapper.TurnToChatCompletion(req, out, result, preBlockCount)` (line 53) → response, including `Usage` from `usageFromResult` (`pkg/openaichat/mapper.go:220`). **⬅ `result.Usage` (`turns.InferenceUsage{InputTokens, OutputTokens, CacheCreationInputTokens, CacheReadInputTokens}`) is the authoritative metering source.**
4. `pkg/server/server.go:150` — `writeJSON(w, 200, resp)`.

**Streaming variant:** `chat_service.go:56-98` runs inference in a goroutine, pushing frames through a `ChatEventSink` (`pkg/openaichat/stream.go:36`) into a channel drained by `writeSSE` (`pkg/server/sse.go:48`), terminated by `data: [DONE]`. Two facts matter for us:

- Streaming responses today carry **no usage object at all** (`ChatCompletionChunk` has no `Usage` field; `stream_options.include_usage` is unimplemented).
- But the `InferenceResult` with authoritative usage **is available inside the stream goroutine** after `RunInferenceWithResult` returns (`chat_service.go:89`) — it's just only used for the finish reason. Metering can hook there.

### 2.3 Geppetto concepts in 60 seconds

- A **profile** bundles: provider api-type, model name, inference settings, API keys, base URLs. Profiles live in registries loaded from YAML (`gepprofiles.NewYAMLFileEngineProfileStore`).
- `settings.InferenceSettings` carries `API *APISettings{APIKeys map[string]string, BaseUrls map[string]string}`. Keys are named `<apitype>-api-key` — in `examples/profiles.yaml` you'll see `claude-api-key: ${ANTHROPIC_API_KEY}`.
- An **engine** (`engine.Engine`) is a ready-to-call provider client, created by `factory.NewStandardEngineFactory().CreateEngine(settings)`.
- A **turn** (`turns.Turn`) is the conversation representation (blocks of system/user/assistant/tool content).
- `RunInferenceWithResult` executes a turn against an engine and returns the extended turn plus an `InferenceResult` (stop reason + usage).

The elegant consequence: **BYOK = making the API-key entry of a resolved profile per-user and per-request** instead of static YAML. Everything downstream (engines, providers, streaming) is untouched.

### 2.4 Prior work: the `2026-04-17--byok-host` workspace

Location: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/2026-04-17--byok-host/ttmp/2026/04/17/`. A doc-first workspace: three docmgr tickets with design docs and runnable prototypes living under each ticket's `scripts/` directory. **Nothing was promoted to production code.** What each contributed:

**BYOK-BROKER** (`BYOK-BROKER--brokered-byok-inference-for-browser-llm-chat-apps/`)
- The threat model of §1.2/§1.3 (design doc §"Threat model and trust boundaries").
- A proposed API surface: OAuth endpoints (`/oauth2/auth`, `/oauth2/token`, `/oauth2/revoke`, discovery), data-plane endpoints (`/v1/models`, `/v1/chat/completions`, `/v1/broker/connections`, `/v1/broker/sessions`), and scopes (`models:list`, `inference:complete`, `inference:stream`, `connections:use:openai`, `usage:read`). Explicitly forbidden: any scope that exports raw secrets.
- The `byok-smoke` prototype (`scripts/byok-smoke/internal/runtime/broker.go`, `fake_provider.go`): a minimal broker with **two distinct bearer secrets** — the token the site presents vs. the upstream key the broker holds — proving the separation boundary. Its smoke test asserts the fake provider's response contains `provider_key_remained_server_side=true`.
- Credential storage options: **Model A** server-managed encrypted-at-rest (recommended start), Model B client-encrypted with runtime unlock, Model C provider-issued ephemeral credentials. A "cryptographically blind broker" is called out as generally impossible without provider-side delegation.

**BYOK-BROKER-WEB-UI** (`BYOK-BROKER-WEB-UI--full-web-ui-for-broker-login-credential-management-and-delegated-website-auth/`)
- A working browser demo: broker login page, dashboard with credential management, an OAuth Authorization Code + PKCE consent flow where the user picks **which stored connection** a website may use, and a demo client website that calls the broker directly from browser `fetch()` with the granted token.
- Endpoint plan: `GET/POST /login`, `POST /logout`, `GET /app`, `POST /app/connections`, `POST /app/connections/delete`, `POST /app/grants/revoke`, `GET /oauth2/auth`, `POST /oauth2/approve|deny|token`, plus the `/v1/*` data plane.

**BYOK-KEYCLOAK-STORAGE** (`BYOK-KEYCLOAK-STORAGE--integrate-keycloak-in-docker-compose-and-add-pluggable-storage-with-sqlite/`)
- Keycloak 26.2 in Docker Compose (`deploy/docker-compose.yaml`): dev mode, host port 18080, auto-imports realm `byok` (`deploy/keycloak/realm-byok.json`) with a confidential client `broker-web` (secret `broker-web-secret`), a public PKCE client `client-demo-site`, and seeded user `alice`/`password123`.
- A clean OIDC RP implementation using `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2` (`scripts/byok-keycloak-demo/internal/auth/keycloak/oidc.go`): state+nonce in short-lived HttpOnly cookies, ID-token verification, then an **HMAC-SHA256-signed app session cookie** (not a JWT) carrying the OIDC claims (`session.go`).
- A pluggable storage layer (`internal/storage/interfaces.go`): `Store = UserStore + ConnectionStore + GrantStore + OAuthArtifactStore + AuditStore`, with `memory/` and `sqlite/` implementations and a full SQLite DDL (tables `broker_users`, `connections`, `grants`, `auth_codes`, `access_tokens`, `audit_events`; JSON-encoded string arrays; transactional single-use auth codes).

**Gaps in the prior work — your job is precisely these:**

1. **No encryption at rest.** The design proposed `secret_ref`/`secret_cipher`; the shipped `connections.api_key` column is plaintext.
2. **No metering or budgets.** Only three policy checks exist (token validity, scope membership, model allowlist). No usage counters, no quotas, no rate limits. Audit events exist only in the Keycloak demo.
3. **No real inference.** All prototypes forward to a fake provider; none touch Geppetto or streaming.
4. **Naive token compare** in byok-smoke (non-constant-time string equality) — a known anti-pattern to fix.
5. Discovery endpoints, `/oauth2/revoke`, refresh tokens, `/v1/broker/sessions`, and the structured `PolicyDecision` engine were designed but unimplemented.

---

## Part III — Target system design

### 3.1 Shape decision: one binary, two planes, one database

We build **one Go binary** (llm-proxy, extended) serving two logical planes on one mux, backed by one SQLite database:

- **Data plane** — the existing `/v1/*` endpoints, now behind bearer-token middleware.
- **Control plane** — new `/login`, `/auth/callback`, `/app`, and `/api/*` endpoints, behind a session cookie (OIDC via Keycloak).

Why not a separate control-plane service? The two planes share the store on every request (token validation, budget accounting), SQLite makes cross-process sharing awkward, and one binary keeps the demo/deploy story trivial (`docker compose up keycloak && llm-proxy-server serve --byok-db ...`). The storage interface keeps a later split cheap: if we outgrow SQLite, swap the store for Postgres and split the binary then. This mirrors the "Pattern A hybrid" conclusion of the Keycloak design: Keycloak owns identity; we own domain objects (credentials, tokens, grants) in our store.

The delegated third-party-website OAuth flow (consent screens, PKCE, grants — the BYOK-BROKER-WEB-UI centerpiece) is **Phase 4, optional**. Phases 1–3 deliver the user-facing value: vault + minting + enforcement. Personal-token minting does not need an OAuth authorization server; it needs a logged-in user and a form.

### 3.2 System overview

```
            ┌───────────────────────── llm-proxy binary ─────────────────────────┐
            │                                                                    │
 browser ──▶│  CONTROL PLANE                        DATA PLANE                   │
 (cookie)   │  GET  /login ─────▶ Keycloak OIDC     POST /v1/chat/completions    │
            │  GET  /auth/callback ◀── code         POST /v1/completions         │
            │  GET  /app  (dashboard UI)            GET  /v1/models              │
            │  CRUD /api/credentials                GET  /healthz  (open)        │
            │  CRUD /api/tokens                          ▲                       │
            │  GET  /api/usage                           │ Authorization:        │
            │        │                                   │ Bearer llmp_…         │
            │        │ session middleware        token middleware                │
            │        ▼                                   ▼                       │
            │  ┌──────────────────────────────────────────────────┐             │
            │  │              pkg/byok  (new package)             │             │
            │  │  store (SQLite) · tokens · vault (AES-GCM) ·     │             │
            │  │  policy · meter · oidc session                   │             │
            │  └──────────────────────────────────────────────────┘             │
            │        │ resolved profile + injected user key                      │
            │        ▼                                                          │
            │  pkg/runtime (EngineProvider) ──▶ Geppetto ──▶ Provider APIs      │
            └────────────────────────────────────────────────────────────────────┘
```

### 3.3 Data model

Adapted from the byok-host schema, renamed to our vocabulary, with the two missing pillars added: **encrypted secrets** and the **usage ledger**. Arrays are stored as JSON text columns (prior-art convention). Open SQLite with `?_foreign_keys=on&_busy_timeout=5000` (github.com/mattn/go-sqlite3, already an indirect dependency).

```sql
CREATE TABLE users (
  id            TEXT PRIMARY KEY,          -- ULID
  oidc_subject  TEXT NOT NULL UNIQUE,      -- Keycloak `sub`
  username      TEXT NOT NULL,
  email         TEXT,
  created_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL
);

CREATE TABLE credentials (                 -- the vault
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users(id),
  provider      TEXT NOT NULL,             -- 'anthropic' | 'openai' | 'gemini' | ...
  api_type      TEXT NOT NULL,             -- geppetto api-type, e.g. 'claude', 'openai'
  label         TEXT NOT NULL,
  secret_cipher BLOB NOT NULL,             -- AES-256-GCM(nonce ‖ ciphertext), see §3.6
  secret_last4  TEXT NOT NULL,             -- display only: '…x4Kq'
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    TIMESTAMP NOT NULL,
  updated_at    TIMESTAMP NOT NULL
);
CREATE INDEX idx_credentials_user ON credentials(user_id);

CREATE TABLE tokens (                      -- minted tokens; secret stored HASHED
  id                TEXT PRIMARY KEY,
  user_id           TEXT NOT NULL REFERENCES users(id),
  token_hash        TEXT NOT NULL UNIQUE,  -- hex(SHA-256(token))
  name              TEXT NOT NULL,         -- user-facing label
  credential_ids    TEXT NOT NULL,         -- JSON array; which vault entries it may use
  allowed_models    TEXT NOT NULL,         -- JSON array of profile slugs/globs; [] = none
  max_total_tokens  INTEGER,               -- NULL = unlimited (prompt+completion)
  max_requests      INTEGER,               -- NULL = unlimited
  rate_limit_rpm    INTEGER,               -- NULL = unlimited
  expires_at        TIMESTAMP,             -- NULL = no expiry
  revoked_at        TIMESTAMP,
  created_at        TIMESTAMP NOT NULL,
  last_used_at      TIMESTAMP
);
CREATE INDEX idx_tokens_user ON tokens(user_id);

CREATE TABLE usage_ledger (                -- append-only, one row per inference call
  id                 TEXT PRIMARY KEY,
  token_id           TEXT NOT NULL REFERENCES tokens(id),
  user_id            TEXT NOT NULL,
  model              TEXT NOT NULL,        -- profile slug
  prompt_tokens      INTEGER NOT NULL,
  completion_tokens  INTEGER NOT NULL,
  cached_tokens      INTEGER NOT NULL DEFAULT 0,
  streamed           INTEGER NOT NULL DEFAULT 0,
  status             TEXT NOT NULL,        -- 'ok' | 'error' | 'rejected'
  created_at         TIMESTAMP NOT NULL
);
CREATE INDEX idx_ledger_token_time ON usage_ledger(token_id, created_at);

CREATE TABLE token_counters (              -- denormalized running totals (fast budget checks)
  token_id       TEXT PRIMARY KEY REFERENCES tokens(id),
  total_tokens   INTEGER NOT NULL DEFAULT 0,
  total_requests INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE audit_events (
  id          TEXT PRIMARY KEY,
  user_id     TEXT,
  token_id    TEXT,
  event_type  TEXT NOT NULL,   -- credential.created/.deleted, token.minted/.revoked,
                               -- inference.request, inference.rejected, ...
  payload     TEXT NOT NULL,   -- JSON; MUST NEVER contain plaintext secrets
  created_at  TIMESTAMP NOT NULL
);
CREATE INDEX idx_audit_user ON audit_events(user_id);
```

Why both a ledger **and** counters: budget checks happen on the hot path of every request; `SUM()` over the ledger degrades as it grows. The counter row is updated in the same transaction as the ledger insert, so the two cannot diverge.

Go-side, mirror the byok-host interface style so memory/SQLite backends stay swappable:

```go
// pkg/byok/store/store.go
type Store interface {
    UserStore        // UpsertUser, GetUserBySubject
    CredentialStore  // Create/Get/List/Delete credentials (cipher in, cipher out)
    TokenStore       // MintToken, GetTokenByHash, ListByUser, Revoke, TouchLastUsed
    MeterStore       // RecordUsage(ledgerRow) + CheckAndReserve(tokenID, estimate)
    AuditStore       // AppendEvent, ListEvents
    Close() error
}
```

### 3.4 Token design

- **Format:** `llmp_` + 256 bits of `crypto/rand`, base64url-encoded, no padding — e.g. `llmp_Zk9rN2Q4cUx…`. The prefix makes tokens greppable in leaked logs and lets secret scanners recognize them.
- **Storage:** we store only `hex(SHA-256(token))`. Validation hashes the presented token and looks the hash up — a leaked database does not leak usable tokens. The mint response is the **only** time the plaintext is visible.
- **Comparison:** the SHA-256-then-index-lookup pattern also sidesteps the byok-smoke prototype's non-constant-time comparison bug: we never compare secrets byte-by-byte.
- **Why opaque tokens, not JWTs:** every request must hit the DB anyway (budget accounting, instant revocation), so stateless-JWT's main benefit vanishes while its costs (key rotation, claim staleness, size) remain. Introspection/JWKS can be added later if the data plane ever runs on separate nodes.
- **Restrictions carried by the token row** (not encoded in the token string): credential bindings, model allowlist (exact slugs or `*` globs matched with `path.Match`), `max_total_tokens`, `max_requests`, `rate_limit_rpm`, `expires_at`.

### 3.5 Request-time enforcement (the heart of the system)

```
consumer                    llm-proxy                              store        provider
   │                            │                                    │              │
   │ POST /v1/chat/completions  │                                    │              │
   │ Authorization: Bearer llmp_…                                    │              │
   ├───────────────────────────▶│                                    │              │
   │                            │ 1 hash token, look up              │              │
   │                            ├───────────────────────────────────▶│              │
   │                            │ 2 valid? not revoked/expired?      │              │
   │                            │ 3 model in allowlist?              │              │
   │                            │ 4 budget/rate pre-check            │              │
   │                            │   (reject 429 if exhausted)        │              │
   │                            │ 5 load credential, decrypt key     │              │
   │                            ├───────────────────────────────────▶│              │
   │                            │ 6 resolve profile, inject key,     │              │
   │                            │   create engine, run inference     │              │
   │                            ├──────────────────────────────────────────────────▶│
   │                            │◀──────────────────────────────────────────────────┤
   │                            │ 7 read result.Usage                │              │
   │                            │ 8 ledger insert + counter update   │              │
   │                            ├───────────────────────────────────▶│              │
   │◀───────────────────────────┤ 9 OpenAI-shaped response           │              │
```

Concretely, in code, four insertion points:

**(a) HTTP middleware** wrapping `srv.Handler()` at `cmd/llm-proxy-server/main.go:134`:

```go
// pkg/byok/authmw/middleware.go — pseudocode
func TokenAuth(store byokstore.Store, next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if !strings.HasPrefix(r.URL.Path, "/v1/") {   // /healthz, control plane: pass
            next.ServeHTTP(w, r); return
        }
        raw, ok := bearerToken(r)                     // "Authorization: Bearer llmp_…"
        if !ok { writeOpenAIAuthError(w, 401, "missing_api_key"); return }

        tok, err := store.GetTokenByHash(r.Context(), sha256hex(raw))
        switch {
        case errors.Is(err, byokstore.ErrNotFound):
            writeOpenAIAuthError(w, 401, "invalid_api_key"); return
        case tok.RevokedAt != nil:
            writeOpenAIAuthError(w, 401, "token_revoked"); return
        case tok.ExpiresAt != nil && now().After(*tok.ExpiresAt):
            writeOpenAIAuthError(w, 401, "token_expired"); return
        }
        if !rateLimiter.Allow(tok.ID, tok.RateLimitRPM) {
            writeOpenAIAuthError(w, 429, "rate_limit_exceeded"); return
        }
        // budget pre-check: cheap counter read; authoritative accounting is post-hoc
        if exhausted(store.Counters(tok.ID), tok) {
            writeOpenAIAuthError(w, 429, "budget_exhausted"); return
        }
        next.ServeHTTP(w, r.WithContext(byokctx.WithToken(r.Context(), tok)))
    })
}
```

Errors reuse the existing `writeOpenAIError` shape (`pkg/server/errors.go:23`) so OpenAI SDKs surface them natively; today that helper only emits 400/500, so add an auth variant emitting `type:"invalid_request_error"` with codes like `invalid_api_key` (401) and `rate_limit_exceeded` (429) — matching OpenAI's own codes so client retry logic behaves.

**(b) Model scoping.** The token travels in `context.Context` (`byokctx`). Two consumers:
- `handleModels` / `ModelLister` (`pkg/server/server.go:82`): wrap the existing `profileModelLister` (`cmd/llm-proxy-server/main.go:156`) with a filter that intersects profiles with `tok.AllowedModels`.
- The services: `ResolveProfile` result is checked against the allowlist before engine creation — defense in depth in case a path bypasses the mux.

**(c) Per-request credential injection** — a wrapping `EngineProvider` (interface at `pkg/runtime/engine_provider.go:12`; the service structs already take it as an injectable field):

```go
// pkg/byok/engines/provider.go — pseudocode
type VaultEngineProvider struct {
    Inner runtime.EngineProvider   // FactoryEngineProvider
    Vault *byokvault.Vault
    Store byokstore.Store
}

func (p *VaultEngineProvider) EngineForProfile(ctx context.Context,
        profile *profiles.ResolvedProfileRuntime) (engine.Engine, error) {
    tok, ok := byokctx.TokenFrom(ctx)
    if !ok { return nil, errors.New("byok: no token in context") }   // fail CLOSED

    if !modelAllowed(tok.AllowedModels, profile.ProfileSlug) {
        return nil, byok.ErrModelNotAllowed
    }
    cred, err := pickCredential(ctx, p.Store, tok, profile)  // match api_type ↔ provider
    if err != nil { return nil, byok.ErrNoCredentialForModel }

    key, err := p.Vault.Decrypt(cred.SecretCipher)           // §3.6
    if err != nil { return nil, err }

    s := profile.Settings.Clone()                            // NEVER mutate shared settings
    s.API.APIKeys[cred.APIType+"-api-key"] = key             // e.g. "claude-api-key"
    scrubServerKeys(s, cred.APIType)                         // drop any YAML/env fallback keys
    return p.Inner.EngineForProfile(ctx, &profiles.ResolvedProfileRuntime{
        RegistrySlug: profile.RegistrySlug, ProfileSlug: profile.ProfileSlug,
        Settings: s, Metadata: profile.Metadata,
    })
}
```

Three subtleties: **clone before mutating** (the resolver may cache/share `Settings` — Geppetto merges into a fresh struct today, but do not depend on that); **fail closed** (no token in context ⇒ error, never fall back to server keys); **scrub fallbacks** so a profile YAML that still contains `${ANTHROPIC_API_KEY}` can never silently subsidize a BYOK caller.

**(d) Metering** — a decorator over the services (or a callback threaded into them), reading `result.Usage` where it is born:

```go
// non-streaming: wrap GeppettoChatCompletionService.Complete (pkg/runtime/chat_service.go:24)
resp, result, err := inner.CompleteWithResult(ctx, req)
u := usageOrZero(result)                              // result.Usage may be nil
store.RecordUsage(ctx, LedgerRow{
    TokenID: tok.ID, Model: req.Model,
    PromptTokens: u.InputTokens, CompletionTokens: u.OutputTokens,
    CachedTokens: u.CacheReadInputTokens,
    Status: statusOf(err),
})                                                    // same tx: counters += totals
```

For **streaming**, the hook lives inside the goroutine at `pkg/runtime/chat_service.go:86-96`: after `RunInferenceWithResult` returns (the stream is complete), `result.Usage` is in hand — record it before emitting `FinalFrame`. Do **not** try to meter from wire chunks: today's chunks carry no usage at all (see §2.2), and the internal result is authoritative anyway. Two consequences worth stating honestly in the UI: budgets are enforced **post-hoc** per request (a single request can overshoot the remaining budget by up to one request's worth; the *next* request is rejected), and a client that disconnects mid-stream still gets metered when the upstream call finishes. Optionally implement `stream_options.include_usage` later so clients can see usage on streams — the plumbing point is `FinalFrame`.

### 3.6 Credential encryption at rest

Model A from the BYOK-BROKER design (server-managed encryption), the recommended starting point:

- A 32-byte master key is supplied via flag/env `--byok-master-key` / `LLM_PROXY_BYOK_MASTER_KEY` (base64). Never stored in the DB. Generated once by `llm-proxy-server byok keygen`.
- Encrypt: AES-256-GCM, fresh random 12-byte nonce per secret, `credential id` as AAD (binds ciphertext to its row — copying a cipher blob to another row fails to decrypt). Stored blob = `nonce ‖ ciphertext`.
- Decrypt only at the moment of engine creation (§3.5c); the plaintext key lives on the stack for one request and is never logged, never audited, never returned by any API. Display uses `secret_last4` only.
- Rotation path: version byte prefix on the blob; a `byok rekey` command decrypts-with-old, encrypts-with-new offline. KMS/HSM custody stays out of scope (it was explicitly deferred in the prior design too).

This fixes prior-work gap #1 (the shipped byok-host schema stored `api_key TEXT` in plaintext — do not copy that column).

### 3.7 Control plane: login, vault, minting

**Login (OIDC RP against Keycloak)** — port the flow from `byok-keycloak-demo/internal/auth/keycloak/{oidc,session}.go`, which is small and correct:

1. `GET /login?return_to=/app` → generate `state` + `nonce` (32 random bytes each), set 10-minute HttpOnly cookies, redirect to Keycloak's authorization endpoint (`oauth2.Config.AuthCodeURL(state, oidc.Nonce(nonce))`, scopes `openid profile email`).
2. Keycloak authenticates the user, redirects to `GET /auth/callback?code&state`.
3. Callback: check `state` against cookie → exchange code → verify ID token (`provider.Verifier`) → check `nonce` → `UpsertUser(sub, username, email)` (auto-provision on first login) → set the session cookie → redirect to sanitized `return_to` (reject absolute and `//` URLs — open-redirect guard).
4. Session cookie: HMAC-SHA256-signed JSON claims (`base64url(payload).base64url(sig)`), HttpOnly, SameSite=Lax, 24 h. Not a JWT; verify with `hmac.Equal`.

Reuse the byok-host Keycloak Compose setup nearly verbatim (`deploy/docker-compose.yaml` + `realm-byok.json`: Keycloak 26.2 on host port 18080, realm `byok`, confidential client `broker-web` — rename to `llm-proxy-web` — and test user `alice`/`password123`).

**Management API** (session-cookie auth; JSON; all scoped to the session's user):

| Method | Path | Body → Response |
|---|---|---|
| GET | `/api/credentials` | → `[{id, provider, api_type, label, secret_last4, disabled, created_at}]` — never the secret |
| POST | `/api/credentials` | `{provider, api_type, label, secret}` → `201 {id, …}`; secret write-only |
| DELETE | `/api/credentials/{id}` | → `204`; revokes tokens bound *only* to this credential (cascade rule from prior art) |
| GET | `/api/tokens` | → `[{id, name, allowed_models, budgets…, usage: {total_tokens, total_requests}, expires_at, revoked_at, last_used_at}]` |
| POST | `/api/tokens` | `{name, credential_ids, allowed_models, max_total_tokens?, max_requests?, rate_limit_rpm?, expires_in_days?}` → `201 {…, token: "llmp_…"}` — **plaintext shown exactly once** |
| POST | `/api/tokens/{id}/revoke` | → `204`; takes effect on the next data-plane request |
| GET | `/api/usage?token_id=&since=` | → ledger rows + per-model aggregates for dashboards |

Minting pseudocode:

```
POST /api/tokens:
  user   = session.user
  creds  = store.GetCredentials(req.credential_ids)  ; assert all owned by user, none disabled
  assert req.allowed_models ≠ ∅
  raw    = "llmp_" + base64url(rand(32 bytes))
  store.MintToken({user_id, token_hash: sha256hex(raw), name, credential_ids,
                   allowed_models, budgets…, expires_at})
  audit("token.minted", {token_id, allowed_models, budgets})   // no secrets in payload
  return 201 {…, token: raw}                                    // never retrievable again
```

**UI pages** (server-rendered templates first, per prior art; a SPA is a later nicety): `/app` dashboard listing credentials and tokens with live usage bars, an add-credential form, a mint-token form (model multi-select fed by the profile list, budget fields), and revoke buttons.

### 3.8 Data-plane API reference (after BYOK)

The surface stays OpenAI-compatible; only auth and filtering change.

- `GET /healthz` — unchanged, unauthenticated.
- `GET /v1/models` — requires `Authorization: Bearer llmp_…`; returns **only** profiles matching the token's `allowed_models`.
- `POST /v1/chat/completions`, `POST /v1/completions` — require bearer; enforce allowlist, rate limit, budgets; metered. Streaming unchanged on the wire.
- New error responses (OpenAI-shaped):

```json
HTTP 401 {"error":{"message":"Invalid API key","type":"invalid_request_error","code":"invalid_api_key"}}
HTTP 403 {"error":{"message":"Model 'gpt-4o' is not allowed for this token","type":"invalid_request_error","param":"model","code":"model_not_allowed"}}
HTTP 429 {"error":{"message":"Token budget exhausted (2000000 tokens)","type":"tokens","code":"budget_exhausted"}}
HTTP 429 {"error":{"message":"Rate limit exceeded (60 rpm)","type":"tokens","code":"rate_limit_exceeded"}}
```

A standard OpenAI SDK pointed at llm-proxy with `api_key="llmp_…"` works unmodified — that is the compatibility bar.

### 3.9 What we deliberately postpone

- **Delegated website OAuth** (consent screens, PKCE, per-site grants) — Phase 4; port `oauth.go`/grant machinery from byok-host when a real third-party integration appears.
- **JWT/introspection tokens**, multi-node data plane, Postgres.
- **Client-side encryption (Model B), KMS custody, provider-issued ephemeral creds (Model C).**
- **Cost-in-dollars budgets** — requires a price table per model; token-count budgets first (the prior design left "currency vs tokens" open; tokens are what we can measure exactly).
- **Prompt privacy features** — restate honestly: the proxy sees traffic.

---

## Part IV — Implementation plan

New code lives in `pkg/byok/` (subpackages `store`, `vault`, `tokens`, `authmw`, `engines`, `meter`, `web`, `oidc`) plus a `byok` command group in `cmd/llm-proxy-server`. Each phase ends green: `make test lint` plus the listed smoke test. Write a diary entry per session (`reference/01-investigation-diary.md`).

**Phase 0 — Store + schema (1–2 days).**
Implement `pkg/byok/store` (interface, SQLite, memory) with the §3.3 DDL; ULID ids; JSON array columns; transactional `RecordUsage` (ledger insert + counter upsert). Port the byok-host SQLite conventions (busy timeout, FK pragma, `ON CONFLICT` upsert for users, transactional consume patterns). Table-driven tests against both backends.

**Phase 1 — Token enforcement, CLI-minted (2–3 days).**
`pkg/byok/tokens` (mint/hash/validate), `pkg/byok/authmw` (§3.5a), scoped `ModelLister`; wire middleware at `cmd/llm-proxy-server/main.go:134` behind new flags `--byok-db` (off ⇒ today's open behavior, loudly logged). CLI: `llm-proxy-server byok token mint --user dev --models 'sonnet,gpt-*' --max-tokens 100000 --expires 30d`, `token list|revoke`, `user add`. Auth error codes per §3.8. Tests follow the `pkg/server/server_test.go` httptest pattern (build `srv`, wrap with middleware, assert 401/403/429 bodies).
*Smoke:* mint a token; `curl` `/v1/models` with/without it; verify filtering and 401.

**Phase 2 — Vault + injection + metering (3–5 days).**
`pkg/byok/vault` (AES-GCM, §3.6) with round-trip and AAD-tamper tests; `byok credential add` CLI; `VaultEngineProvider` (§3.5c) injected into both services in `runServer` (`main.go:127-128`); metering decorator (§3.5d) for `Complete` and `Stream` on both services; budget pre-check in middleware reads `token_counters`.
*Smoke:* two users, two real provider keys; verify each token uses its owner's key (assert upstream key selection via a fake provider first, in the byok-smoke tradition — `provider_key_remained_server_side=true`); run a budgeted token to exhaustion and observe 429; confirm streamed requests land in the ledger.

**Phase 3 — Control plane webapp (5–8 days).**
`pkg/byok/oidc` (port `oidc.go`/`session.go` from byok-keycloak-demo), `deploy/docker-compose.yaml` + realm import (adapted from byok-host), session middleware, `/api/*` handlers (§3.7), server-rendered `/app` UI. CSRF tokens on all mutating form posts; strict `SameSite`; secure-cookie detection via `X-Forwarded-Proto`.
*Smoke:* full browser pass — `docker compose up`, log in as alice, add credential, mint token, run an OpenAI SDK script against `/v1`, watch usage tick on the dashboard, revoke, observe 401. Script it in tmux under `scripts/` (see the byok-host playbooks for the pattern).

**Phase 4 (optional) — Delegated website flow.**
Port the broker-as-authorization-server layer (`/oauth2/auth`, `/oauth2/approve`, `/oauth2/token`, PKCE S256, grants + consent-with-credential-picker) from `byok-web-demo`/`byok-keycloak-demo` `internal/app/oauth.go`; grants then mint ordinary scoped tokens internally, so the whole Phase 1–2 enforcement path is reused unchanged.

---

## Part V — Working reference

### 5.1 Where everything lives

| Thing | Path |
|---|---|
| llm-proxy server wiring, middleware insertion point | `llm-proxy/cmd/llm-proxy-server/main.go` (esp. lines 117–150) |
| Mux, handlers, OpenAI error shape, SSE | `llm-proxy/pkg/server/{server,errors,sse}.go` |
| Profile resolution, `ResolvedProfileRuntime`, API-key location | `llm-proxy/pkg/profiles/resolver.go`; keys in `Settings.API.APIKeys["<apitype>-api-key"]` |
| Engine creation seam (`EngineProvider`) | `llm-proxy/pkg/runtime/engine_provider.go` |
| Services + usage source (`result.Usage`) | `llm-proxy/pkg/runtime/{chat_service,completion_service}.go`; mapping at `pkg/openaichat/mapper.go:220` |
| Example profiles YAML | `llm-proxy/examples/profiles.yaml` |
| Prior threat model + broker design | byok-host `…/BYOK-BROKER…/design-doc/01-delegated-byok-broker-design-and-implementation-guide.md` |
| Prior storage interfaces + SQLite DDL | byok-host `…/BYOK-KEYCLOAK-STORAGE…/scripts/byok-keycloak-demo/internal/storage/` |
| Prior OIDC RP + signed-cookie session | `…/byok-keycloak-demo/internal/auth/keycloak/{oidc,session}.go` |
| Keycloak compose + realm | byok-host `…/BYOK-KEYCLOAK-STORAGE…/deploy/` |
| Prior web UI + consent/PKCE flow | byok-host `…/BYOK-BROKER-WEB-UI…/scripts/byok-web-demo/internal/app/` |
| Token-separation smoke prototype | byok-host `…/BYOK-BROKER…/scripts/byok-smoke/internal/runtime/broker.go` |

(byok-host root: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/2026-04-17--byok-host/ttmp/2026/04/17/`.)

### 5.2 Pitfalls checklist

- **Fail closed.** No token in context ⇒ refuse inference. Never fall back to server-side YAML keys for `/v1/*` when BYOK is enabled; scrub them at injection time.
- **Never compare secrets with `==`.** Hash-then-lookup for tokens; `hmac.Equal` for session signatures. (byok-smoke got this wrong on purpose-built demo code; don't copy it.)
- **Never log or audit plaintext secrets.** Log `secret_last4` / token id only. The audit `payload` column is a secrets-free zone.
- **Clone `InferenceSettings` before mutating** in the engine provider — resolver output may be shared.
- **Streaming usage is internal-only** today: meter from `result.Usage` inside the stream goroutine, not from wire chunks; budgets are post-hoc (documented overshoot of ≤1 request).
- **SQLite:** always `_busy_timeout` + `_foreign_keys` in the DSN; keep ledger insert + counter update in one transaction; single writer means keep transactions short.
- **Cookies:** state/nonce cookies 10-min HttpOnly; session HMAC-signed, SameSite=Lax; open-redirect-guard `return_to`.
- **Credential deletion cascades:** disable/revoke dependent tokens (prior art revoked grants on connection delete — same rule).
- **`usage` may be nil** (`usageFromResult` returns nil when the provider omits it) — record zeros with `status='ok'` and flag it, don't crash and don't skip the request count.

### 5.3 Verifying the whole system (the definition of done)

The end-to-end acceptance test, runnable from a laptop:

1. `docker compose up keycloak` (realm auto-imports; alice/password123).
2. `llm-proxy-server serve --profiles examples/profiles.yaml --byok-db var/byok.db --byok-master-key $KEY`.
3. Browser: login → add Anthropic credential → mint token `demo` with `allowed_models=["sonnet"]`, `max_total_tokens=5000`.
4. `python -c "…openai.OpenAI(base_url='http://127.0.0.1:8080/v1', api_key='llmp_…')…"`:
   - `models.list()` shows only `sonnet`;
   - a chat completion succeeds (upstream billed to the user's own Anthropic account);
   - requesting `gpt-4o-mini` → 403 `model_not_allowed`;
   - after ~5000 tokens of use → 429 `budget_exhausted`;
   - dashboard shows the ledger; revoke → next call 401.
5. `rg -i 'sk-ant|api_key' var/byok.db` finds nothing readable (encrypted at rest), and no secret ever appears in logs.

---

## References

- Ticket landing page: `../index.md`; architecture decision record: `01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md`; running notes: `../reference/01-investigation-diary.md`.
- OAuth for browser-based apps: https://oauth.net/2/browser-based-apps/ and IETF draft-ietf-oauth-browser-based-apps.
- OWASP HTML5 security cheat sheet (browser storage threat background).
- go-go-mcp OAuth/OIDC reference implementation: `/home/manuel/code/wesen/corporate-headquarters/go-go-mcp/pkg/auth/oidc/{server,exports}.go` (PKCE enforcement, introspection, discovery endpoints — relevant for Phase 4).
