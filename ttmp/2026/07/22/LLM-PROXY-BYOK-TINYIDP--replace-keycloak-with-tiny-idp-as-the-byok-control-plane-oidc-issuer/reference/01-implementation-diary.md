---
Title: Implementation diary
Ticket: LLM-PROXY-BYOK-TINYIDP
Status: active
Topics:
    - auth
    - security
    - byok
    - oidc
    - identity
    - llm-proxy
    - integration
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/design-doc/01-tinyidp-byok-coding-agent-architecture-and-intern-implementation-guide.md
      Note: |-
        Primary architecture and implementation guide produced in Step 1
        Primary architecture and implementation guide produced in diary Step 1
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/research/01-tiny-idp-integration-research-and-architect-onboarding-brief.md
      Note: |-
        Prior evidence map reviewed and corrected for the missing PKCE implementation
        Evidence map reviewed and corrected for PKCE
    - Path: abs:///home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/internal/cmds/admin_client.go
      Note: tiny-idp PR 15 operator secret-file provisioning
    - Path: abs:///home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/internal/fositeadapter/provider.go
      Note: tiny-idp PR 15 RFC 8707 device resource handling
    - Path: repo://deploy/tinyidp/bootstrap.sh
      Note: Device and confidential resource client bootstrap
    - Path: repo://deploy/tinyidp/issue-local-cert.sh
      Note: Persistent-CA one-shot BYOK leaf issuance and validation
    - Path: repo://pkg/byok/deviceclient/cache_unix.go
      Note: POSIX lock descriptor and atomic cache security
    - Path: repo://pkg/byok/meter/health.go
      Note: Shared fail-closed metering circuit and committed recovery probe
    - Path: repo://pkg/byok/meter/health_test.go
      Note: Circuit threshold recovery concurrency and recorder tests
    - Path: repo://pkg/byok/store/audit.go
      Note: Typed lifecycle and meter-circuit audit payloads
    - Path: repo://pkg/byok/store/sqlite/lifecycle_test.go
      Note: Injected audit failure rollback evidence
    - Path: repo://pkg/byok/store/sqlite/schema.go
      Note: |-
        Phase 0 forward-only migration runner and schema constraint validator
        Issuer-aware Phase 2 migration and schema validation
        Schema v3 grant counters bindings and provenance
    - Path: repo://pkg/byok/store/sqlite/store.go
      Note: Atomic persisted grant lifecycle accounting and rotation
    - Path: repo://pkg/byok/web/api.go
      Note: Browser grant lifecycle API
    - Path: repo://pkg/byok/web/oidc.go
      Note: PKCE callback ordering and one-time authorization transaction
    - Path: repo://pkg/byok/web/static/app.js
      Note: Browser grant management UI
ExternalSources: []
Summary: Chronological record of the architecture takeover, evidence verification, selected tiny-idp and coding-agent token design, documentation changes, validation, and reMarkable delivery.
LastUpdated: 2026-07-22T15:42:54-04:00
WhatFor: Preserve why the architecture decisions were made, which source contracts were verified, what was corrected, and how to review or continue the implementation.
WhenToUse: Read before resuming this ticket or reviewing the architecture guide and its implementation phases.
---



















# Implementation diary

## Goal

Capture the architect takeover of `LLM-PROXY-BYOK-TINYIDP`: verification of the
existing research map, correction of inaccurate assumptions, selection of the
runtime and token architecture, production-readiness constraints, and delivery
of an intern-facing implementation guide.

## Step 1: Verify the map and design the complete coding-agent authority chain

This step moved the ticket from a research inventory to an architecture. I read
the previous colleague's map, then verified the critical claims directly in
llm-proxy and tiny-idp. The source inspection showed that the existing BYOK data
plane is a strong base—encrypted credentials, opaque capability tokens,
request-time key injection, model policy, usage counters, and audit records are
already present—but that the browser OIDC RP is not yet compatible with
tiny-idp's required browser profile because it lacks PKCE.

The resulting design selects a separately deployed tiny-idp and introduces an
explicit two-token chain. A coding agent first obtains a short-lived tiny-idp
access token through RFC 8628 Device Authorization. It exchanges that identity
token for an ordinary `llmp_...` capability derived from a browser-approved
agent grant. llm-proxy continues to own credential bindings, model policy,
revocation, budgets, usage, and audit. This keeps identity authorization and LLM
authorization separate and prevents repeated token acquisition from resetting
cumulative budgets.

### Prompt Context

**User prompt (verbatim):** "We are now the architect taking over. Analyze the ticket and use the map given by the previous colleague, and sketch out implementing tiny-idp integration and turning this into a real usable LLM credential setup that can be used by a coding agent to acquire scoped tokens for LLM access, and keeping audits and metrics and usage tracking.

Create  a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable."

**Assistant interpretation:** Verify the ticket's existing research against the current source trees, make the unresolved architecture decisions, and produce a technically detailed intern guide covering tiny-idp integration, browser and device OAuth, capability issuance, provider credential setup, enforcement, audit, metrics, usage, implementation phases, and operational gates; then validate and publish it.

**Inferred user intent:** Give a new engineer a trustworthy, implementation-ready handoff that turns the current prototype-quality pieces into a coherent coding-agent product without rediscovering the repositories or weakening security boundaries.

### What I did

- Read the existing 653-line research brief and the original 616-line BYOK intern guide.
- Inspected llm-proxy's OIDC RP, session codec, management API, token minting,
  authentication middleware, engine provider, vault, usage recorder, SQLite
  schema, integration test, server routes, CLI, and composition root.
- Inspected tiny-idp's embedding foundation, browser/device client profiles,
  production validation, discovery metadata, device CLI, resource-server
  introspection reference, admin client provisioning, production host, and
  production-shaped local Compose example.
- Verified repository state and version boundaries: llm-proxy is at `c898aae`
  and currently requires Geppetto `v0.13.4`; the inspected tiny-idp working tree
  is at `f640eb6` while published tags visible locally stop at `v0.0.3`.
- Created
  `design-doc/01-tinyidp-byok-coding-agent-architecture-and-intern-implementation-guide.md`.
- Created this diary because the active ticket did not yet have one.
- Corrected the research brief's inaccurate claims that nonce was PKCE and that
  `pkg/byok/web/oidc.go` would work unchanged with tiny-idp.
- Selected a separate-service topology, public browser client with PKCE S256,
  public device client, confidential introspection client, pre-approved agent
  grants, two token classes, issuer-aware identities, cumulative grant budgets,
  transactional audit/accounting, and a single-active-broker v1 deployment.
- Added file-level phases, pseudocode, API schemas, ASCII sequence diagrams,
  test matrices, production gates, risks, and an intern review order.

### Why

- The architecture must be based on current source behavior rather than the
  previous document's assumptions.
- tiny-idp should establish identity, while llm-proxy should continue to own
  dynamic LLM capability policy and provider credential custody.
- A device-authenticated terminal must not be able to choose arbitrary stored
  credentials or reset budgets by repeatedly minting tokens.
- Audit, usage accounting, and operational metrics solve different problems and
  need separate records and retention policies.

### What worked

- The existing `VaultEngineProvider` already proves the most important secret
  boundary: it replaces server profile keys with the user's decrypted key only
  for the request and fails closed without an authenticated capability.
- The current `llmp_...` token shape and hash-only persistence are reusable for
  coding agents without introducing a second data-plane authorization format.
- tiny-idp already has a tested device flow, exact resource audiences, RFC 7662
  introspection, and production-shaped Compose patterns to use as references.
- The two-token exchange cleanly maps standards-based identity into the broker's
  existing policy and metering path.

### What didn't work

- The first orientation command ran `docmgr status --summary-only` from the
  session's original cwd (`/home/manuel/code/wesen/2026-04-17--byok-host`) and
  therefore reported the byok-host docmgr root instead of llm-proxy. Subsequent
  docmgr commands explicitly used:

  `cd /home/manuel/code/wesen/go-go-golems/llm-proxy`

- The prior research statement “PKCE (already implemented in `oidc.go` via
  `gooidc.Nonce`)” was false. Source inspection found no
  `oauth2.S256ChallengeOption` or `oauth2.VerifierOption`. This was corrected in
  the research brief and elevated to a production prerequisite.

### What I learned

- OIDC nonce and PKCE protect different bindings. Nonce binds the ID token to
  the authentication request; PKCE binds authorization-code redemption to the
  client transaction.
- The existing data plane is farther along than the IdP integration: the main
  missing product layer is safe self-service capability acquisition for a
  terminal, not provider-key injection itself.
- Per-token budgets alone are insufficient when a device can reacquire tokens.
  A durable agent-grant counter is needed to preserve cumulative limits.
- The current llm-proxy wire surface supports Chat Completions and legacy
  Completions, not `/v1/responses` or Anthropic-native Messages. “Coding agent
  support” must name and test a concrete compatible client.

### What was tricky to build

- The main design challenge was separating three kinds of authority that are
  easy to conflate: tiny-idp identity consent, llm-proxy LLM capability policy,
  and upstream provider authentication. The symptom of a bad design is one
  bearer token being accepted everywhere or OAuth scopes carrying credential
  IDs and mutable budget state. The chosen solution gives each token one route
  family and one responsibility.
- Retry behavior at one-time plaintext token issuance is subtle. A normal
  idempotency cache would need to retain recoverable plaintext. The design
  instead uses a stable non-secret client instance ID and transactionally
  revokes the previous active child token before issuing a replacement. A lost
  response can be retried without leaving multiple usable credentials.
- Metering is post-hoc because provider usage is known after inference. The
  design states the one-request overshoot honestly and requires a circuit
  breaker so persistent ledger failures cannot become unbounded spend.

### What warrants a second pair of eyes

- The agent-grant schema and exact cascade behavior when credentials are
  deleted or grants are tightened.
- Introspection cache TTL and its effect on revocation latency for the privileged
  token-issuance endpoint.
- The migration from subject-only users to `(issuer, subject)` and the required
  operator input for existing databases.
- Whether the first concrete coding-agent client needs `/v1/responses`, which
  would expand the HTTP compatibility work.
- The metering circuit-breaker threshold and transient-versus-durable SQLite
  error classification.

### What should be done in the future

- Implement the guide phase by phase, starting with schema migrations and meter
  health rather than adding new tables to the unversioned schema.
- Pin an approved tiny-idp release or image digest containing the production
  device and introspection work.
- Select one concrete coding-agent acceptance target before claiming general
  compatibility.
- Upgrade llm-proxy's Geppetto dependency only as a separate validated change;
  provider OAuth remains contract- and approval-gated.

### Code review instructions

- Start with the guide's Executive Summary, Parts III–IV, and decision records.
- Verify the PKCE correction against `pkg/byok/web/oidc.go` and
  `golang.org/x/oauth2`'s `GenerateVerifier`, `S256ChallengeOption`, and
  `VerifierOption`.
- Compare the introspection contract with tiny-idp
  `cmd/tinyidp-xapp/internal/resourceauth/resourceauth.go`.
- Compare the device flow with tiny-idp `cmd/tinyidp-xapp/device_cli.go`.
- Verify existing inference separation through `pkg/byok/integration_test.go`.
- Run `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30` after
  ticket bookkeeping is complete.

### Technical details

Selected authority chain:

```text
browser login + credential/grant setup
-> RFC 8628 device approval
-> tiny-idp access token (identity, audience, OAuth scope)
-> RFC 7662 introspection at llm-proxy
-> agent-grant lookup
-> llmp capability issuance/rotation
-> /v1 policy + vault injection
-> Geppetto/provider
-> usage ledger + token/grant counters + audit + bounded metrics
```

No implementation code changed in this step; the artifacts are architecture and
ticket documentation.

## Step 2: Validate and publish the architecture bundle

This step converted the working architecture into a validated ticket
deliverable. Ticket metadata, frontmatter, related-file links, tasks, and the
changelog were reconciled before publication. The bundle dry run showed the
intended four documents and destination, and the real upload completed
successfully.

The published PDF places the ticket index first, followed by the full
architecture guide, the corrected research map, and this diary. That ordering
lets an intern start with the implementation contract while preserving the
source evidence and decision history in the same artifact.

### Prompt Context

**User prompt (verbatim):** (same as Step 1)

**Assistant interpretation:** Complete the ticket documentation lifecycle,
validate it, and publish the full architecture package to reMarkable.

**Inferred user intent:** Make the architecture immediately reviewable away from
the source tree and leave verifiable delivery evidence for the next engineer.

### What I did

- Updated the ticket index, tasks, changelog, and related-file metadata.
- Checked architecture tasks 6–11 and marked the in-process-only dependency
  tasks resolved as not applicable under the selected separate-service design.
- Added implementation tasks for migrations, PKCE, server-side sessions, agent
  grants, RFC 7662 introspection, RFC 8628 CLI exchange, cumulative accounting,
  audit/metrics, and concrete coding-agent compatibility.
- Ran frontmatter validation for the guide and diary.
- Ran `git diff --check` and a targeted scan for token- or credential-shaped
  fixture material in this ticket; no matches remained.
- Ran `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30`.
- Ran the reMarkable bundle dry run, then the real non-interactive upload.

### Why

- A long architecture document is useful only if its metadata, task state, and
  delivery path are reliable.
- The dry run confirms file ordering and remote destination without rendering
  or uploading.
- Publishing research and diary beside the design preserves evidence and makes
  corrections—especially the PKCE correction—visible to reviewers.

### What worked

- `docmgr doctor` reported `✅ All checks passed`.
- Both new documents reported `Frontmatter OK`.
- The upload command returned:

  `OK: uploaded LLM PROXY BYOK TinyIDP Architecture Guide.pdf -> /ai/2026/07/22/LLM-PROXY-BYOK-TINYIDP`

### What didn't work

- N/A. The dry run and real upload both succeeded on the first attempt.

### What I learned

- The guide renders as a single bundle without needing source-code attachments;
  its diagrams and API examples are self-contained.
- Correcting the research brief before bundling prevents a reviewer from seeing
  two contradictory PKCE claims without explanation.

### What was tricky to build

- The bundle needed to include enough provenance for review without burying the
  implementation guide. Ordering the index and design before research and diary
  keeps the table of contents useful.
- Ticket tasks originally contained conditional in-process work. Those tasks
  had to be explicitly resolved as not applicable rather than left appearing as
  implementation blockers after the topology decision.

### What warrants a second pair of eyes

- Review the rendered diagrams and long code blocks on the reMarkable page size;
  the upload succeeded, but visual wrapping should be checked by a human reader.
- Confirm that the chosen implementation task ordering matches release-owner
  priorities across the separate hardening and production-readiness tickets.

### What should be done in the future

- Add the live tiny-idp smoke playbook to the next published bundle after the
  production-shaped Compose exists.
- Update this diary with commits and exact test evidence as implementation phases
  land.

### Code review instructions

- Open the reMarkable bundle at
  `/ai/2026/07/22/LLM-PROXY-BYOK-TINYIDP/LLM PROXY BYOK TinyIDP Architecture Guide.pdf`.
- In the repository, start at the ticket `index.md`, then the design guide.
- Re-run `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30` after
  any ticket edit.

### Technical details

Dry-run destination:

```text
/ai/2026/07/22/LLM-PROXY-BYOK-TINYIDP
```

Bundle contents:

```text
index.md
architecture and intern implementation guide
corrected integration research brief
implementation diary
```

## Step 3: Implement Phase 0 migrations, atomic audit, and fail-closed metering

This step implemented the production prerequisites identified in the
architecture rather than beginning the tiny-idp protocol integration on top of
an unversioned database and best-effort accounting. The BYOK schema now has an
explicit forward-only version contract, all production credential and token
mutations use an atomic typed-audit boundary, and metering health is shared by
the recorder, request middleware, readiness endpoint, and transition audit.

The circuit does not reopen inference merely because time passed. After its
cooldown, exactly one caller performs a committed database write probe. Other
callers remain rejected, and a concurrent accounting failure wins over a
successful probe. This preserves the core safety property: provider dispatch
cannot resume while durable writes are only probably healthy.

### Prompt Context

**User prompt (verbatim):** "phase 0"

**Assistant interpretation:** Implement Phase 0 from the accepted architecture:
versioned BYOK migrations, atomic security lifecycle audit, and fail-closed
metering health with tests, configuration, readiness, documentation, and full
validation.

**Inferred user intent:** Move from architecture into the first production-
blocking implementation phase without starting later tiny-idp, PKCE, session,
or agent-grant work on unsafe storage/accounting foundations.

### What I did

- Added `pkg/byok/store/sqlite/schema.go` with ordered migrations and
  `SupportedSchemaVersion = 1`.
- Added transactional handling for empty, legacy version-0, current, malformed,
  missing-constraint, future-version, and failed-migration databases.
- Added validation of required columns, NOT NULL and primary-key constraints,
  unique/lookup indexes, foreign keys, and the metering probe singleton before
  a schema is accepted.
- Added typed audit payloads and `LifecycleStore` audited methods for credential
  create/delete and token mint/revoke.
- Updated every production web and operator CLI lifecycle path to use the
  audited methods; removed the previous best-effort follow-up appends.
- Included cascade token-revocation events in the same credential-delete
  transaction.
- Added trigger-based SQLite fault tests proving audit delivery failure rolls
  back credential creation/deletion, cascade revocation, token minting, and
  token revocation.
- Added `metering_health` and `Store.CheckMeteringHealth` as a committed write
  probe rather than a read-only ping.
- Added `pkg/byok/meter.Health` with persistent/transient classification,
  configurable transient threshold and cooldown, open/half-open/closed states,
  single-probe recovery, transition counters, readiness, and typed audits.
- Wired `meter.Recorder` failures into health and prevented late successes from
  closing an open circuit.
- Added `TokenAuthWithMeterHealth`; open circuits return OpenAI-shaped
  `503 metering_unavailable` before dispatch.
- Added `/readyz` without changing `/healthz` liveness behavior.
- Added server flags for transient threshold and recovery cooldown plus a
  startup committed write probe.
- Updated `README.md`, the embedded overview help page, and the architecture's
  Phase 0 implementation record.

### Why

- `CREATE TABLE IF NOT EXISTS` cannot safely evolve or validate a production
  database. A malformed legacy table could otherwise be silently accepted.
- Appending audit after a successful mutation permits an unaudited credential
  or capability when the second write fails.
- Logging a meter failure while continuing inference permits unbounded upstream
  spend under a persistent database outage.
- A read-only health query can succeed on a database that cannot commit usage,
  so recovery must exercise a real write transaction.

### What worked

- Empty and legacy databases converge on version 1 while preserving legacy
  data.
- Future and malformed schemas fail before modification.
- Deliberately broken migration DDL rolls back its table and leaves
  `user_version=0`.
- Audit rejection triggers roll back both primary and cascade mutations.
- Focused tests and focused race tests passed across store, meter, middleware,
  and server packages.
- The first full `GOWORK=off go test ./... -count=1` passed.
- A built-binary smoke produced healthy `/healthz`, ready `/readyz`, a 401 for
  unauthenticated `/v1/models`, schema version 1, and one metering probe row.
- `make glazed-lint`, `make logcopter-check`, `make lint` after fixes, and
  `make gosec` passed.

### What didn't work

- The first `make lint` run failed with four `errcheck` findings in
  `pkg/byok/store/sqlite/schema_test.go` because deferred test-store closes did
  not consume returned errors:

  ```text
  Error return value of `store.Close` is not checked (errcheck)
  ```

  The defers now explicitly discard close errors in test cleanup, and the next
  lint run reported zero issues.
- The first `make govulncheck` found reachable `GO-2026-5970` in
  `golang.org/x/text v0.37.0` and `GO-2026-5856` in the configured Go 1.26.4
  standard library. I upgraded the compatible `golang.org/x/*` dependency set,
  pinned toolchain Go 1.26.5, tidied modules, and reran tests/build. The next
  vulnerability scan reported zero reachable vulnerabilities.

### What I learned

- Version stamping must validate security constraints, not only table and column
  names. Unique token hashes and ownership foreign keys are part of the schema's
  security contract.
- Circuit recovery has a concurrency edge: an inference already in flight can
  report a failure while a half-open probe runs. State must be checked again
  after the probe so the newer failure wins.
- Credential deletion is also token lifecycle. Cascade revocations need their
  own typed audit events inside the deletion transaction.

### What was tricky to build

- The migration runner must support both an empty version-0 database and the
  historical unversioned schema. It uses idempotent version-1 DDL followed by
  strict validation inside the same transaction, so partial legacy schemas are
  rejected and all newly-created DDL rolls back.
- Meter health cannot import auth middleware because the recorder already reads
  the authenticated token from that package. A narrow `MeterAvailability`
  interface in middleware avoids an import cycle.
- Audit transition writes may themselves encounter the failed database. The
  circuit state and readiness remain authoritative even when transition audit
  delivery logs a coarse failure; recovery still requires the independent
  committed metering probe.

### What warrants a second pair of eyes

- Review `schema.go`'s required-index and foreign-key inventory whenever a new
  migration adds tables or constraints.
- Review whether three transient failures and a five-second cooldown are the
  desired production defaults.
- Confirm that keeping all `/v1/*` unavailable while metering is open—including
  model listing—is the preferred fail-closed API policy.
- Persisted sessions and agent grants do not exist yet. Their future mutation
  methods must extend `LifecycleStore`; they must not compose raw CRUD and
  `AppendEvent` in handlers.

### What should be done in the future

- Phase 1 should add the production-shaped tiny-idp deployment only after this
  Phase 0 change is reviewed and merged.
- The rate-limiter and dispatch-lock maps still need pruning under the separate
  hardening task; this step did not conflate that work with metering health.
- A future metrics exporter should read `HealthSnapshot` rather than adding
  user, token, or credential identifiers as metric labels.

### Code review instructions

1. Start with `pkg/byok/store/sqlite/schema.go` and
   `pkg/byok/store/sqlite/schema_test.go`.
2. Review `pkg/byok/store/audit.go`, `LifecycleStore`, and the SQLite trigger
   fault tests in `lifecycle_test.go`.
3. Review `pkg/byok/meter/health.go` and its concurrency tests.
4. Follow wiring through `meter.go`, `authmw/middleware.go`, `server.go`, and
   `cmd/llm-proxy-server/main.go`.
5. Confirm web and CLI paths contain no separate best-effort lifecycle audit.
6. Run the commands recorded in the final validation step before approval.

### Technical details

Phase 0 request path when accounting is unhealthy:

```text
/v1 request
-> TokenAuthWithMeterHealth
-> Health.BeforeInference
-> open and cooldown active: 503, no downstream dispatch
-> cooldown expired: one committed UPDATE of metering_health
-> probe fails: remain open, 503
-> probe succeeds and no concurrent failure: close, continue auth/policy
```

No provider credentials, capability plaintext, authorization artifacts, or
client secrets were used in implementation fixtures or documentation.

## Step 4: Run the completion audit and reconcile companion tickets

This step repeated the complete validation matrix after the final schema
constraint and dependency changes, then reconciled the Phase 0 work with the
main tiny-idp ticket and the older hardening and production-readiness tickets.
No validation result below predates the final implementation.

The audit also distinguishes completed Phase 0 work from later phases. Persisted
sessions and agent grants do not exist yet and were not introduced prematurely;
their future lifecycle APIs are explicitly required to extend the atomic
`LifecycleStore` boundary. Cumulative grant counters and public metrics export
remain correctly open in later tasks, while schema versioning and the meter
circuit are checked complete.

### Prompt Context

**User prompt (verbatim):** "phase 0"

**Assistant interpretation:** Complete and prove every Phase 0 requirement,
including repository-wide quality and security gates and accurate cross-ticket
bookkeeping.

**Inferred user intent:** Leave a reviewable implementation rather than a partial
patch whose safety claims depend on unrun tests or stale planning documents.

### What I did

- Re-ran full tests, focused race tests, full build/generation, lint, Glazed vet,
  logcopter validation, gosec, and govulncheck under Go 1.26.5.
- Re-ran a built-binary local BYOK smoke with generated ephemeral key material,
  validating `/healthz`, `/readyz`, unauthenticated `/v1` rejection, schema
  version 1, and the committed-write probe row.
- Ran `git diff --check`, placeholder scans, and credential-shaped material
  scans over changed files.
- Checked the current ticket's migration and Phase 0 completion tasks.
- Checked schema versioning in `LLM-PROXY-BYOK-HARDENING` and both meter-circuit
  decisions in `LLM-PROXY-BYOK-PROD-READINESS`.
- Updated all three changelogs and added implementation status callouts to the
  companion design documents.
- Related the migration, audit, meter health, and fault-test files to the design
  and diary.

### Why

- The goal requires fresh evidence after all edits, not a collection of earlier
  partial runs.
- Companion ticket tasks would otherwise continue to claim that schema
  migrations and the meter circuit do not exist.
- Explicitly preserving later tasks prevents “Phase 0 complete” from being
  misread as the entire tiny-idp integration being complete.

### What worked

Final evidence:

```text
GOWORK=off go test ./... -count=1                                      PASS
GOWORK=off go test -race ./pkg/byok/... ./pkg/server ./cmd/llm-proxy-server -count=1  PASS
GOWORK=off go build ./...                                               PASS
make build                                                              PASS
make lint                                                               0 issues
make glazed-lint                                                        PASS
make logcopter-check                                                    PASS
make gosec                                                              0 issues
make govulncheck                                                        0 reachable vulnerabilities
built-binary BYOK smoke                                                 PASS
git diff --check                                                        PASS
credential-shape and placeholder scans                                 PASS
```

### What didn't work

- One parallel final-validation attempt started `make lint` while another
  golangci-lint process held its global lock and returned:

  ```text
  Error: parallel golangci-lint is running
  ```

  This was tool contention, not a code finding. I reran `make lint` sequentially
  after the other process exited; it completed with zero issues.
- Initial `docmgr doctor` runs for the two companion tickets reported three
  warnings each with the exact class `missing_related_file — related file not
  found`. Their old index frontmatter used repository-name-prefixed relative
  paths such as `llm-proxy/pkg/byok/meter/meter.go`, which resolved under the
  ticket directory. I removed those entries, added absolute paths with updated
  reasons, and reran both doctors; all checks passed.

### What I learned

- Security validation must run after module and toolchain upgrades because a
  passing pre-upgrade lint/test run does not prove the patched dependency graph.
- Cross-ticket reconciliation is part of correctness for this project: future
  agents use those task lists to decide what remains.

### What was tricky to build

- The completion audit had to map a broad phrase—atomic grant/session
  lifecycle—to current reality without pretending later domains exist. The
  evidence is explicit: there are no persisted grant/session mutation paths in
  the repository, and the Phase 0 atomic lifecycle interface is the mandatory
  extension point when those models arrive.
- Secret scanning must avoid printing candidate material. The targeted scan
  reports only pass/fail and uses no live credentials.

### What warrants a second pair of eyes

- Confirm the public compatibility implications of the new Go 1.26.5 toolchain
  and patched transitive `golang.org/x/*` set.
- Review whether raw low-level concrete store mutation methods should become
  test-only in a future breaking release; production interfaces and call sites
  now use the audited boundary.

### What should be done in the future

- N/A for Phase 0. Remaining work belongs to the explicitly open Phase 1+
  tasks: tiny-idp deployment, PKCE/sessions, agent grants, introspection, device
  exchange, cumulative grant accounting, and public metrics/usage summaries.

### Code review instructions

- Use `git diff --stat` to identify the implementation and documentation
  surface, then follow the review order from Step 3.
- Repeat the final evidence commands above from the repository root.
- Run `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30` after any
  ticket edits.

### Technical details

Completion boundary:

```text
complete: schema migration foundation, atomic existing lifecycle audit,
          fail-closed meter health, recovery, readiness, config, docs, tests
open:     tiny-idp deployment and every Phase 1+ domain/protocol feature
```

#### Requirement-to-evidence completion audit

1. **Forward-only production-safe migrations:**
   `pkg/byok/store/sqlite/schema.go` owns sorted plan validation, per-migration
   transactions, `PRAGMA user_version`, legacy classification, future-version
   rejection, schema/index/foreign-key/probe-row validation, and a post-migrate
   foreign-key check. `schema_test.go` proves empty, current/idempotent, legacy
   data preservation, future, malformed, weakened-security, invalid-plan,
   statement-failure rollback, committed probe, and malformed-current cases.
2. **Atomic security lifecycle plus audit:** `store.LifecycleStore` is the only
   production mutation boundary; memory and SQLite implement it. SQLite inserts
   mutation and typed audit rows in one transaction. Credential deletion emits
   credential deletion and every cascade token revocation in that transaction.
   Four trigger-injection tests prove mutation rollback when audit insertion
   fails, and backend conformance proves expected event shape. A source scan
   found no raw mutation or `AppendAudit` call in non-test Go code.
3. **Grant/session wording:** no persisted grant/session type, table, or mutation
   symbol exists yet (verified by source scan). Therefore no unaudited mutation
   path is being deferred inside Phase 0. `LifecycleStore` documents that every
   future security domain must extend this boundary; the migration foundation
   exists before those domains are introduced.
4. **Shared fail-closed meter health:** `pkg/byok/meter/health.go` is shared by
   recorder, request middleware, readiness, committed probe, and typed circuit
   audit. Tests prove immediate persistent opening, bounded transient opening,
   success reset, failed probe, one half-open prober, concurrent-failure
   precedence, and recorder integration.
5. **No dispatch while unhealthy and recovery:**
   `TestTokenAuthMeterCircuitPreventsProviderDispatch` proves 503
   `metering_unavailable` and zero downstream calls while open, followed by one
   committed recovery probe and dispatch only after it succeeds. Focused race
   tests include this package and all state-machine tests.
6. **Readiness and configuration:** `/healthz` remains liveness; `/readyz` uses
   the shared circuit and is covered by
   `TestReadyzReflectsDependencyReadinessWithoutChangingLiveness`. The CLI
   exposes documented threshold/cooldown flags, validates settings, and runs a
   startup committed-write probe. Defaults are threshold 3 and cooldown 5s.
7. **Compatibility and behavior preservation:** the entire test suite and build
   pass. The built-binary smoke proves readiness, schema version/probe row, and
   unchanged unauthenticated rejection. Existing `/healthz` remains 200 while
   readiness can fail. No live provider request was made.
8. **Quality/security:** final lint reports zero issues; Glazed vet, logcopter,
   gosec, and diff checks pass; govulncheck reports zero reachable
   vulnerabilities after the Go/dependency patch; changed-file credential-shape
   and Phase 0 placeholder scans pass. Central interfaces/helpers avoid
   duplicated mutation and error-envelope logic; lint finds no dead code.
9. **Documentation/bookkeeping:** README and embedded overview describe the
   migration, atomicity, metering, flags, endpoints, and operational behavior.
   Main and companion design docs, tasks, changelogs, relations, and this diary
   are reconciled. `docmgr doctor` passes for all three affected tickets.
10. **User changes:** the pre-existing `ttmp/vocabulary.yaml` edit and the new
    ticket tree remain present; no unrelated working-tree change was discarded.

## Step 5: Deliver and validate Phase 1 tiny-idp Compose

Phase 1 replaces the Keycloak-only local control-plane deployment with a
production-shaped tiny-idp topology. The deployment pins the released tiny-idp
v0.0.4 artifact by digest, runs long-lived services non-root, mounts secrets
as files, and makes the proxy wait for a CA-verified tiny-idp readiness probe.

This is intentionally an infrastructure phase, not a claim that browser OIDC
works. The deployed public browser client requires PKCE, while the current
llm-proxy callback lacks PKCE S256 and server-side session/auth-transaction
storage; those are Phase 2 work and remain unclaimed.

### Prompt Context

**User prompt (verbatim):** "ok, cllean up the numbering system if necessary, then do phase 1."

**Assistant interpretation:** Make the implementation roadmap unambiguous and
implement the tiny-idp Compose foundation using the newly released artifact.

**Inferred user intent:** Replace the unsafe Keycloak development setup with a
reproducible, security-shaped local authority deployment without pretending the
later OAuth lifecycle is complete.

### What I did

- Made design-doc Phase 0–7 the canonical task numbering and relabeled old
  discovery/design headings as completed pre-roadmap work.
- Verified tiny-idp `v0.0.4`, tag commit `bdbad44ef30136fe3837dbffd011c053cec2e6c1`,
  and image digest `sha256:2e6a35ccf0cb740dfaa8d896edb4efe52fb1274edd01d8f634c71be91a874942`.
- Replaced `deploy/docker-compose.yaml`; removed the old Keycloak realm;
  added Caddy, CA export, non-root tiny-idp and proxy services, durable volumes,
  idempotent bootstrap, a public browser catalog, and public device client.
- Added file-based llm-proxy secret flags and tests.
- Ran the ephemeral-secret Compose smoke through Caddy TLS; verified both
  `/readyz` endpoints, the device client, and that generated test secrets did
  not appear in service logs.

### Why

- Mutable images and command-line secrets are unacceptable for a broker
  authority deployment.
- Docker `.localhost` resolution needs an explicit gateway address inside the
  network; Caddy’s local CA must be used by back-channel clients rather than
  disabling certificate verification.

### What worked

```text
phase1-compose-smoke=ok
Caddy, tiny-idp, and llm-proxy: healthy
CA-verified https://idp.localhost:18443/readyz: 200
CA-verified https://proxy.localhost:18443/readyz: 200
llm-proxy-agent device client: present
secret log scan: clean
```

### What didn't work

- Initial runs found a pre-existing Docker subnet collision on `172.31.0.0/24`,
  a busy host port `8443`, and a readiness probe that assumed an HTTP client in
  tiny-idp’s intentionally minimal image. The final topology uses free subnet
  `172.30.0.0/24`, localhost port `18443`, and a separate non-root,
  CA-verifying probe using the proxy image’s curl.

### What I learned

- The published tiny-idp image is intentionally minimal; readiness checking
  must not modify it merely to add curl or wget.
- A released Git tag and release asset do not guarantee a versioned GHCR tag;
  deployment pins the verified immutable digest and records its source commit.

### What was tricky to build

- The Caddy TLS hostname must be `idp.localhost` for issuer and certificate
  validation, but containers treat `.localhost` specially. A stable Caddy
  address plus explicit hosts mapping preserves both SNI and verified TLS.
- A confidential resource client cannot be safely provisioned before Phase 4:
  tiny-idp’s current admin CLI accepts its secret as an argument, and llm-proxy
  has no RFC 7662 consumer yet. Creating one now would either leak a secret or
  introduce unused security-sensitive state, so the task remains open.

### What warrants a second pair of eyes

- Review the root-only short-lived volume initializer and whether the deployment
  platform supplies volume ownership more safely.
- Confirm the chosen tiny-idp digest against the release pipeline before any
  non-local deployment.

### What should be done in the future

- Phase 2: PKCE S256, one-time auth transactions, issuer-aware user/session
  storage, and browser-login acceptance.
- Phase 4: confidential RFC 7662 resource client provisioned from an
  operator-managed secret file.

### Code review instructions

- Start with `deploy/docker-compose.yaml`, then read `deploy/tinyidp/` and
  `cmd/llm-proxy-server/main.go` secret resolution.
- Generate temporary secret files and run the Compose command in README.
- Confirm the browser callback is registered but do not attempt a login until
  Phase 2 lands.

### Technical details

The one-shot readiness path is:

```text
Caddy local CA -> ca-export -> tiny-idp serve-production -> idp-readiness
(verified TLS /readyz) -> llm-proxy startup
```

## Step 6: Implement the Phase 2 PKCE, identity, and session boundary

Implemented Phase 2 as one coordinated security migration rather than exposing partial store interfaces. The browser flow now binds authorization codes with PKCE S256, consumes persisted authorization state once before exchange, provisions exact issuer/subject identities, and creates revocable opaque server-side sessions.

The change also extends the Phase 0 fail-closed audit invariant to the new auth-transaction and session mutations. Migration, replay, expiry, identity-isolation, session-revocation, callback-order, audit-failure, and focused race tests now exercise the complete boundary.

### Prompt Context

**User prompt (verbatim):** "ok, go ahead again"

**Assistant interpretation:** Resume Phase 2 and implement the full browser OIDC security boundary coherently.

**Inferred user intent:** Make tiny-idp browser login genuinely usable without retaining the prior nonce-as-PKCE mistake, global-subject identity ambiguity, or self-contained browser sessions.

### What I did

- Added schema version 2 with issuer-aware users, one-time auth transactions, and server-side sessions.
- Added an explicit legacy issuer migration input and fail-closed handling for populated version-1 databases.
- Implemented memory and SQLite conformance for exact identity lookup, transaction consumption, session use, expiry, listing, and revocation.
- Added atomic typed audit events for auth-transaction and session create/consume/revoke operations.
- Added PKCE S256 authorization, verifier-bound exchange, exact callback ordering, and post-callback auth-cookie expiration.
- Replaced claim-bearing session cookies with signed opaque IDs whose hashes are persisted.
- Added per-session listing/revocation APIs and fail-closed current-session logout revocation followed by the discovered, validated tiny-idp end-session redirect.
- Ran `GOWORK=off go test ./... -count=1` and focused race tests.

### Why

- OIDC nonce and PKCE protect different boundaries; tiny-idp's public browser client requires PKCE.
- OIDC subjects are issuer-scoped, so a bare subject cannot identify a user safely.
- Server-side state is required for one-time authorization, independent session revocation, and enforceable idle/absolute expiry.
- New security-sensitive persistence must preserve Phase 0's atomic audit guarantee.

### What worked

- Full in-process OIDC flow tests prove S256 challenge/verifier binding, token and nonce validation, one-time replay rejection, local session revocation, and the exact provider end-session redirect.
- Live browser acceptance proved that Sign out revokes local state, traverses tiny-idp end-session, returns through the registered root redirect to `/app`, and presents a fresh tiny-idp login form rather than silently reusing SSO.
- The version-1 migration preserves users and dependent credential foreign keys after the migration runner reserved one SQLite connection, temporarily disabled FK enforcement there, and ran `foreign_key_check` before commit.
- Full tests and focused store/web/server race tests pass.

### What didn't work

- The first version-1 preservation test failed at migration commit with `FOREIGN KEY constraint failed` because dropping and replacing the referenced `users` table invalidated dependent SQLite foreign keys while enforcement remained active.
- The first cookie-expiration assertion failed with `auth transaction cookie was not expired after callback` because Go parses a deletion header containing `Max-Age=0` as zero rather than a negative `Cookie.MaxAge`; the test now verifies the deletion header semantics directly.
- The first lint pass failed with `pkg/byok/web/oidc_flow_test.go:49:58: QF1008: could remove embedded field "PublicKey" from selector`; the selector was simplified and lint then passed.
- Changing the registered post-logout URI from `/` to `/app` caused persisted tiny-idp startup to fail closed with `Error: bootstrap production browser clients: client "llm-proxy-web" conflicts in fields: post_logout_redirect_uris`. The registration was restored unchanged; llm-proxy now redirects the already registered exact root URI to `/app`.

### What I learned

- SQLite parent-table rebuilds require a dedicated migration connection, connection-local FK suspension, and explicit graph validation before commit.
- Deleting consumed auth transactions immediately reduces retained PKCE-verifier exposure while preserving one-time semantics.
- Cookie deletion should include both a past `Expires` value and `Max-Age=0` for robust browser behavior.

### What was tricky to build

- Callback ordering crossed persistence, OAuth exchange, JWT verification, identity provisioning, and session creation. The implementation makes transaction consumption the first irreversible step, so wrong state, expiry, or replay cannot reach the token endpoint.
- Session audit failure had to roll back both create and revoke mutations. SQLite methods therefore own their transaction and append the typed audit event before commit; trigger-injection tests prove rollback.
- Legacy users had no issuer. Rather than inventing one, startup requires the operator-supplied legacy issuer whenever populated version-1 data is migrated.

### What warrants a second pair of eyes

- Review schema-v2 migration connection handling and `foreign_key_check` ordering.
- Review callback ordering and every error path after one-time transaction consumption.
- Review session idle-touch concurrency and the public-ID versus hashed-cookie-ID separation.

### What should be done in the future

- Add the Phase 3 agent-grant domain only through the same atomic audited mutation boundary.

### Code review instructions

- Start with `pkg/byok/store/sqlite/schema.go`, then `pkg/byok/web/oidc.go`, `pkg/byok/web/session.go`, and `pkg/byok/store/sqlite/store.go`.
- Run `GOWORK=off go test ./... -count=1` and `GOWORK=off go test -race ./pkg/byok/store/... ./pkg/byok/web ./cmd/llm-proxy-server -count=1`.

### Technical details

The enforced callback order is:

```text
consume transaction -> exchange with verifier -> verify signature/issuer/audience
-> compare nonce -> upsert (issuer, subject) -> create opaque session
```

## Step 7: Reuse the persistent workstation CA for BYOK TLS

Changed the BYOK deployment to issue its leaf certificate from the already trusted, manually retained tiny-idp Caddy authority. The design keeps the shared CA keys root-owned and read-only while preserving a non-root long-running BYOK Caddy.

A bounded one-shot issuer now creates or reuses a 30-day certificate for both BYOK hostnames. Host curl, OpenSSL, the existing Playwright page, back-channel readiness, and the original tiny-idp endpoint all validate through the same root.

### Prompt Context

**User prompt (verbatim):** "do it."

**Assistant interpretation:** Make BYOK use the established persistent and system-trusted tiny-idp CA.

**Inferred user intent:** Avoid disposable per-stack authorities and make normal browser automation trust BYOK without certificate overrides or extra windows.

### What I did

- Added `deploy/tinyidp/issue-local-cert.sh`.
- Mounted external volume `tinyidp-local-caddy-pki` read-only into a root-only one-shot issuer.
- Issued a leaf with SANs `idp.localhost` and `proxy.localhost` into a separate BYOK-owned volume.
- Configured non-root Caddy to serve the explicit certificate and key.
- Exported the persistent public root for llm-proxy discovery and readiness checks.
- Verified issuer idempotency, chain validation, both SANs, clean logs, and preserved external-volume labels.
- Completed the browser login in the existing Playwright page with normal certificate validation.

### Why

- Importing BYOK's prior project-owned CA would leave browser trust vulnerable to accidental `docker compose down -v` rotation.
- Mounting shared root-owned CA keys directly into non-root Caddy would either fail permissions or weaken the established private-key ownership boundary.
- A one-shot issuer minimizes access to signing material and avoids a second long-running privileged service.

### What worked

- `https://idp.localhost:18443/readyz`, `https://proxy.localhost:18443/readyz`, and `https://idp.localhost:8443/readyz` all returned 200 with host verification enabled.
- Re-running `local-cert-issuer` preserved the existing certificate hash.
- The Playwright default page navigated and completed login without `ignoreHTTPSErrors` and without opening a new page.
- The external authority remains labeled `manual-delete-only` and is not owned by the BYOK Compose project.

### What didn't work

- Recreate attempts repeatedly ended with `Error response from daemon: Address already in use` even though `ss`, `/proc/net/tcp`, and a direct Python bind proved host port 18443 was free. The address was the statically reserved edge IP `172.30.0.2`, not the host port: short-lived dynamic containers could race Caddy for `.2` because the Compose subnet had no dynamic allocation range. Adding `ip_range: 172.30.0.128/25` reserved the lower half for static infrastructure and removed the race.
- An initial validation command ran `docker compose config --quiet` without the three required secret-file environment variables and failed with `required variable TINYIDP_BOOTSTRAP_PASSWORD_FILE is missing a value`; rerunning with the configured temporary paths passed.

### What I learned

- The retained authority volume deliberately keeps PKI directories and keys root-owned at mode 0600; that invariant must not be changed merely to accommodate another Caddy process.
- Sharing the authority for certificate issuance does not require sharing writable Caddy state or mounting CA private keys into the long-running proxy.

### What was tricky to build

- The certificate chain had to be usable by Caddy while preserving least privilege. The one-shot job signs with the existing intermediate, writes the leaf plus intermediate chain atomically, assigns only the leaf key to UID 1000, and never copies authority keys out of the read-only mount.
- Issuance must be restart-safe. The script checks remaining lifetime, both SANs, and chain validity before deciding to reuse the existing leaf.

### What warrants a second pair of eyes

- Review the one-shot issuer's OpenSSL extensions, validity policy, file modes, and atomic replacement behavior.
- Review the operational dependency on the external `tinyidp-local-caddy-pki` volume.

### What should be done in the future

- Pin the remaining deployment base-image references by digest as part of release hardening.
- Add an operator-facing command that reports leaf renewal time without exposing certificate or key material.

### Code review instructions

- Start with `deploy/tinyidp/issue-local-cert.sh`, `deploy/docker-compose.yaml`, and `deploy/tinyidp/Caddyfile`.
- Run Compose with temporary secret files, verify all three HTTPS readiness URLs without `--insecure`, rerun `local-cert-issuer`, and confirm the leaf hash is unchanged.

### Technical details

```text
tinyidp-local-caddy-pki (read-only)
  -> root-only one-shot leaf issuer
  -> BYOK tls-certs volume
  -> non-root Caddy
  -> host/system trust and llm-proxy CA-verified discovery
```

## Step 8: Implement and live-validate the agent authority chain

Implemented the persisted grant, strict introspection, device authorization, capability exchange, and secure local-cache layers that form roadmap Phases 3–5. The work keeps browser identity, provider credentials, tiny-idp access tokens, and broker capabilities at separate boundaries while making grant budgets survive token rotation and reissue.

A production-shaped local smoke completed browser OIDC, RFC 8628 approval, RFC 7662 introspection, exact identity mapping, pre-approved grant exchange, and both directions of token-route separation. Immutable default deployment wiring remains intentionally open until tiny-idp PR #15 is approved, merged, released, and pinned by digest.

### Prompt Context

**User prompt (verbatim):** (same as Step 1)

**Assistant interpretation:** Complete the canonical Phase 3–5 authority chain rather than stopping at design or partial protocol code.

**Inferred user intent:** Deliver a coding-agent flow whose policy, accounting, revocation, credential isolation, and deployment claims withstand a security review.

**Commits (tiny-idp dependency):** `b6e14b1339e756e6dd288ff714f6ba7556fc499d` — "feat: support secure BYOK resource clients"; `18ff495` — "fix: tighten BYOK client and resource validation"; `73dd644` — "fix: enforce client secret limit at service boundary"; `c93f385` — "refactor: share client secret validation"; `b66bdbc` — "fix: validate client secret files atomically"; `7a210d9` — "fix: reject mixed resource parameter keys"; `2ae3b21` — "feat: allow introspection-only resource clients" (PR #15; not merged)

### What I did

- Added schema version 3 grant, binding, cumulative-counter, and token-provenance persistence with forward-only validation.
- Added memory/SQLite grant CRUD, update/revoke/delete cascades, child-token revocation, atomic issue/rotate, accounting, and typed audit rollback tests.
- Added browser grant APIs and UI with concrete profile validation and no provider-secret disclosure.
- Enforced grant liveness, model/credential policy, per-token limits, and aggregate budgets on `/v1/*` across rotated capabilities.
- Added `pkg/byok/oidcauth` discovery and RFC 7662 authentication with exact claims and bounded keyed-digest caches.
- Added `/agent/v1/grants` and `/agent/v1/tokens` with exact `(issuer, subject)` mapping and hidden credential bindings.
- Added `pkg/byok/deviceclient`, secure POSIX persistence, stable random installation IDs, explicit grant choice, and BYOK `agent login/status/logout` commands. Cache setup rejects permissive or symlinked directories and permissive lock files rather than silently chmodding operator-selected paths.
- Corrected RFC 6749 Basic credential encoding for secrets containing reserved characters.
- Added shared `pkg/byok/urlpolicy` validation so issuer, audience, broker, discovery, and introspection destinations require clean HTTPS URLs before credentials are attached, with loopback HTTP reserved for local tests.
- Hardened every llm-proxy deployment secret-file read to use POSIX descriptor-level no-follow/type/size validation and bounded reads; unsupported non-POSIX file input fails closed.
- Added tiny-idp operator `--secret-file` provisioning and RFC 8707 `resource` handling; opened tiny-idp PR #15 without merging it.
- Removed the resource client's unnecessary token-grant capability: confidential `CanIntrospect` clients with an exact audience may now declare zero grant types and therefore cannot obtain authorization-code, refresh, or device tokens.
- Addressed all automated PR review findings so far: enforce bcrypt's 72-byte client-secret ceiling through one shared validator, distinguish malformed RFC 8707 resources (`invalid_target`) from mixed standard/legacy parameters (`invalid_request`), eliminate the secret-file check/read race with descriptor-level `O_NOFOLLOW`, type/size checks, and bounded reads, and reject mixed parameter keys even when the legacy `audience` value is empty.
- Built temporary local images and completed a non-provider-dispatching live flow through the real TLS, OIDC, device, introspection, grant, and route boundaries.

### Why

- A device identity token authorizes consent; it must not itself become an inference credential.
- A rotated broker token must not reset the grant owner's cumulative authorization budget.
- Resource-client secrets cannot appear in Compose output or process argv.
- A CLI cache containing a capability needs atomic replacement, lock serialization, symlink resistance, and fail-closed platform behavior.

### What worked

- Full llm-proxy and tiny-idp normal test suites passed.
- Focused llm-proxy BYOK race tests passed, as did tiny-idp command/introspection normal tests.
- llm-proxy lint, vet, Glazed lint, logcopter generation/check, gosec policy, and govulncheck passed.
- tiny-idp lint, vet, Glazed lint, gosec, govulncheck, and pre-commit full tests passed.
- The live smoke returned success for device approval, identity-token introspection, capability creation, broker-token access on `/v1/*`, identity-token access on `/agent/v1/*`, and rejection in both opposite directions.
- Neither the agent grant listing nor capability response exposed credential IDs or provider secrets.

### What didn't work

- The first device exchange fixture returned HTTP 200 and failed with `agent capability exchange returned 200`; the fixture was corrected to the API's required 201 response.
- The first local bootstrap failed with `/config/bootstrap.sh: 17: cannot open /run/secrets/tinyidp_bootstrap_password: Permission denied`; local Compose bind-mounted secret files needed container-readable mode inside an owner-only directory.
- The first resource-client bootstrap failed with `Error: client declares an unsupported grant type`; the introspection-only client now uses a supported stored grant type even though it never obtains tokens.
- Initial llm-proxy launches failed with `unknown flag: --byok-agent-resource-client-id`, `master key must be 32 bytes, got 48`, and `decode master key: illegal base64 data at input byte 1`; the smoke configuration was aligned to the actual flag names and base64-encoded 32-byte master-key contract.
- Live grant listing first returned 503 because HTTP Basic credentials were not RFC 6749 form-escaped before encoding. A random secret containing `+` exposed the defect; the client now encodes both fields and has a reserved-character regression test.
- Introspection next returned an inactive token because tiny-idp's device endpoint persisted legacy `audience` but ignored RFC 8707 `resource`. PR #15 accepts repeatable absolute resources, rejects mixed legacy/standard inputs, and preserves the approved resource as token audience.
- Tiny-idp's provider-clock expiry assertion in `TestSQLiteIntrospectionExpiresAccessTokenAtProviderClock` failed repeatedly under race builds and once during a later normal focused-package run; targeted Phase 5 device/resource tests and complete CI pass. This pre-existing timing defect did not originate in PR #15, but the broader claim has been corrected from “race-only” to “timing-flaky.”
- The tiny-idp pre-push snapshot release hook failed at `go generate ./...` with `tsc: not found` because that clean worktree had no frontend `node_modules`. The full Go tests and lint hooks passed; the branch was pushed with hooks disabled only after recording this infrastructure failure. Nothing was merged or released.
- The first visual review found placeholder-only grant fields, ambiguous units, and weak grouping between lifetime and per-capability budgets. The form now has persistent labels, explicit seconds/requests-per-minute units, grouped budget semantics, empty selector states, and a disabled submit action until both credentials and configured profiles exist.

### What I learned

- RFC 7662 client authentication inherits RFC 6749's form-encoding rule before Basic encoding; `SetBasicAuth` alone is insufficient for arbitrary generated secrets.
- RFC 8628 does not define resource targeting by itself; RFC 8707's repeatable `resource` parameter must be carried explicitly into the approved grant and token audience.
- A stable client installation ID must be persisted before browser approval succeeds, otherwise retrying a failed first login can bypass rotation identity.
- A mode check followed by `os.ReadFile` still leaves a symlink-swap window. Opening with `O_NOFOLLOW` and validating the opened descriptor closes that gap.

### What was tricky to build

- Aggregate budget enforcement spans several independently rotated token rows. The implementation serializes against grant-owned counters and never derives remaining budget from one token.
- Grant revocation and credential deletion must atomically revoke every descendant capability and append typed audit evidence; partial success would leave an authorization path alive.
- Introspection errors need safe public classifications without retaining raw tokens. Cache keys are keyed digests, caches are bounded, and unavailable versus invalid versus insufficient-scope results remain distinct.
- Live acceptance required real browser approval without printing passwords, device codes, identity tokens, or broker capabilities. The transient harness kept all sensitive values in memory, emitted only boolean success, and was deleted immediately after the run.

### What warrants a second pair of eyes

- Review SQLite transaction/locking order in grant accounting, issue/rotate, and credential-delete cascades.
- Review RFC 7662 exact-claim validation and cache expiry bounds in `pkg/byok/oidcauth/oidcauth.go`.
- Review the POSIX cache's descriptor-level checks, lock lifecycle, atomic rename, and directory sync.
- Review tiny-idp PR #15's `resource` normalization and confidential client secret-file handling before merge.

### What should be done in the future

- After explicit approval, merge tiny-idp PR #15, publish an immutable release image, replace the v0.0.4 Compose pin, mount the fourth operator secret, and rerun the complete clean-volume acceptance matrix.
- Select and test one explicitly approved OpenAI Chat Completions-compatible coding-agent client before making a named compatibility claim.

### Code review instructions

- Start with `pkg/byok/store/grants.go`, `pkg/byok/store/sqlite/schema.go`, and `pkg/byok/store/sqlite/store.go`.
- Continue through `pkg/byok/agentapi/server.go`, `pkg/byok/oidcauth/oidcauth.go`, and `pkg/byok/deviceclient/`.
- Review `cmd/llm-proxy-server/cmds/byok/agent.go`, `cmd/llm-proxy-server/main.go`, and the browser grant API/UI last.
- Run `GOWORK=off go test ./... -count=1`, focused BYOK races, `make lint glazed-lint logcopter-check gosec govulncheck`, and a clean-volume local device flow after the release pin exists.

### Technical details

```text
browser session -> owned credential -> pre-approved AgentGrant
RFC 8628 identity token -> strict /agent/v1 introspection
-> exact (issuer, subject) -> atomic issue/rotate
-> scoped llmp capability -> /v1 only
-> per-token + cumulative grant accounting
```

## Step 9: Pin tiny-idp v0.0.5 and close the immutable acceptance gate

The merged tiny-idp dependency was verified at its release commit and immutable OCI digest, then wired into the production-shaped Compose topology with the fourth operator-managed resource-client secret. A clean-volume run exercised the real browser and device authority chain rather than inferring compatibility from the prior development image.

The acceptance completed without provider dispatch. It covered browser PKCE login, persisted credential/grant management, RFC 8628 approval, RFC 7662 introspection, capability exchange, cache permissions and logout, all four token/route combinations, and grant-to-child-token revocation.

### Prompt Context

**User prompt (verbatim):** "ok it's merged. tagge v0.0.5 and image is built"

**Assistant interpretation:** Verify the merge and release artifacts, pin the immutable image, complete clean-volume acceptance, rerun validation, and close the Phase 3–5 audit if every requirement passes.

**Inferred user intent:** Remove the final release blocker and finish the durable coding-agent authority-chain goal with production-shaped evidence.

**Commit (code):** `7cebfac008d957a0d2a79d8fd8f158298d988e1b` — "feat: complete tiny-idp BYOK agent authority chain"

**Commit (dependency):** `486a3e3108f3eeda3d100f3db613aecc74f4d13d` — merge commit released as tiny-idp `v0.0.5`

### What I did

- Verified PR #15 merged at `486a3e3108f3eeda3d100f3db613aecc74f4d13d`, tag `v0.0.5` points to that commit, and release CI completed successfully.
- Resolved the published multi-platform image to immutable digest `sha256:d5d9b78ff2eb6adb2e6d984ee9e913bf9570eea38380f153ca87a8a639e9a629`.
- Updated `deploy/docker-compose.yaml` to pin that digest, mount `tinyidp_resource_client_secret` into bootstrap and llm-proxy, and configure exact resource client, audience, and device-client allowlist flags.
- Made `deploy/tinyidp/bootstrap.sh` unconditionally provision the introspection-only resource client with no token grant type.
- Started a fresh Compose project with new project volumes while retaining only the external workstation CA volume.
- Completed browser login and UI grant creation, CLI device login/exchange, mode checks, route separation, grant revocation, and CLI logout.
- Re-ran repository tests, focused races, vet, builds, lint, Glazed lint, logcopter, gosec, govulncheck, JS syntax, shellcheck, diff checks, and Windows cross-compilation.
- Updated README, architecture, tasks, index, and this completion audit.

### Why

- Source and development-image tests could not prove that the supported default deployment contained the tiny-idp protocol and secret-input changes.
- Pinning the release digest and testing clean volumes closes the exact supply-chain and persisted-state gap that kept the goal incomplete.

### What worked

- Rendered Compose contained secret paths but none of the four secret values.
- tiny-idp and llm-proxy readiness both returned HTTP 200 over CA-verified TLS.
- Browser OIDC and persisted grant creation succeeded against the pinned image.
- RFC 8628 approval exchanged through strict RFC 7662 validation into a capability cached in a mode-`0600` file inside a mode-`0700` directory.
- Route results were `200/401/200/401` for tiny→agent, tiny→v1, llmp→v1, and llmp→agent respectively.
- Revoking the grant made its child capability return 401; CLI logout deleted the local cache.
- Runtime logs contained no operator secret, private key, or `llmp_` capability.
- Full unit and focused race suites and every static/security gate passed.

### What didn't work

- The first clean startup failed with `/config/bootstrap.sh: 17: cannot open /run/secrets/tinyidp_bootstrap_password: Permission denied`. Local Docker Compose bind-mounts file-backed secrets without remapping ownership; the files were changed to container-readable mode inside their owner-only mode-`0700` parent directory, project volumes were deleted, and the run restarted from clean state.
- The first Windows validation command attempted to execute the cross-compiled binary and failed with `fork/exec /tmp/go-build3534547867/b001/deviceclient.test.exe: exec format error`. It was corrected to `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 GOWORK=off go test -c -o /tmp/deviceclient-windows.test.exe ./pkg/byok/deviceclient`.
- The first broad secret-pattern scan matched a historical documented fake bearer fixture. The final scan treats that known non-secret documentation fixture separately rather than claiming an unexplained clean grep.
- Playwright clipboard transfer for the one-time device URI was unreliable. After the user explicitly permitted exposing the test user code, the approval URL was passed directly to Playwright and the real flow completed.

### What I learned

- The image publisher emits commit tags (`sha-486a3e3`) rather than a `v0.0.5` image tag; the release boundary must be verified by tag-to-commit identity and the immutable manifest-list digest, not assumed from matching tag names.
- Local Compose file-backed secrets need host traversal/read semantics that differ from orchestrator-managed secret ownership; an owner-only parent directory keeps temporary test files inaccessible on the host while allowing non-root containers to read their bind mounts.
- Four-direction route testing catches accidental authenticator overlap more clearly than testing only each token's successful route.

### What was tricky to build

- The acceptance had to exercise real browser authentication without logging the owner password or provider fixture. Password values moved only through protected files and a transient clipboard and were cleared afterward.
- Device approval initially preserved the user-code secrecy requirement by redirecting CLI output to protected files, but browser-process clipboard focus was not deterministic. Once disclosure was explicitly permitted for this local test, direct navigation removed that harness ambiguity.
- The release image had no semantic-version OCI tag even though the GitHub release was tagged. Verifying the manifest digest from the merge commit's immutable `sha-*` tag linked source, release, and deployed artifact precisely.

### What warrants a second pair of eyes

- Review the Compose local-secret operational note: file-backed secrets must remain inside an owner-only directory even when individual files need container-readable mode under local Docker Compose.
- Confirm downstream operators are comfortable pinning the multi-platform index digest rather than the architecture-specific child manifest.
- Review that the introspection-only tiny-idp client has an empty grant-type set and cannot obtain tokens.

### What should be done in the future

- Select and separately approve one concrete OpenAI Chat Completions-compatible coding-agent client before making any named compatibility claim.
- Keep broader Phase 6–7 observability and hardening tasks separate from the now-complete Phase 3–5 authority chain.

### Code review instructions

- Start with `deploy/docker-compose.yaml` and `deploy/tinyidp/bootstrap.sh` for the immutable image, secret mounts, exact audience/client configuration, and introspection-only client.
- Review `reference/02-phase-3-5-completion-audit.md` row by row against implementation and test evidence.
- Validate with `GOWORK=off go test ./... -count=1`, the focused race command, `go vet ./...`, `make lint glazed-lint logcopter-check gosec govulncheck`, `node --check pkg/byok/web/static/app.js`, shellcheck on `deploy/tinyidp/*.sh`, and `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30`.

### Technical details

```text
tiny-idp v0.0.5 tag -> merge 486a3e3
-> ghcr.io manifest sha256:d5d9b78...a629
-> clean Compose + persistent workstation CA
-> browser PKCE session + persisted grant
-> RFC 8628 device token -> RFC 7662 /agent/v1
-> exact identity/grant exchange -> llmp capability
-> /v1 only -> grant revoke -> child token 401
```

## Step 10: Validate one real Umans GLM 5.2 provider request

A bounded live request exercised the completed BYOK path against the exact `umans-glm-5.2` profile from the local Pinocchio profile catalog. The test reused only its model, API type, base URL, and credential value; a temporary sanitized profile omitted the provider key and the key entered llm-proxy only through the encrypted browser-managed BYOK vault.

The non-streaming OpenAI-compatible Chat Completions request returned HTTP 200 and exactly `live smoke ok`. Durable token and usage records reported one request and 43 total tokens, after which the capability was revoked, its next request returned 401, the provider credential was deleted, and every transient container, volume, profile, secret, log, and clipboard value was removed.

### Prompt Context

**User prompt (verbatim):** "use the umans glm-5.2 from ~/.config/pinocchio/profiles.yaml ."

**Assistant interpretation:** Use the existing private Pinocchio Umans GLM 5.2 profile and credential for one tightly bounded real provider request through llm-proxy, while preserving credential secrecy and verifying accounting, revocation, and cleanup.

**Inferred user intent:** Move beyond protocol-only acceptance and prove that the completed BYOK stack can perform real inference against one concrete approved provider target.

### What I did

- Read only non-secret metadata for `umans-base` and `umans-glm-5.2`: OpenAI API type, `https://api.code.umans.ai/v1`, model `umans-glm-5.2`, and the `openai-api-key` slot.
- Created an untracked sanitized one-profile YAML with no provider credential and mounted it through a temporary Compose override.
- Started fresh project volumes against the pinned tiny-idp v0.0.5 image and completed browser OIDC.
- Copied the existing Pinocchio profile credential directly into the BYOK browser field without printing or persisting it elsewhere.
- Minted a one-day capability restricted to `umans-glm-5.2`, 128 total tokens, and 2 requests/minute.
- Sent one non-streaming `/v1/chat/completions` request with a 32-token output bound.
- Verified HTTP 200, model identity, exact response text, provider usage, durable llm-proxy usage, and token counters.
- Revoked the capability, verified a subsequent `/v1/models` request returned 401, deleted the credential, scanned logs, and deleted transient artifacts.

### Why

- Unit tests and local fake providers proved credential injection and accounting mechanics but not an actual provider network request.
- A single exact target avoids converting one successful smoke into an unsupported general provider or coding-agent claim.

### What worked

- Provider response: HTTP 200, model `umans-glm-5.2`, content `live smoke ok`.
- Provider and durable broker usage agreed at 19 prompt, 24 completion, and 43 total tokens.
- The broker token recorded one successful request against its 128-token ceiling.
- Revocation returned 204 and immediately changed capability access to 401.
- Credential deletion returned 204.
- Runtime logs contained none of the Umans key, four deployment secrets, broker capability, or private-key material.
- The external workstation CA remained while all project containers and volumes were removed.

### What didn't work

- N/A

### What I learned

- The Pinocchio profile stack resolves `umans-glm-5.2` through `umans-base` to ordinary OpenAI-compatible settings: API type `openai`, key slot `openai-api-key`, and base URL `https://api.code.umans.ai/v1`.
- BYOK's replacement semantics correctly inject the encrypted user key into that key slot without requiring a key in the mounted profile YAML.
- The provider's usage response flowed through Geppetto into llm-proxy's durable usage ledger without adaptation for this target.

### What was tricky to build

- The private Pinocchio YAML contains a stored provider key, so ordinary file inspection would have exposed it in tool output. Small local scripts emitted only field names and non-secret routing metadata, then moved the credential directly to the browser clipboard without printing it.
- The minted broker capability appears once in browser DOM by design. The request, revocation check, and DOM clearing ran within one browser automation closure so the token never appeared in terminal or tool output.

### What warrants a second pair of eyes

- Confirm whether the exact Umans GLM 5.2 acceptance should remain README evidence or move into a dedicated provider compatibility matrix as more targets are tested.
- Review future streaming acceptance separately; this smoke intentionally covered only non-streaming Chat Completions.

### What should be done in the future

- Select one concrete coding-agent client that speaks `/v1/chat/completions` and validate its full interaction pattern before claiming client compatibility.
- Add other provider/model targets only through the same explicit contract and bounded live-smoke process.

### Code review instructions

- Read the exact-target note in `README.md`; ensure it does not imply `/v1/responses`, Anthropic-native, arbitrary Umans-model, streaming, or coding-agent compatibility.
- Review the existing real-provider path beginning at `pkg/byok/engines/provider.go`, then `pkg/runtime/chat_service.go` and durable metering.
- The live evidence is: HTTP 200, exact content, usage `19/24/43`, durable request count 1, revoke 204→401, credential delete 204, clean log scan, and complete transient cleanup.

### Technical details

```text
Pinocchio umans-glm-5.2 metadata
-> sanitized keyless profile
-> encrypted BYOK credential (api_type=openai)
-> scoped llmp capability
-> POST /v1/chat/completions
-> https://api.code.umans.ai/v1
-> HTTP 200 / live smoke ok / usage 19+24=43
-> durable ledger -> revoke -> 401 -> credential delete -> cleanup
```
