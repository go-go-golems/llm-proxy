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

## Step 4: Glazed CLI rewrite + glazed-lint/logcopter enforcement

Per user direction, the entire byok CLI moved off hand-rolled cobra flags and `os.Getenv` onto the Glazed command framework: settings structs with `glazed:` tags, `fields.New` definitions, and glazed's built-in env source so every flag doubles as an `LLM_PROXY_*` environment variable. List commands became `GlazeCommand`s (free `--output json|table|yaml`), mutations became `WriterCommand`s. The repo already had glazed-lint and logcopter targets and CI jobs; the new code now passes both, and the pre-commit hook enforces them locally too.

Two real glazed integration bugs were found and worked around; both are documented below because they will bite anyone extending these commands.

### Prompt Context

**User prompt (verbatim):** "use the glazed command line framework (see skill, see other tools) to do flags and env and all that. no cobra, no flags, no env. (add the glkazed ci lint rules (see ~/code/wesen/go-go-golems/infra-tooling for help, also logcopter and CICD actions and all)"

**Assistant interpretation:** Replace the cobra-flag/os.Getenv byok CLI with Glazed commands (flags AND env via glazed's sources), and make sure glazed-lint, logcopter, and the CI actions from infra-tooling conventions are wired up.

**Inferred user intent:** Keep the repo consistent with go-go-golems conventions: one command framework, uniform env/flag handling, and the standard lint/codegen enforcement in hooks and CI.

**Commit (code):** 1327bef — "byok: rewrite CLI on Glazed with env support; wire glazed-lint/logcopter"; ff91e0b — "ci: enforce glazed-lint and logcopter-check in pre-commit hook"

### What I did
- New `cmd/llm-proxy-server/cmds/byok/` package (root.go, common.go, user.go, token.go, credential.go, keygen.go) following the glazed-command-authoring layout (group folder, verb files, `root.go` registration). Deleted the old `cmd/llm-proxy-server/byok.go`.
- Parser config `cli.CobraParserConfig{ShortHelpSections: [default], AppName: "llm_proxy"}` on every command including serve — this activates glazed's built-in env source with prefix `LLM_PROXY_`.
- Secrets: `credential add` reads the API key from field `byok-secret` (env `LLM_PROXY_BYOK_SECRET` preferred, flag fallback); no `os.Getenv` anywhere in the CLI.
- `sqlite.Open` now rejects empty paths.
- Ran `make logcopter-generate` (generated `logcopter.go` package loggers in all byok packages) and switched authmw/meter/engines from `zerolog/log` imports to the generated loggers; `make glazed-lint` and `make logcopter-check` both pass.
- lefthook pre-commit now runs `glazed-lint` and `logcopter-check` (CI push.yml already did).
- End-to-end CLI smoke, fully env-driven: keygen → user add → credential add (secret via env) → credential list --output json → token mint → token list --output table, with the DB path itself coming from `LLM_PROXY_BYOK_DB`.

### Why
- Single-framework CLIs get env/config/profile support, structured output, and help-system integration for free; hand-rolled cobra flags had already produced one real bug (cobra's `cmd.Printf` writes to stderr).

### What worked
- `--print-parsed-fields` (from glazed's command-settings section) was the killer debugging tool — it shows every field with its source chain (defaults/env/cobra) and immediately exposed both bugs below.

### What didn't work
- **Glazed env prefix keeps hyphens.** With `AppName: "llm-proxy"`, glazed computes the env prefix as `strings.ToUpper(AppName)` = `LLM-PROXY`, so it looked for `LLM-PROXY_BYOK_SECRET` — a name most shells can't even export. Verified with `env 'LLM-PROXY_BYOK_SECRET=x' … --print-parsed-fields` (the value loaded!). Fix: `AppName: "llm_proxy"`. Only the field name is hyphen→underscore normalized (glazed `pkg/cmds/sources/update.go:156-160`), not the prefix — arguably a glazed bug worth an upstream issue.
- **Glazed `DecodeInto` silently skips embedded structs.** My settings structs embedded a shared `commonSettings{DB string}`; `FieldValues.DecodeInto` iterates only top-level tagged fields (glazed `pkg/cmds/fields/initialize-struct.go:146-150`), so `s.DB` stayed `""` — and the empty path made mattn/go-sqlite3 create a database file literally named `?_foreign_keys=on&_busy_timeout=5000` in the repo root. Each command process wrote to its own copy of that file, which is why the smoke *appeared* to work while no real DB existed. Fix: inline the `DB` field in every settings struct (with a comment), plus the empty-path guard in `sqlite.Open`.
- Committing the lefthook edit initially failed because the Write tool requires reading the file first — trivial, but it meant the hook change landed one commit after the code (ff91e0b).

### What I learned
- logcopter's generated `var log = logcopter.Package(...)` collides with a `zerolog/log` import in the same package — after `go generate`, drop the import and use the generated logger.
- The repo's CI (push.yml) already runs `make logcopter-check`, `make glazed-lint`, `go generate` + `git diff --exit-code`, unit tests; lint.yml runs golangci-lint via the shared action; release/codeql/secret-scanning workflows all exist from go-go-golems project setup. Nothing needed to be created from scratch — only the pre-commit hook lacked the two checks.

### What was tricky to build
- **Diagnosing the phantom database.** Symptoms: CLI printed success, listing showed data, yet no `.db` file existed at either the env path or the default. The chain (embedded-struct decode skip → empty path → DSN-as-filename) only became visible by `find`-ing a file named `?_foreign_keys=on&_busy_timeout=5000` in the repo root. Lesson: with layered config frameworks, verify the *effective* value (`--print-parsed-fields`), not the observable side effects.

### What warrants a second pair of eyes
- `AppName = "llm_proxy"` now also applies to the serve command — all serve flags (listen, profiles, byok-db, byok-master-key) are env-settable as `LLM_PROXY_LISTEN` etc. Intended, but it widens the config surface.
- Two glazed upstream issues worth filing: hyphen-in-prefix env keys, and silent embedded-struct skipping in `DecodeInto`.

### What should be done in the future
- File the two glazed issues upstream.
- Consider a `byok usage` GlazeCommand exposing the ledger for dashboards/scripts.

### Code review instructions
- Start: `cmd/llm-proxy-server/cmds/byok/common.go` (AppName rationale, build helper), then any verb file; check `main.go` for the serve-side AppName and `byokcmds.OpenVault`.
- Validate: `make glazed-lint logcopter-check test`; env smoke: `export LLM_PROXY_BYOK_DB=/tmp/t.db LLM_PROXY_BYOK_MASTER_KEY=$(llm-proxy-server byok keygen); llm-proxy-server byok user add --username a; LLM_PROXY_BYOK_SECRET=sk-x llm-proxy-server byok credential add --user a --provider anthropic --api-type claude; llm-proxy-server byok credential list --user a --output json`.

### Technical details
- Env naming rule (glazed): `TOUPPER(AppName) + "_" + TOUPPER(section_prefix + field-name, "-"→"_")` — field `byok-master-key` → `LLM_PROXY_BYOK_MASTER_KEY`.
- Command types: mutations `cmds.WriterCommand` (stdout via `io.Writer`), lists `cmds.GlazeCommand` (rows via `types.NewRow`/`gp.AddRow`).

## Step 5: Phase 3 — control-plane webapp (OIDC login, vault UI, minting)

The user-facing half of BYOK now exists: the same llm-proxy binary serves a control plane next to the data plane. Users log in (OIDC against Keycloak, or a dev-only passwordless route), manage encrypted credentials, mint scoped tokens, watch usage bars fill, and revoke — all backed by the same SQLite store the data plane enforces against. The full loop was verified live: browser dev-login → credential added via API → token minted → that token immediately worked against `/v1/models` → dashboard rendered with the credential, the token, and its 0/5000 usage bar in a real (Playwright-driven) browser.

### Prompt Context

**User prompt (verbatim):** "try playwright again, you should have access now without sandbox" (continuation of Step 1's implementation directive)

**Assistant interpretation:** Continue Phase 3 and verify the webapp in a real browser now that Playwright access works.

**Inferred user intent:** (see Step 1) — plus explicit browser-level verification of the UI.

**Commit (code):** 6b71c01 — "byok: control-plane webapp — OIDC login, credential vault UI, token minting"

### What I did
- `pkg/byok/web/session.go` — HMAC-SHA256-signed session cookie (`payload.sig`, base64url; not a JWT), `Secure` derived from TLS/X-Forwarded-Proto.
- `pkg/byok/web/oidc.go` — OIDC RP ported from the byok-host keycloak demo: go-oidc v3 discovery, state+nonce in 10-min HttpOnly cookies, ID-token + nonce verification, auto-provisioning `UpsertUser`, `sanitizeReturnTo` open-redirect guard; `/dev-login` mounted only when `--byok-dev-user` is set (loudly logged).
- `pkg/byok/web/api.go` — session-authed JSON API: credentials CRUD (secret write-only, `secret_last4` display), token mint/list/revoke (plaintext only in the mint response), usage ledger endpoint with ownership check; same-origin check on all mutations; audit events for every mutation.
- `pkg/byok/web/web.go` + `static/` — server assembly, embedded Bootstrap dashboard (`/app`, `/static/*` per repo web guidelines), mint form with credential multi-select and budget fields, usage progress bars.
- `deploy/docker-compose.yaml` + `deploy/keycloak/realm-byok.json` — Keycloak 26.2 dev setup adapted from byok-host (client renamed `llm-proxy-web`, redirects to :8080, alice/password123).
- `main.go` — flags `byok-session-secret`, `byok-oidc-issuer-url/client-id/client-secret`, `byok-public-url`, `byok-dev-user`; control plane mounts on an outer mux with the token-guarded data plane at `/` (Go 1.22 specific-pattern precedence).
- Tests (`web_test.go`): session round-trip/tamper/wrong-secret, 401 without session, full credential→mint→usage→revoke lifecycle incl. plaintext-leak assertions, mint validation, cross-origin mutation rejection.

### Why
- One binary/one mux keeps deploys trivial and honors the design decision from design-doc/02 §3.1; route precedence gives the control plane specific paths while `/v1/*` stays behind `TokenAuth`.
- `/dev-login` exists because meaningful browser-level verification shouldn't require Docker/Keycloak on every dev loop; it is opt-in and impossible to enable by accident.

### What worked
- The end-to-end loop on the first live run: dev-login 302 → `/api/me` → credential POST → mint POST (plaintext token in response) → `/v1/models` 200 with exactly the allowed model.
- Playwright verification: dashboard renders logged-in state, tables populate from the API, mint form's credential picker fills — only console noise is a favicon 404.

### What didn't work
- First Playwright attempt was blocked by sandbox permissions; the user re-enabled access and the second attempt worked (no code issue).

### What I learned
- Go 1.22 mux precedence makes "control plane wins on specific paths, data plane catch-all at /" composition clean — no path-prefix router needed.
- go-oidc v3 + oauth2 were pulled in as direct deps; everything else was already transitively present.

### What was tricky to build
- **CSRF posture for a cookie-authed JSON API.** SameSite=Lax blocks cross-site POSTs in modern browsers, but non-browser and legacy paths remain; mutations additionally verify the `Origin` header against the request host / configured public URL, while requests without an Origin header (curl, tests, SDKs) pass. This is deliberate: the API is same-origin-only from browsers but scriptable with the session cookie.
- **Plaintext-token discipline across API shapes.** `tokenOut` is reused for list and mint responses; the plaintext lives in an `omitempty` field set exactly once in the mint handler. The lifecycle test asserts the list response never contains the minted string.

### What warrants a second pair of eyes
- The same-origin check trusts `Host` when no public URL is configured — fine behind a sane reverse proxy, but worth review for exotic deployments.
- `/dev-login` gating: flag-only, warning-logged. Consider refusing to start with both `--byok-dev-user` and a non-loopback `--listen`.
- Session cookies are not invalidated server-side (no session store); revocation of a compromised session requires rotating `--byok-session-secret`.

### What should be done in the future
- Keycloak end-to-end pass (docker compose) — the OIDC path is ported from working byok-host code and unit-tested, but hasn't been driven against a live Keycloak in this repo yet.
- Favicon, logout button in the UI, per-model usage aggregates on the dashboard.

### Code review instructions
- Start: `pkg/byok/web/web.go` (assembly + route table), `oidc.go` (callback checks order: state → exchange → verify → nonce), `api.go` (`requireSession`, mint handler).
- Validate: `go test ./pkg/byok/web/ -v`; live: `go run ./cmd/llm-proxy-server serve --profiles examples/profiles.yaml --byok-db /tmp/b.db --byok-master-key $(go run ./cmd/llm-proxy-server byok keygen) --byok-session-secret dev-session-secret-16chars --byok-dev-user alice` then open http://127.0.0.1:8080/app.

### Technical details
- Route table: `GET /login`, `GET /auth/callback`, `POST /logout`, `[GET /dev-login]`, `GET /app`, `GET /static/*`, `GET /api/me`, `GET|POST /api/credentials`, `DELETE /api/credentials/{id}`, `GET|POST /api/tokens`, `POST /api/tokens/{id}/revoke`, `GET /api/usage?token_id=&since=`.
- New deps: `github.com/coreos/go-oidc/v3 v3.19.0`, `golang.org/x/oauth2 v0.36.0` (now direct).

## Step 6: Project report to the vault; React-UI follow-up ticket queued

Two bookkeeping deliverables closed out the day. First, a deep-dive technical report on the whole BYOK implementation was written to the go-go-parc Obsidian vault in textbook style and pushed (`Projects/2026/07/05/PROJ - LLM-Proxy BYOK - Credential Vault, Token Minting, and Metered Proxy Enforcement.md`, vault commit 1dfe403). Second, the user's queued request for a React web UI was captured as ticket **LLM-PROXY-BYOK-REACT-UI** with an expandable analysis/implementation guide.

### Prompt Context

**User prompt (verbatim):** "Make a web UI that uses the react library for /home/manuel/workspaces/2026-07-03/improve-rag-evaluation-system/rag-evaluation-system (as npm package, not sure if it's published already) . This is for later once the core has been done, but remember it by creating a docmgr ticket and writing a small analysis / implementatino guide that canbe expanded later." — and — "Before you continue much further, to keep track of your progress: w write a detailed project report for the obsidian vault as a deep dive technical analysis blog post using a textbook writing style (no analogies, see skill). Commit and push the bsidian vault when done (go-go-parc vault)."

**Assistant interpretation:** (1) Write and push a textbook-style deep-dive project report to the go-go-parc vault. (2) Create a future-work docmgr ticket for rebuilding the BYOK dashboard as a React app on the rag-evaluation-system's React library, with an analysis doc to expand later.

**Inferred user intent:** Preserve the day's work as durable, readable documentation, and make sure the React-UI idea survives as a tracked ticket rather than a chat memory.

**Commit (code):** vault 1dfe403 (go-go-parc); ttmp docs committed below.

### What I did
- Vault note: PROJ-style, textbook prose, mermaid architecture diagram, enforcement-chain walkthrough, the four integration bugs as a failure-modes section, commit table, open questions. Staged only the note; pushed to go-go-parc main.
- Investigated the rag-evaluation-system repo: the React library is `@go-go-golems/rag-evaluation-site` (Widget-IR renderer + site shell; `packages/rag-evaluation-site/src/{widgets,app,cms,context,hooks}`), consumed by `web/` via React 19 + rtk-query + Vite + Tailwind 4. **Verified it IS published on npm: dist-tags.latest = 0.1.16**, matching the workspace version — settling the user's "not sure if it's published" question.
- Created LLM-PROXY-BYOK-REACT-UI with design-doc/01 covering: package inventory, two consumption modes (Widget-IR-first vs component-first, with a recommendation to split by surface type), the exact `/api/*` surface the UI must cover incl. the CSRF/same-origin constraint on dev servers, an integration sketch (llm-proxy `web/` + go:generate embed), a 5-step plan, and open questions. 5 tasks added; doctor passes.

### Why
- The current vanilla-JS dashboard was scoped as a proof; the React rebuild deserves its own ticket so the BYOK core ticket can close cleanly.

### What worked
- `npm view` settled the publication question in one call; the reference consumer in `web/` gave the exact dependency stack to prescribe.

### What didn't work
- docmgr relativized the cross-workspace RelatedFiles path with the wrong depth (`../../../../../../../../2026-07-03/...` → doctor `missing_related_file`). Fixed by switching that entry to an absolute path; doctor green.

### What I learned
- `docmgr doc relate` cross-workspace paths are safer absolute; relative rewriting assumes paths under the repo.

### What was tricky to build
- Nothing structural; this step was investigation and documentation.

### What warrants a second pair of eyes
- The Widget-IR vs component-first recommendation in the React-UI guide is a judgment call to re-validate in the kickoff spike.

### What should be done in the future
- Start LLM-PROXY-BYOK-REACT-UI once the BYOK core settles (spike task first).

### Code review instructions
- Read `ttmp/2026/07/05/LLM-PROXY-BYOK-REACT-UI--…/design-doc/01-….md`; verify the API-surface section against `pkg/byok/web/api.go`.

### Technical details
- npm package: `@go-go-golems/rag-evaluation-site@0.1.16`, public, `sideEffects: ["**/*.css"]`, source `/home/manuel/workspaces/2026-07-03/improve-rag-evaluation-system/rag-evaluation-system/packages/rag-evaluation-site/`.

## Related

- `../design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md`
- byok-host workspace: `/home/manuel/workspaces/2026-07-05/llm-proxy-byok/2026-04-17--byok-host/`

## Step 7: Replace custom fake-engine BYOK test with real Geppetto provider packaging

After Geppetto PR #386 was merged and tagged, the BYOK end-to-end test no longer needed its custom fake `EngineWithResult` path. I replaced that test seam with Geppetto's real OpenAI-compatible engine factory pointed at a local `httptest` provider, so the test now exercises the same provider packaging, outbound URL validation, SSE parser, and usage extraction path that production uses.

The local fake provider remains only an HTTP server implementing the OpenAI-compatible `/v1/chat/completions` streaming wire protocol. It verifies the bearer key that reached the provider, requires `stream_options.include_usage=true`, emits a real SSE text chunk plus a final usage chunk, and then the existing BYOK assertions verify key injection, model rejection, budget crossing, ledger/counters, and revocation.

### Prompt Context

**User prompt (verbatim):** "we bumped geppetto and glazed. What's next?"

**Assistant interpretation:** Now that Geppetto and Glazed dependencies have been bumped, verify that llm-proxy can use the merged Geppetto outbound URL opt-in and finish replacing the custom BYOK fake-engine test with a real Geppetto-backed test.

**Inferred user intent:** Remove the temporary llm-proxy-only testing shortcut and make the BYOK integration test cover the actual Geppetto provider code path before pushing the PR branch.

**Commit (code):** 0210e8f — "test: exercise BYOK through real Geppetto provider path"

### What I did
- Updated `go.mod` from `github.com/go-go-golems/geppetto v0.13.3` to `v0.13.4`, which contains Geppetto PR #386.
- Replaced `pkg/byok/integration_test.go`'s custom `usageEngine` and `keyAssertingProvider` with a real `runtime.FactoryEngineProvider` via `VaultEngineProvider`'s default inner provider.
- Added a local `httptest` OpenAI-compatible provider that:
  - requires `Authorization: Bearer sk-fake-users-own-key`,
  - requires streaming requests and `stream_options.include_usage=true`,
  - emits real SSE chunks, including a final usage chunk with `prompt_tokens=12` and `completion_tokens=7`.
- Kept the existing BYOK acceptance assertions: user key injection, server-side key scrubbing, model allowlist rejection with no provider call, budget crossing and 429 pre-check, ledger/counter math, and immediate revocation.

### Why
- The previous custom fake engine proved BYOK logic but bypassed Geppetto provider packaging. With Geppetto's local-provider URL opt-in available, the test can now cover the real factory-created OpenAI-compatible engine path.

### What worked
- `GOWORK=off go test ./pkg/byok -run TestEndToEndGeppettoKeyInjectionMeteringAndBudget -v` passed after bumping Geppetto to `v0.13.4`.
- `GOWORK=off go test ./pkg/byok/... ./pkg/runtime/...` passed.
- `GOWORK=off go test ./...` passed.

### What didn't work
- Before bumping Geppetto, `GOWORK=off` still used `v0.13.3` and failed to compile because `APISettings.AllowHTTP` and `APISettings.AllowLocalNetworks` did not exist yet:
  - `pkg/byok/integration_test.go:122:8: s.API.AllowHTTP undefined`
  - `pkg/byok/integration_test.go:123:8: s.API.AllowLocalNetworks undefined`
- The fix was to update llm-proxy to `github.com/go-go-golems/geppetto v0.13.4` and run `GOWORK=off go mod tidy`.

### What I learned
- The Geppetto OpenAI-compatible engine always runs through the streaming path and asks for `stream_options.include_usage` unless the model name contains `mistral`; the fake provider intentionally asserts this to keep the BYOK metering path honest.
- A local `httptest` provider is enough to exercise the real Geppetto HTTP/SSE/client code while keeping the test deterministic and CI-safe.

### What was tricky to build
- The main sharp edge was dependency timing. Workspace mode picked up the local Geppetto branch immediately, but `GOWORK=off` still used the released module. Since the llm-proxy PR validates with `GOWORK=off`, the test could only be committed after Geppetto was merged and tagged as `v0.13.4`.

### What warrants a second pair of eyes
- Whether this test is sufficient as the replacement for the planned byok-host-style tmux smoke, or whether a separate operator playbook should still be kept for manual debugging.
- Whether `openai` is the right provider type for the canonical BYOK integration test, or whether a Claude/OpenAI Responses variant should be added later.

### What should be done in the future
- Update the Geppetto follow-up ticket to replace the stale "document in-process fake-engine seam" task with documentation for the Geppetto-backed `httptest` provider seam.
- Continue with `stream_options.include_usage` on the llm-proxy response wire once the core PR lands.

### Code review instructions
- Start at `pkg/byok/integration_test.go`; review `newFakeOpenAIProvider`, `testResolver`, and `TestEndToEndGeppettoKeyInjectionMeteringAndBudget`.
- Validate with:
  - `GOWORK=off go test ./pkg/byok -run TestEndToEndGeppettoKeyInjectionMeteringAndBudget -v`
  - `GOWORK=off go test ./...`

### Technical details
- Required dependency: `github.com/go-go-golems/geppetto v0.13.4`.
- Fake provider endpoint: `/v1/chat/completions`.
- Fake usage: 12 prompt tokens + 7 completion tokens = 19 total per accepted call.

## Step 8: Address BYOK PR code-review findings and GoSec failure

After the Geppetto-backed integration test was pushed, PR #5 still had one failing GitHub check and several automated review comments. I treated the GoSec failure and the P1 data-plane configuration bypass as merge-blocking, then addressed the concrete P2 issues in the same pass because they were small and testable.

The resulting changes keep local development ergonomics while making the production behavior fail closed: BYOK now requires profiles, OpenAI Responses receives the key name Geppetto expects, return redirects are restricted to local paths, and per-token dispatch is serialized so request-count budgets cannot be bypassed by simultaneous requests on a single proxy node.

### Prompt Context

**User prompt (verbatim):** "go ahead. Keep a detailed diary as you work, commit at appropriate intervals"

**Assistant interpretation:** Implement the outstanding llm-proxy PR review items, keep a detailed implementation diary, validate locally, and commit the work in sensible increments.

**Inferred user intent:** Get PR #5 back to a mergeable state by resolving CI/security/code-review feedback while preserving a continuation-friendly record of what changed and why.

**Commit (code):** 3dcff7f — "fix: address BYOK review findings"

### What I did
- Hardened OIDC `return_to` handling in `pkg/byok/web/oidc.go`:
  - accepts only local absolute-path references,
  - rejects absolute URLs,
  - rejects protocol-relative URLs,
  - rejects backslash variants such as `/\\evil.example` that can be normalized by clients/proxies.
- Added `pkg/byok/web/oidc_test.go` coverage for valid local paths and rejected redirect shapes.
- Added targeted GoSec suppressions with rationale for:
  - scheme-derived `Secure` on local-development-compatible session/OIDC cookies (`G124`),
  - the final redirect after `sanitizeReturnTo` (`G710`).
- Rejected `--byok-db` without `--profiles` in `cmd/llm-proxy-server/main.go`, preventing token auth from wrapping static stub services that do not perform credential injection, scoped model enforcement, or metering.
- Added `cmd/llm-proxy-server/main_test.go` asserting that invalid configuration fails before binding a server.
- Added `apiKeyNameForAPIType` in `pkg/byok/engines/provider.go` so `open-responses` and `openai-responses` inject the user's key into `openai-api-key` instead of deriving a literal `open-responses-api-key` slot.
- Added `TestOpenAIResponsesUsesOpenAIKeySlot` in `pkg/byok/engines/provider_test.go`.
- Added per-token dispatch locks in `pkg/byok/authmw/ratelimit.go` and used them in `TokenAuth` so budget preflight and downstream inference/usage recording are serialized per token.
- Added `TestTokenAuthSerializesRequestBudgetThroughDispatch`, which starts two concurrent requests for a token with `max_requests=1` and expects exactly one success plus one `429` budget rejection.

### Why
- GoSec was the only failing GitHub check on PR #5.
- The P1 review identified a real fail-open mode: BYOK auth could be enabled without profiles, leaving requests authenticated but not BYOK-enforced by the runtime services.
- The OpenAI Responses key-slot comment was cheap to address and avoids depending on alias behavior across Geppetto providers.
- Request budgets are meant to be caps; allowing concurrent requests to all pass preflight before usage recording undermines that contract on the single-node proxy.

### What worked
- Focused tests passed:
  - `GOWORK=off go test ./cmd/llm-proxy-server ./pkg/byok/authmw ./pkg/byok/engines ./pkg/byok/web`
- Full tests passed:
  - `GOWORK=off go test ./...`
- Local GoSec with the CI exclusions passed with zero issues:
  - `gosec -exclude=G101,G304,G301,G306,G204 -exclude-dir=.history ./...`

### What didn't work
- The first GoSec rerun still reported `G710` on `http.Redirect(w, r, returnTo, http.StatusFound)` even after strengthening `sanitizeReturnTo`:
  - `pkg/byok/web/oidc.go:194 - G710 (CWE): Open redirect via taint analysis`
- I kept the stricter sanitizer and added a narrowly scoped `#nosec G710` on the redirect with an explanation that `returnTo` is restricted to a local absolute path and falls back to `/app`.

### What I learned
- GoSec's cookie rule does not accept scheme-derived `Secure: isSecureRequest(r)` even when `HttpOnly` and `SameSite` are set; a targeted suppression is needed if local HTTP development must keep working.
- The Geppetto OpenAI Responses provider accepts aliases, but `openai-api-key` is the safest BYOK injection slot because it matches the existing OpenAI profile examples and provider family.

### What was tricky to build
- The budget-concurrency fix needed to preserve existing usage-recording semantics. The store records usage after inference completes, so the simplest correct single-node fix is to hold a per-token lock across `next.ServeHTTP`; this ensures the next request does not read stale counters before the previous request has recorded usage.
- The tradeoff is intentional per-token serialization. Different tokens can still run concurrently, but parallel requests sharing one minted token are serialized through budget preflight and dispatch.

### What warrants a second pair of eyes
- The `#nosec G124` choice keeps HTTP localhost development working. A stricter production-only deployment might instead force `Secure: true` or add explicit trusted-proxy/public-URL cookie policy.
- The per-token lock map currently grows with token IDs seen by the process, matching the existing rate-limiter pattern. If the proxy handles very high churn of one-shot tokens, cleanup or store-backed reservations would be better.
- The dispatch lock is single-process only. Multi-node deployments still need store-backed atomic reservations or distributed locking to enforce request budgets globally.

### What should be done in the future
- Add a hardening follow-up for distributed/multi-node budget reservations if llm-proxy runs horizontally.
- Consider an explicit cookie security mode that forces Secure cookies in production while preserving local HTTP behavior for `--byok-dev-user` demos.

### Code review instructions
- Start with the review-comment files:
  - `cmd/llm-proxy-server/main.go` for the fail-closed BYOK/profile validation.
  - `pkg/byok/web/oidc.go` and `pkg/byok/web/session.go` for redirect/cookie security changes.
  - `pkg/byok/engines/provider.go` for key-slot mapping.
  - `pkg/byok/authmw/middleware.go` and `pkg/byok/authmw/ratelimit.go` for per-token dispatch serialization.
- Review tests alongside each change:
  - `cmd/llm-proxy-server/main_test.go`
  - `pkg/byok/web/oidc_test.go`
  - `pkg/byok/engines/provider_test.go`
  - `pkg/byok/authmw/middleware_test.go`
- Validate with:
  - `GOWORK=off go test ./...`
  - `gosec -exclude=G101,G304,G301,G306,G204 -exclude-dir=.history ./...`

### Technical details
- GoSec result after suppressions: `Issues : 0`, `Nosec : 5`.
- Per-token serialization is implemented as an in-process `TokenLocks` map keyed by token ID.
- OpenAI Responses API types mapped to `openai-api-key`: `open-responses`, `openai-responses`.
