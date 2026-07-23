---
Title: Investigation Diary
Ticket: LLM-PROXY-BYOK-OBSERVABILITY
Status: active
Topics:
    - byok
    - security
    - llm-proxy
    - integration
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/byok/meter/health.go
      Note: |-
        Established the first bounded metrics source
        Investigation source for bounded health metrics
    - Path: repo://pkg/byok/meter/meter.go
      Note: |-
        Established the authoritative completed-inference seam
        Investigation source for provider usage semantics
    - Path: repo://pkg/byok/store/sqlite/schema.go
      Note: Investigation source for ledger indexes and migration needs
    - Path: repo://pkg/byok/store/sqlite/store.go
      Note: Established durable summary and indexing constraints
    - Path: repo://pkg/byok/web/api.go
      Note: Established owner-scoped API and current raw-ledger behavior
    - Path: repo://pkg/byok/web/static/app.js
      Note: Investigation source for current usage UI
ExternalSources:
    - https://prometheus.io/docs/practices/naming/
    - https://prometheus.io/docs/practices/instrumentation/
Summary: Chronological evidence and decisions behind the phased BYOK operational metrics and owner usage-summary design.
LastUpdated: 2026-07-23T16:45:00-04:00
WhatFor: Preserve how the observability design was derived, what was inspected, what was deliberately deferred, and how a future intern should resume.
WhenToUse: Read before implementing any phase or changing the design's cardinality, privacy, listener, or persistence decisions.
---


# Investigation Diary

## Goal

Design incremental operational metrics and usage summaries for llm-proxy BYOK.
The work must produce useful MVPs without forcing a complete monitoring stack,
and it must preserve credential secrecy, owner isolation, durable accounting,
and bounded metric cardinality.

## Step 1: Create the focused follow-up ticket and map current behavior

This step separated observability from the completed tiny-idp authority-chain
ticket. The investigation began from implementation evidence rather than the
older roadmap sketch: the ledger, counters, health circuit, browser API, runtime
composition, and live-test findings are all merged and now define the baseline.

The source map showed that durable accounting is already coherent. The missing
work is read-side aggregation and safe exposition, not a rewrite of metering or
a new event pipeline.

### Prompt Context

**User prompt (verbatim):** "Create a new ticket for that and Create  a detailed analysis / design / implementation guide that is for a new intern, explaining all the parts of the system needed to understand what it is, with prose paragraphs and bullet point sand pseudocode and diagrams and api references and file references. It should be very clear and technical. Store in the ticket and the nupload to remarkable.

We'll do it later. Separate in incremental phases in complexity so that we can do MVPs without going full in"

**Assistant interpretation:** Create a durable docmgr ticket for BYOK observability, research the current code, write a technically detailed intern-ready architecture and phased implementation guide with APIs/diagrams/pseudocode/file evidence, preserve a chronological diary, and upload the bundle to reMarkable for later implementation.

**Inferred user intent:** Park a trustworthy, implementation-ready observability plan that can be resumed later and delivered through small valuable increments instead of a large monitoring-platform project.

### What I did

- Created ticket `LLM-PROXY-BYOK-OBSERVABILITY` with a design doc and diary.
- Searched the repository for usage recording, counters, audit events, browser usage APIs, health/readiness, runtime wiring, metrics dependencies, and deployment constraints.
- Read the complete metering recorder and health circuit implementation.
- Read ledger/counter/audit domain types and store interfaces.
- Read SQLite `RecordUsage`, schema indexes, browser token/usage API, UI rendering, runtime observer seam, middleware rejection flow, and server composition.
- Re-read the prior tiny-idp architecture's metrics section and its single-active-broker constraint.
- Consulted official Prometheus naming, instrumentation, Go-client, and OpenMetrics references.

### Why

- Metrics must be derived from actual semantic boundaries; adding counters at
  convenient call sites risks double counting and leaks.
- The user explicitly requested phased MVPs, so the plan needed independently
  releasable stopping points.

### What worked

- `meter.Health.Snapshot()` already exposes bounded process state and counters,
  making it an ideal first collector.
- `runtime.UsageRecorder` is a provider-neutral completed-inference seam used by
  both streaming and non-streaming paths.
- SQLite already commits ledger, token counters, and grant counters atomically.
- The current browser API proves the owner/session boundary and supplies a
  natural home for an aggregate endpoint.
- Existing process-local grant/token locks explain why the current singleton can
  serialize budget checks while distributed coordination remains out of scope.

### What didn't work

- No production metrics endpoint, registry, or direct Prometheus dependency
  exists to extend.
- The current ledger index is `(token_id, created_at)`, so it does not efficiently
  support owner-wide time-window summaries.
- The current `/api/usage` implementation lists all owned tokens to prove one
  token's ownership and cannot answer cross-token questions.
- The initial broad search found OpenTelemetry only as indirect dependencies;
  it did not reveal an existing project-level OTel provider/export lifecycle.

### What I learned

- “Usage summaries” and “operational metrics” are separate products with
  different security, retention, and cardinality contracts.
- Audit events are the existing source for pre-dispatch rejection decisions;
  successful/error provider attempts populate the usage ledger.
- Cached tokens are stored separately from prompt/completion tokens and are not
  currently added to budget totals, so summaries must not silently redefine
  total-token semantics.
- Profile/model labels are useful but require a startup allowlist/cap because
  arbitrary labels violate cardinality guarantees.

### What was tricky to build

- There are several plausible observation points. Middleware sees rejection but
  not provider usage; runtime sees provider completion but not every policy
  rejection; the store sees durable writes but should not import a metrics
  library. The design therefore introduces a small domain observer and keeps
  Prometheus adaptation at composition edges.
- A metrics endpoint on the main server would be easy but would cross public
  browser/data-plane boundaries. A separate disabled-by-default loopback
  listener is slightly more work and substantially safer.

### What warrants a second pair of eyes

- Whether configured profile slugs should be metric labels at all; the proposed
  cap of 64 plus `other` is bounded but still increases series count.
- Whether future non-loopback container scraping should require TLS/auth in the
  process or rely on a private network and external proxy.
- Whether a 90-day summary window is acceptable for expected SQLite volume.

### What should be done in the future

- Implement only Phase 0/1 first and measure query plans before approving
  rollups or charting.
- Record exact query latency and emitted series counts in this diary when work
  resumes.

### Code review instructions

- Start with `pkg/byok/meter/meter.go`, `pkg/byok/meter/health.go`, and
  `pkg/byok/store/sqlite/store.go:1044-1139`.
- Follow the current owner path in `pkg/byok/web/api.go:470-521` and the runtime
  wiring in `cmd/llm-proxy-server/main.go:328-500`.
- Review every proposed metric label against the forbidden-label table in the
  design document.

### Technical details

```text
Current durable path:
runtime completion -> meter.Recorder -> usage_ledger
                                    -> token_counters
                                    -> agent_grant_counters

Proposed incremental path:
Phase 1: indexed owned summary query + authenticated API
Phase 2: simple summary UI
Phase 3: loopback health metrics
Phase 4: bounded inference/rejection metrics
Phase 5+: OIDC/device/inventory metrics and optional dashboards
```

## Step 2: Write the intern implementation guide and prepare delivery

The primary guide translates the evidence into independently releasable phases.
It starts with a no-runtime-change vocabulary phase and an owner summary API,
then adds UI, a health-only metrics listener, request metrics, and optional
advanced collectors. The plan intentionally avoids requiring Prometheus,
Grafana, rollup tables, or OpenTelemetry in the first useful release.

The document includes concrete SQL, Go interfaces, HTTP JSON, metric names,
label bounds, ASCII diagrams, decision records, file-level changes, tests,
risks, alternatives, and acceptance criteria so a new engineer can implement an
MVP without rediscovering the system.

### Prompt Context

**User prompt (verbatim):** (same as Step 1)

**Assistant interpretation:** Convert the source investigation into a standalone technical guide and delivery bundle.

**Inferred user intent:** Make later implementation cheap to restart and safe to split across small PRs.

### What I did

- Defined durable-summary versus operational-metric terminology and ownership.
- Specified `UsageSummaryStore`, fixed aggregate rows, the owner/time SQLite
  query, and a v4 index migration.
- Specified `GET /api/usage-summary` with bounded time windows and an exact JSON
  response.
- Designed a no-chart browser MVP.
- Designed a custom Prometheus registry and loopback-only optional listener.
- Defined a bounded inference observation contract, metric vocabulary, reason
  normalization, and forbidden labels.
- Added later OIDC/device/inventory phases without making them MVP blockers.
- Added seven architecture decision records, acceptance matrices, query/perf
  tests, security analysis, and intern kickoff instructions.

### Why

- A phased plan needs explicit exit criteria; otherwise later phases quietly
  become prerequisites and the MVP is no longer small.
- Concrete contracts reduce the chance that an intern invents unbounded labels,
  arbitrary SQL grouping, or a public metrics route.

### What worked

- The design reuses existing domain seams and does not require changes to the
  durable write path for MVP 1–3.
- Every phase can be reviewed and released independently.
- The guide distinguishes exact owner-visible profile grouping from bounded
  operator profile labels.
- `docmgr doctor --ticket LLM-PROXY-BYOK-OBSERVABILITY --stale-after 30` passed.
- The dry run accepted the four-document bundle, and the real upload returned
  `OK: uploaded LLM PROXY BYOK Observability Design.pdf -> /ai/2026/07/23/LLM-PROXY-BYOK-OBSERVABILITY`.

### What didn't work

- N/A

### What I learned

- The smallest useful deliverable is the aggregate owner API, not Prometheus.
- A custom Prometheus registry is important even for a single binary because
  tests may construct multiple instances in one process.

### What was tricky to build

- The design had to be specific enough to implement while leaving measured
  choices (rollups, histogram buckets, dashboards) genuinely deferred.
- Prometheus cardinality is multiplicative; the guide therefore cautions against
  attaching every safe label to every metric even when each individual label is
  bounded.

### What warrants a second pair of eyes

- Review the proposed metric names before Phase 3 because names become an
  operational compatibility contract.
- Review SQL index shape with representative data before migration acceptance.

### What should be done in the future

- When implementation starts, add diary steps per phase with query plans,
  screenshots, scrape samples, and series counts.

### Code review instructions

- Read the primary design in order; do not skip directly to metrics names before
  understanding data ownership and the MVP sequence.
- Validate ticket metadata and related files with docmgr doctor.

### Technical details

```text
MVP 1: durable summary API (no metrics dependency)
MVP 2: table-based browser summary (no chart dependency)
MVP 3: health-only loopback metrics (no request instrumentation)
MVP 4: bounded request/rejection metrics
MVP 5+: OIDC/device/inventory, then optional dashboards/rollups
```
