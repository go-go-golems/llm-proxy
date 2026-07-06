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

## Step 1: Phase 0 — BYOK store layer (SQLite + memory)

Implementation begins. This step lays the persistence foundation from the intern guide §3.3: a pluggable `Store` interface with two backends, covering users, the credential vault (ciphertext-opaque at this layer), hashed minted tokens, the append-only usage ledger with denormalized per-token counters, and audit events. Everything downstream (middleware, vault, metering, webapp) builds on these interfaces.

The design deliberately keeps encryption out of the store: `Credential.SecretCipher` is an opaque `[]byte`, so the store never sees plaintext keys and the vault (Phase 2) owns all crypto.

### Prompt Context

**User prompt (verbatim):** "ipmlement, commit at appropriate intervals, always write to the diary (see skill for format) so that we can keep track of your work and write a killer report afterwards"

**Assistant interpretation:** Implement the BYOK system designed in design-doc/02 (phases 0→3), committing at phase boundaries, and keep a strict-format diary entry per step for a later report.

**Inferred user intent:** Get a working BYOK implementation in llm-proxy with a clean, reviewable history and a documentation trail good enough to produce a project report afterwards.

**Commit (code):** ba0fb4c — "byok: add store layer (SQLite + memory) with vault, tokens, ledger, audit" (docs baseline commit before it: 044368f)

### What I did
- Created `pkg/byok/store/{models.go,store.go}` — domain structs (`User`, `Credential`, `Token`, `LedgerEntry`, `Counters`, `AuditEvent`) and the `Store` interface composed of `UserStore + CredentialStore + TokenStore + MeterStore + AuditStore`.
- Created `pkg/byok/store/sqlite/store.go` — full DDL from the guide (users, credentials with `secret_cipher BLOB`, tokens with `token_hash UNIQUE` and nullable budget columns, `usage_ledger`, `token_counters`, `audit_events`), DSN `?_foreign_keys=on&_busy_timeout=5000`, JSON-encoded string-array columns.
- Created `pkg/byok/store/memory/store.go` — mutex-guarded map implementation for tests/dev.
- Created `pkg/byok/store/conformance_test.go` — 5 test functions, each run against both backends via a `backends(t)` table.
- `go mod tidy` (promotes `mattn/go-sqlite3` to a direct dependency), `go test ./pkg/byok/...`, commit (lefthook runs full `GOWORK=off go test ./...` + `golangci-lint` pre-commit: 0 issues).

### Why
- The store is the dependency of every other BYOK component; getting the transactional semantics (ledger+counters, cascade revocation) right first means the enforcement layers can stay thin.
- Two backends from day one keeps unit tests fast and honest (conformance suite catches semantic drift between them).

### What worked
- Both backends pass the conformance suite on first run after `go mod tidy`.
- Reusing byok-host SQLite conventions (busy timeout, FK pragma, `ON CONFLICT(oidc_subject) DO UPDATE` upsert, JSON array columns) transplanted cleanly.

### What didn't work
- Editor LSP reports `go.work requires go >= 1.26.3 (running go 1.25.5)` on every file — stale gopls toolchain, not a build problem; shell `go build`/`go test` (go 1.26.4 toolchain) are green. Ignored.

### What I learned
- The repo's lefthook pre-commit already runs the full test suite and golangci-lint with `GOWORK=off`, so every commit is a verified checkpoint — no need for separate CI-style validation runs between phases.

### What was tricky to build
- **Credential-delete cascade semantics.** Deleting a vault entry must revoke tokens whose *only* binding was that credential, while tokens bound to several credentials survive. Since `credential_ids` is a JSON column, this can't be a pure-SQL update: the SQLite implementation loads the user's live tokens inside the delete transaction, decodes bindings in Go, and revokes the now-orphaned ones — all-or-nothing with the row delete.
- **Rejected requests and budgets.** A rejected request must appear in the ledger (auditability) but must not burn `max_requests`/token budget. Solved by branching on `status != 'rejected'` for the counter upsert, inside the same transaction as the ledger insert; conformance test pins this (170 tokens / 2 requests after ok+error+rejected rows).

### What warrants a second pair of eyes
- The counter upsert `ON CONFLICT(token_id) DO UPDATE SET total_tokens = total_tokens + excluded.total_tokens, total_requests = total_requests + 1` — correctness under concurrent writers relies on SQLite's serialized writes; if we move to Postgres the same statement is safe but should be re-reviewed.
- `GetUserByUsername` picks the oldest match (`ORDER BY created_at LIMIT 1`); usernames are not unique in the schema (OIDC subject is the identity). Fine for CLI convenience, but worth confirming.

### What should be done in the future
- Add a `schema_version` pragma/table before the first real deployment so migrations are possible.

### Code review instructions
- Start with `pkg/byok/store/store.go` (interfaces) then `sqlite/store.go` (`ensureSchema`, `DeleteCredential`, `RecordUsage`).
- Validate: `go test ./pkg/byok/store/ -count=1 -v`.

### Technical details
- Nullable budgets map to `sql.NullInt64`/`sql.NullTime` via tiny helpers; `Counters` for an unknown token returns zeros (not `ErrNotFound`) so the middleware pre-check needs no special casing.
- IDs are 16-byte crypto/rand hex (`store.NewID()`); no new dependencies beyond promoting `mattn/go-sqlite3` to direct.

## Step 2: Phase 1 — token enforcement on the data plane

llm-proxy's `/v1/*` endpoints are now gated by minted tokens. This step delivered the whole "who may call, how often, within what budget" layer without touching credentials or inference: `llmp_` tokens hashed with SHA-256, a `TokenAuth` middleware doing validity → rate-limit → budget pre-checks, a scoped `/v1/models` lister, OpenAI-shaped error surfacing (401/403/429) via a structural interface in `pkg/server`, and a `byok` CLI to manage users and tokens before any UI exists.

A live tmux smoke test confirmed the boundary: no token → 401 `missing_api_key`, wrong token → 401 `invalid_api_key`, valid token → `/v1/models` returns exactly the allowlisted profile (1 of 3 configured).

### Prompt Context

**User prompt (verbatim):** (see Step 1)

**Assistant interpretation:** Continue with Phase 1 of the implementation plan.

**Inferred user intent:** (see Step 1)

**Commit (code):** e6b3b1f — "byok: enforce minted tokens on the data plane"

### What I did
- `pkg/byok/tokens/tokens.go` — `Mint()` (llmp_ + 32 rand bytes base64url), `Hash()` (SHA-256 hex).
- `pkg/byok/apierr/apierr.go` — status-carrying OpenAI-shaped errors (`missing_api_key`, `invalid_api_key`, `token_revoked`, `token_expired`, `model_not_allowed`, `no_credential_for_model`, `budget_exhausted`, `rate_limit_exceeded`).
- `pkg/byok/policy/policy.go` — pure helpers: `ModelAllowed` (exact or `path.Match` glob), `CheckTokenUsable`, `CheckBudgets`.
- `pkg/byok/authmw/` — `TokenAuth` middleware (context.go/ratelimit.go/middleware.go), `ScopedModelLister` (models.go), 6 httptest-based tests.
- `pkg/server/errors.go` — new `httpAPIError` structural interface so wrapped byok errors override status/type/code/param; header write moved after status resolution.
- `cmd/llm-proxy-server/byok.go` — cobra `byok` group: `user add|list`, `token mint|list|revoke` (plaintext printed exactly once, mint/revoke audited).
- `cmd/llm-proxy-server/main.go` — `--byok-db` flag; wraps handler with `TokenAuth` and lister with `ScopedModelLister`; loud warning when BYOK is off.
- Smoke: `byok user add` + `token mint --models sonnet`, server in tmux on :8099, curl matrix (401/401/200-filtered/healthz-open).

### Why
- Enforcement before credentials: this ordering lets everything be tested with the existing YAML keys, and Phase 2 only has to swap *which* key the engine uses.
- Structural error interface keeps `pkg/server` free of byok imports (no dependency cycle, no coupling).

### What worked
- The full curl matrix behaved exactly per design-doc §3.8 on the first server run.
- Model filtering verified against a 3-profile YAML: only the allowlisted `sonnet` is visible.

### What didn't work
- First commit attempt failed lint: `nonamedreturns` linter rejected `func Mint() (raw string, hash string, err error)` and a named-return test helper (`golangci-lint` 2.4.0, repo config). Fixed by switching to plain return signatures. Exact error: `pkg/byok/tokens/tokens.go:20:1: named return "raw" with type "string" found (nonamedreturns)`.

### What I learned
- The repo lint config forbids named returns entirely — write plain signatures from the start.
- `http.MaxBytesReader` and mux patterns need no changes for middleware wrapping; Go 1.22 method patterns compose cleanly with a plain `http.Handler` wrapper.

### What was tricky to build
- **Ordering of checks in the middleware.** Rate-limit must run before the budget read (cheap in-memory vs DB read), but after token validity so scanners can't spin the limiter for other tokens; and `TouchTokenUsed` must only fire for accepted requests, otherwise a rejected-loop keeps `last_used_at` fresh and misleads "is this token dead?" dashboards.
- **Rejection auditing without a model name.** Middleware rejections happen before the body is parsed, so no model is known; ledger rows require a model. Solution: middleware writes `inference.rejected` audit events (token/user/code) instead of ledger rows; ledger `rejected` rows are reserved for post-parse policy failures (Phase 2's engine provider).

### What warrants a second pair of eyes
- The fixed-window rate limiter allows up to 2× rpm across a window boundary (classic fixed-window artifact) and its per-token map is never pruned (unbounded growth only if millions of distinct tokens hit one process). Both acceptable for v1, both worth a comment in review.
- `writeAPIError` in authmw duplicates the OpenAI envelope of `pkg/server` (which is unexported). If the envelope ever changes, both places must change.

### What should be done in the future
- Prune stale rate-limiter windows (periodic sweep) if token cardinality grows.
- Consider exporting a shared error-envelope writer from `pkg/server`.

### Code review instructions
- Start: `pkg/byok/authmw/middleware.go` (check ordering), `pkg/server/errors.go` (interface override), `cmd/llm-proxy-server/main.go` (wiring).
- Validate: `go test ./pkg/byok/authmw/ -v -count=1`; live: `go run ./cmd/llm-proxy-server byok user add --db /tmp/b.db --username alice && go run ./cmd/llm-proxy-server byok token mint --db /tmp/b.db --user alice --name t --models sonnet` then serve with `--byok-db /tmp/b.db` and curl `/v1/models` with/without the token.

### Technical details
- Token format: `llmp_` + base64url(32 bytes crypto/rand), 48 chars total; storage `hex(SHA-256(raw))`, unique-indexed.
- Budget pre-check reads `token_counters` only (O(1)); authoritative accounting lands in Phase 2.

## Step 3: Phase 2 — vault, per-user key injection, and metering

The BYOK promise is now real: a caller's minted token causes inference to run with *their* stored, encrypted provider key — never the server's YAML keys — and every request is metered into the ledger with authoritative provider usage. The three pieces: an AES-256-GCM vault, a `VaultEngineProvider` wrapping the Geppetto engine factory, and a `UsageRecorder` hook threaded into both runtime services.

An in-process integration test (`pkg/byok/integration_test.go`) replaces the originally planned fake-provider tmux smoke: it drives real HTTP handlers through middleware → service → fake `EngineWithResult` engine and asserts the key boundary, wire usage, model 403, budget-crossing 429, ledger/counter math, and instant revocation — all CI-runnable.

### Prompt Context

**User prompt (verbatim):** (see Step 1)

**Assistant interpretation:** Continue with Phase 2: credential vault, per-request key injection, budget metering.

**Inferred user intent:** (see Step 1)

**Commit (code):** 388cb6d — "byok: per-user credential vault, key injection, and usage metering"

### What I did
- `pkg/byok/vault/` — AES-256-GCM; blob = version byte ‖ nonce ‖ ciphertext; credential ID as AAD; `keygen` helper; 5 tests incl. AAD-tamper and wrong-key.
- `pkg/byok/engines/provider.go` — `VaultEngineProvider`: token from context (fail closed), model allowlist check (rejections ledgered with `status=rejected`), credential selection by `api_type`, decrypt, `Settings.Clone()`, replace `API.APIKeys` with exactly one entry, scrub `Chat.APIKeys`.
- `pkg/runtime/usage.go` + edits to `chat_service.go`/`completion_service.go` — optional `Usage UsageRecorder` field; called with `result.Usage` after `RunInferenceWithResult` in all four paths (complete/stream × chat/completions).
- `pkg/byok/meter/meter.go` — `Recorder` implementing `runtime.UsageRecorder`; records via `context.WithoutCancel` so client disconnects mid-stream still meter.
- CLI: `byok keygen`, `byok credential add|list|rm` (secret read from an env var via `--secret-env`, never argv; encrypted before storage; `secret_last4` display only).
- `main.go`: `--byok-master-key` (or `LLM_PROXY_BYOK_MASTER_KEY`); when `--byok-db` is set the master key is now REQUIRED and services get `VaultEngineProvider` + `meter.Recorder`.
- Tests: `pkg/byok/engines/provider_test.go` (6 tests incl. no-mutation and scrubbing), `pkg/byok/integration_test.go` (full stack).

### Why
- AAD-binding to the credential ID means a cipher blob copied between rows is useless — cheap misuse resistance.
- Metering must live where `result.Usage` is born: streaming responses carry no usage on the wire (verified: `ChatCompletionChunk` has no Usage field), so a response-level decorator could never meter streams.

### What worked
- `InferenceSettings.Clone()` exists in geppetto (settings-inference.go:334) — no hand-rolled deep copy needed.
- The `EngineWithResult` optional interface (run_with_result.go:28) let the fake engine return usage directly, making the integration test exact: 12+7 tokens per call, budget 30, third call 429.

### What didn't work
- **Planned tmux smoke against a Python fake provider failed**: geppetto hard-codes `security.ValidateOutboundURL(..., AllowHTTP: false)` in every provider path (e.g. `geppetto/pkg/steps/ai/openai/chat_stream.go:68`), so an `http://127.0.0.1` base URL is rejected with `invalid chat completion URL: http scheme is not allowed`. No env/flag escape hatch exists. Pivoted to the in-process integration test, which is strictly better for CI. (The 403 model-scope check DID pass in that live run before the pivot.)
- **cobra output gotcha burned 10 minutes**: `cmd.Printf` writes to *stderr* by default (`OutOrStderr`), so `KEY=$(... 2>/dev/null)` captured an empty master key and `credential add` failed with "no master key". Diagnosed by re-running keygen without redirection.

### What I learned
- Geppetto's SSRF guard (pkg/security/outbound_url.go) blocks http and local networks unconditionally from providers — any future local-provider testing needs either an https test server with a trusted cert or an engine-level fake.
- `turns.InferenceUsage` uses `int` fields and includes cache-token counts; we fold `CachedTokens + CacheReadInputTokens` into the ledger's `cached_tokens`.

### What was tricky to build
- **Where to meter streaming.** The stream goroutine owns the only reference to `result` after the SSE channel is closed; recording had to happen inside `chat_service.go`'s goroutine after `RunInferenceWithResult` returns but before `FinalFrame`, and with `context.WithoutCancel` because the request context is frequently already canceled when a client disconnects mid-stream — otherwise the ledger write itself would be canceled and usage silently lost.
- **Key scrubbing semantics.** Replacing the whole `APIKeys` map (not just overwriting one entry) is what guarantees a profile with `${OPENAI_API_KEY}` can't subsidize a claude-scoped token that resolves an openai profile through a glob. The integration test pins `len(keys) == 1`.

### What warrants a second pair of eyes
- The `--byok-db`-implies-master-key decision: enabling BYOK now hard-requires the vault key at startup. Intentional fail-closed behavior, but it means Phase 1-style "tokens only, YAML keys" deployments no longer exist.
- `meter.Recorder` swallows store errors (logs only). If ledger writes fail persistently, budgets stop advancing — should this eventually trip a circuit breaker that 503s the data plane?

### What should be done in the future
- `stream_options.include_usage` support so streaming clients can see usage (plumbing point: `FinalFrame`).
- `byok rekey` command for master-key rotation (blob version byte is already in place).

### Code review instructions
- Start: `pkg/byok/engines/provider.go` (clone/scrub/fail-closed), `pkg/byok/meter/meter.go` (WithoutCancel), runtime service diffs (4 call sites).
- Validate: `go test ./pkg/byok/... ./pkg/runtime/... -count=1`; the single most informative test is `go test ./pkg/byok/ -run TestEndToEnd -v`.

### Technical details
- Vault blob: `0x01 ‖ nonce(12) ‖ AES-256-GCM(secret, AAD=credentialID)`; master key base64 via `byok keygen`.
- Credential selection: first enabled credential on the token whose `api_type` equals the profile's `Chat.ApiType`; key name written as `<api_type>-api-key` (matches geppetto factory lookup `factory.go:206`).

## Related

- `../design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md`
- byok-host workspace: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/2026-04-17--byok-host/`
