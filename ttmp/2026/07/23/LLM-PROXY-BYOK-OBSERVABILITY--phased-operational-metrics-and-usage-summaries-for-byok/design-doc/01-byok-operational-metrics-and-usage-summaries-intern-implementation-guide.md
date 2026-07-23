---
Title: BYOK Operational Metrics and Usage Summaries Intern Implementation Guide
Ticket: LLM-PROXY-BYOK-OBSERVABILITY
Status: active
Topics:
    - byok
    - security
    - llm-proxy
    - integration
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://cmd/llm-proxy-server/main.go
      Note: |-
        Runtime composition and future loopback metrics listener
        Runtime composition and future metrics listener lifecycle
    - Path: repo://pkg/byok/authmw/middleware.go
      Note: Pre-dispatch rejection and serialization seam
    - Path: repo://pkg/byok/meter/health.go
      Note: |-
        Existing bounded process-health snapshot and circuit transitions
        Bounded circuit snapshot and transition counters
    - Path: repo://pkg/byok/meter/meter.go
      Note: |-
        Authoritative post-inference usage recording seam
        Authoritative completed-inference usage seam
    - Path: repo://pkg/byok/store/sqlite/store.go
      Note: |-
        Durable ledger transaction and future aggregate query implementation
        Durable usage transaction and future aggregate query
    - Path: repo://pkg/byok/store/store.go
      Note: Store interfaces to extend with owned aggregate summary queries
    - Path: repo://pkg/byok/web/api.go
      Note: |-
        Authenticated owner-scoped usage API boundary
        Owner-scoped usage API boundary
ExternalSources:
    - https://prometheus.io/docs/practices/naming/
    - https://prometheus.io/docs/practices/instrumentation/
    - https://prometheus.io/docs/guides/go-application/
    - https://pkg.go.dev/github.com/prometheus/client_golang/prometheus
    - https://pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp
    - https://prometheus.io/docs/specs/om/open_metrics_spec/
Summary: Evidence-backed, incremental design for owner-visible durable usage summaries and bounded-cardinality operator metrics, starting with small MVPs and preserving BYOK security and accounting boundaries.
LastUpdated: 2026-07-23T16:45:00-04:00
WhatFor: Give a new engineer enough architecture, API, schema, privacy, testing, and phased implementation detail to add useful observability without leaking identities or building a full monitoring platform at once.
WhenToUse: Read before changing usage queries, adding a metrics dependency or listener, instrumenting inference/authentication, or exposing BYOK operational data.
---


# BYOK Operational Metrics and Usage Summaries Intern Implementation Guide

## 1. Executive summary

llm-proxy already has the hard part of BYOK accounting: an append-only usage
ledger, transactionally updated token and grant counters, typed audit events,
and a fail-closed metering-health circuit. What it does not yet have is a useful
aggregate view for a credential owner or a bounded operational view for a
service operator.

This document proposes two deliberately separate products:

1. **Durable usage summaries** answer owner questions such as “how many requests
   and tokens did my capabilities use this week?” They are computed from the
   SQLite ledger under the authenticated browser user's identity. They may
   contain exact profile names and per-owner detail because access is scoped and
   the data is already durable.
2. **Operational metrics** answer fleet/process questions such as “is metering
   healthy?”, “are requests failing?”, and “are device exchanges being
   rejected?”. They are ephemeral Prometheus/OpenMetrics time series. They must
   never contain user IDs, subjects, emails, token IDs, grant IDs, credential
   IDs, client installation IDs, raw error text, bearer material, or provider
   credentials.

The work is split into small increments. An intern can stop after any MVP and
still leave a useful, secure result:

- **MVP 1:** owner usage-summary API only;
- **MVP 2:** a small usage-summary panel in the existing browser UI;
- **MVP 3:** loopback-only metrics listener with metering-health metrics;
- **MVP 4:** bounded inference/rejection metrics;
- **Later increments:** OIDC/device metrics, inventory gauges, dashboards, and
  retention controls.

The design does not introduce distributed coordination, tracing, a metrics
backend, billing, cost conversion, or a multi-tenant operator console. It keeps
the existing single-active-broker constraint.

## 2. Problem statement

### 2.1 What users can see today

The browser UI shows each token's running token/request counters and a raw table
of up to 200 ledger entries. `pkg/byok/web/static/app.js:76-86` renders a token
progress bar, `app.js:162-180` renders ledger rows, and
`pkg/byok/web/api.go:470-521` implements `GET /api/usage` after checking token
ownership.

This is adequate for debugging one token. It is not adequate for questions
that cross token rotation, web/device issuance, models, or time windows:

- total requests and tokens today, this week, or this month;
- usage grouped by profile/model;
- successful versus provider-error calls;
- streaming versus non-streaming usage;
- web capabilities versus device-exchanged capabilities;
- cumulative usage across all tokens belonging to one grant.

### 2.2 What operators can see today

Operators have liveness and readiness only. `pkg/server/server.go:78-103`
registers `/healthz` and `/readyz`; readiness reflects the metering circuit.
`pkg/byok/meter/health.go:38-46` already defines a useful bounded snapshot:
state, failure counters, open/recovery totals, and retry time. No endpoint
exports that snapshot.

Runtime logs capture individual failures, but logs are not an aggregate signal.
They are also the wrong place to encode high-volume usage or user dimensions.

### 2.3 Why “just add labels” is unsafe

Prometheus creates one time series per unique label set. Labels such as
`user_id`, `token_id`, or `subject` therefore create both a privacy leak and an
unbounded memory/storage cost. Official Prometheus guidance explicitly warns
against high-cardinality identifiers and recommends investigating alternatives
when a metric can grow to roughly 100 or more series.

BYOK makes this sharper than an ordinary web service:

- token, grant, credential, and installation IDs are intentionally random and
  unique;
- issuer subjects and emails are identity data;
- error strings can contain upstream metadata;
- raw paths can contain unbounded values in future route families;
- model strings are caller-provided but must resolve to configured profile
  slugs before dispatch.

Durable owned summaries and operational metrics therefore need different
schemas and access boundaries.

## 3. Scope

### 3.1 In scope

- authenticated, owner-scoped aggregate usage summaries;
- a small browser summary panel built on those aggregates;
- process-local Prometheus/OpenMetrics exposition on an optional loopback-only
  listener;
- bounded metrics for metering health, inference outcomes, policy rejection,
  and later OIDC/device exchange;
- explicit metric vocabulary and label allowlists;
- schema/index changes needed for efficient owner/time aggregate queries;
- tests for privacy, ownership, cardinality, SQL parity, and failure behavior;
- incremental deployment and rollback guidance.

### 3.2 Out of scope

- provider invoice reconciliation or monetary billing;
- cost tables, currencies, discounts, or price history;
- user IDs, token IDs, grant IDs, subjects, email, IP, or request IDs as metric
  labels;
- full distributed tracing;
- log aggregation or a Prometheus/Grafana deployment;
- a public metrics endpoint on the Caddy browser listener;
- distributed/multi-broker aggregation;
- changing budget enforcement or durable ledger semantics;
- exporting provider credentials, bearer tokens, or audit payloads;
- long-term precomputed rollup tables in the first MVP.

## 4. Current-state architecture

### 4.1 Request and accounting flow

```text
client
  -> /v1/chat/completions or /v1/completions
  -> authmw: token/grant/rate/budget checks
  -> profile resolution + encrypted credential injection
  -> provider inference
  -> runtime UsageRecorder hook
  -> meter.Recorder
  -> Store.RecordUsage transaction
       + usage_ledger row
       + token_counters increment
       + agent_grant_counters increment when applicable
```

`pkg/byok/authmw/middleware.go:36-126` performs fail-closed checks and serializes
requests with token/grant process-local locks. Rejections call
`rejected(...)`, which appends an `inference.rejected` audit event at
`middleware.go:135-145`.

`pkg/runtime/usage.go:10-14` defines the provider-neutral completion hook.
Non-streaming and streaming Chat Completions invoke it in
`pkg/runtime/chat_service.go:46-55` and `chat_service.go:90-103`. The recorder
uses provider-authoritative usage when available and records zero counts when a
provider omits usage so request budgets still advance
(`pkg/byok/meter/meter.go:24-54`).

SQLite commits the ledger row and both counter families in one transaction
(`pkg/byok/store/sqlite/store.go:1044-1106`). A failed write opens or advances
the shared metering circuit; new provider dispatch then fails closed.

### 4.2 Existing durable records

| Record | Existing purpose | Key dimensions | Current reader |
| --- | --- | --- | --- |
| `usage_ledger` | append-only completed inference | token, user, model, status, token counts, stream flag, time | owner `/api/usage` |
| `token_counters` | O(1) per-token enforcement totals | token | middleware and UI |
| `agent_grant_counters` | non-resettable grant totals | grant | middleware and UI |
| `audit_events` | security/control-plane decisions | user, token, event type, payload, time | operator/fixtures |
| `metering_health` | committed-write probe row | singleton | health circuit |

The domain types are in `pkg/byok/store/models.go:83-164`. These records must
remain semantically distinct. An audit event is not billable usage; a metric is
not durable accounting; a ledger row is not an operational alert.

### 4.3 Existing indexes and query gap

The current ledger index is `(token_id, created_at)`
(`pkg/byok/store/sqlite/schema.go:72-83`). That exactly supports the current
per-token ledger endpoint. It does not efficiently support owner-wide time
queries such as:

```sql
WHERE user_id = ? AND created_at >= ? AND created_at < ?
GROUP BY model, status, streamed
```

MVP 1 therefore needs a forward migration adding an owner/time index. Query
plan tests should prove SQLite selects it.

### 4.4 Existing browser behavior

`GET /api/tokens` calls `GetCounters` once per token
(`pkg/byok/web/api.go:364-385`), then the UI shows one progress bar per token.
`GET /api/usage` first lists all owned tokens to prove ownership, then reads at
most 200 rows for one token (`api.go:470-521`).

The summary API should not repeat that N+1 pattern. Ownership belongs directly
in the aggregate SQL predicate (`WHERE usage_ledger.user_id = ?`).

### 4.5 Existing process health

The metering circuit already provides the first safe metric source. Its
`Snapshot()` method takes one lock and returns only bounded values
(`pkg/byok/meter/health.go:211-226`). It does not include identities, token IDs,
errors, or paths. This should be the entire MVP 3 source rather than duplicating
circuit state inside a metrics package.

### 4.6 Runtime composition

`cmd/llm-proxy-server/main.go:328-396` creates the SQLite store, health circuit,
vault engine provider, usage recorder, runtime services, and data-plane
middleware. `main.go:483-500` owns the HTTP server lifecycle. A metrics listener
belongs here as another explicit runtime component with the same cancellation
and shutdown context.

## 5. Terminology and data ownership

### 5.1 Usage

A durable fact about a completed provider attempt. Usage may have status `ok` or
`error`; provider omissions produce zero token counts but still count as a
request. Rejected pre-dispatch policy decisions are audited but are not provider
usage.

### 5.2 Summary

An aggregate derived from durable usage rows under an authenticated owner's
scope. A summary can be recomputed after restart and supports exact profile
names.

### 5.3 Metric

A process-local time series intended for scraping and alerting. Counters may
reset at restart. Metrics use a small global vocabulary and never expose owner
identifiers.

### 5.4 Audit

An append-only security/control-plane record. Audit payloads may include opaque
internal IDs when appropriate for investigation, but those IDs do not become
metric labels.

## 6. Proposed architecture

```text
                          durable plane
provider completion ─┐
provider error ───────┼─> meter.Recorder ─> Store.RecordUsage
                     │                       ├─ usage_ledger
                     │                       ├─ token counters
                     │                       └─ grant counters
                     │
                     └─> Metrics.ObserveCompleted (later MVP)

policy rejection ───────> audit event
                     └─> Metrics.ObserveRejected (later MVP)

browser session ─> GET /api/usage-summary
                     └─> UsageSummaryStore.Aggregate(user, window)
                             └─ usage_ledger + tokens JOIN

operator scraper ─> loopback metrics listener /metrics
                     ├─ custom registry
                     ├─ process/Go collectors
                     ├─ meter.Health Snapshot collector
                     └─ bounded runtime counters
```

### 6.1 New package boundaries

Recommended packages:

```text
pkg/byok/usage/
  models.go            # summary request/result types
  validate.go          # range, bucket, and limit validation

pkg/byok/observability/
  metrics.go           # metric vectors and Observer methods
  health_collector.go  # reads meter.Health Snapshot
  labels.go            # closed route, outcome, and reason vocabularies
  server.go            # custom registry + loopback listener lifecycle
```

Store implementations remain under `pkg/byok/store/memory` and
`pkg/byok/store/sqlite`. Do not import Prometheus types into store interfaces,
policy, or runtime domain models.

## 7. MVP 1: owner usage-summary API

### 7.1 User story

“As a signed-in credential owner, show my total requests and tokens for a
bounded time window, grouped by configured profile and outcome, across web and
device capabilities.”

This MVP has no Prometheus dependency and no new listener.

### 7.2 Store contract

Add a narrow interface embedded into `store.Store`:

```go
type UsageSummaryQuery struct {
    UserID string
    Since  time.Time // inclusive
    Until  time.Time // exclusive
}

type UsageSummaryRow struct {
    Model            string
    Status           string
    Streamed         bool
    IssueChannel     IssueChannel
    Requests         int64
    PromptTokens     int64
    CompletionTokens int64
    CachedTokens     int64
}

type UsageSummaryStore interface {
    SummarizeUsage(context.Context, UsageSummaryQuery) ([]UsageSummaryRow, error)
}
```

Do not accept arbitrary grouping fields from HTTP. The result shape is fixed so
SQL remains reviewable and callers cannot construct expensive dimensions.

### 7.3 SQLite query

```sql
SELECT
  l.model,
  l.status,
  l.streamed,
  t.issue_channel,
  COUNT(*) AS requests,
  COALESCE(SUM(l.prompt_tokens), 0),
  COALESCE(SUM(l.completion_tokens), 0),
  COALESCE(SUM(l.cached_tokens), 0)
FROM usage_ledger AS l
JOIN tokens AS t ON t.id = l.token_id
WHERE l.user_id = ?
  AND l.created_at >= ?
  AND l.created_at < ?
GROUP BY l.model, l.status, l.streamed, t.issue_channel
ORDER BY l.model, l.status, l.streamed, t.issue_channel;
```

The query returns bounded dimensions relative to one user's actual ledger.
Add migration v4:

```sql
CREATE INDEX idx_ledger_user_time
ON usage_ledger(user_id, created_at);
```

Start with the minimal two-column index. Do not prematurely add model/status to
the index; measure `EXPLAIN QUERY PLAN` and representative data first.

### 7.4 HTTP contract

```http
GET /api/usage-summary?since=2026-07-01T00:00:00Z&until=2026-08-01T00:00:00Z
Cookie: opaque BYOK session
```

Rules:

- session is mandatory through the existing `requireSession` wrapper;
- `since` and `until` are RFC3339 UTC instants;
- default window is the last 24 hours;
- maximum window is 90 days in MVP 1;
- `until` must be after `since`;
- future `until` is clamped to server `now` or rejected consistently;
- response never contains token, grant, credential, user, subject, or client
  installation IDs;
- query body is absent; URL length remains bounded.

Response:

```json
{
  "since": "2026-07-01T00:00:00Z",
  "until": "2026-08-01T00:00:00Z",
  "totals": {
    "requests": 18,
    "prompt_tokens": 410,
    "completion_tokens": 732,
    "cached_tokens": 90,
    "total_tokens": 1142
  },
  "groups": [
    {
      "model": "umans-glm-5.2",
      "status": "ok",
      "streamed": false,
      "issue_channel": "device_exchange",
      "requests": 6,
      "prompt_tokens": 111,
      "completion_tokens": 146,
      "cached_tokens": 0,
      "total_tokens": 257
    }
  ]
}
```

Compute `total_tokens = prompt_tokens + completion_tokens`; cached tokens remain
reported separately because current enforcement does not add them to total
budget counters.

### 7.5 Memory-store parity

The memory store must implement the same grouping semantics. Iterate ledger rows
under its lock, resolve each token's issue channel, aggregate by a comparable
struct key, and sort results deterministically. Shared conformance tests must run
against memory and SQLite stores.

### 7.6 MVP 1 acceptance

- owner A cannot observe owner B's rows;
- boundary timestamps use `[since, until)` exactly;
- all statuses and both stream values group correctly;
- web, CLI, and device issue channels remain distinct;
- totals equal the sum of groups;
- zero-row windows return empty groups and zero totals;
- invalid/oversized windows return 400;
- SQLite query plan uses `idx_ledger_user_time`;
- no existing `/api/usage` behavior changes.

## 8. MVP 2: browser usage-summary panel

MVP 2 consumes MVP 1 without adding persistence or metrics.

### 8.1 UI scope

Add a separate “Usage summary” card above the raw usage table:

- range presets: 24 hours, 7 days, 30 days;
- totals: requests, prompt, completion, total, cached;
- a compact table grouped by profile/model and status;
- issue-channel badge (`web`, `operator_cli`, `device_exchange`);
- loading, empty, and error states;
- no graphing dependency in the first version.

The existing raw per-token table remains useful for investigation. Do not
replace it.

### 8.2 Why no chart library yet

A table is accessible, printable, easy to test, and sufficient to validate the
aggregate contract. Charting creates decisions about buckets, time zones,
colors, tooltips, and dependencies before the underlying API has production
experience.

### 8.3 Browser pseudocode

```javascript
async function refreshUsageSummary(range) {
  showLoading();
  const until = new Date();
  const since = subtractRange(until, range);
  const summary = await api(
    `/api/usage-summary?since=${encodeURIComponent(since.toISOString())}` +
    `&until=${encodeURIComponent(until.toISOString())}`
  );
  renderTotals(summary.totals);
  renderSummaryRows(summary.groups);
}
```

Never interpolate response values through `innerHTML`; use the existing `el`
helper/text nodes.

## 9. MVP 3: loopback metering-health metrics

### 9.1 Goal

Expose only the already-bounded metering-health snapshot and standard process/Go
collectors. This proves listener security and scrape behavior before touching
request instrumentation.

### 9.2 Dependency and registry

Use `github.com/prometheus/client_golang` with a **custom registry**, not the
global default registry:

```go
registry := prometheus.NewRegistry()
registry.MustRegister(
    collectors.NewGoCollector(),
    collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
    observability.NewHealthCollector(meterHealth),
)
handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
```

A custom registry prevents tests and multiple server instances from sharing
mutable global collectors or panicking on duplicate registration.

### 9.3 Listener contract

Add:

```text
--metrics-listen string
    Empty by default (disabled).
    MVP 3 accepts loopback host:port only, e.g. 127.0.0.1:9090.
```

The metrics server:

- uses a dedicated `http.Server` and `ServeMux`;
- registers only `GET /metrics`;
- applies `ReadHeaderTimeout`, `ReadTimeout`, and `IdleTimeout`;
- starts after all collectors are constructed;
- shuts down from the same root context as the main server;
- never mounts on the browser/data-plane mux;
- is not routed through Caddy by default;
- fails startup if the configured address is wildcard, unspecified, or
  non-loopback in MVP 3.

Loopback HTTP is acceptable because no credential leaves the host. Production
container orchestration can scrape over a private namespace in a later phase;
that expansion requires an explicit threat-model decision.

### 9.4 Health metric contract

```text
# Exactly one state has value 1.
llm_proxy_byok_meter_state{state="closed|open|half_open"} 0|1

llm_proxy_byok_meter_consecutive_transient_failures 0
llm_proxy_byok_meter_failures_total{class="transient|persistent"} 0
llm_proxy_byok_meter_circuit_transitions_total{transition="opened|recovered"} 0
llm_proxy_byok_meter_retry_timestamp_seconds 0
```

Use counters for monotonically increasing failure/transition totals and gauges
for state, consecutive failures, and retry timestamp. `HealthSnapshot` is the
single source of truth.

### 9.5 Absence behavior

When BYOK is disabled, either:

- omit BYOK health metrics entirely; or
- expose `llm_proxy_byok_enabled 0` and omit the rest.

Choose one and lock it in tests. This guide recommends `llm_proxy_byok_enabled`
plus omission of unavailable subsystem metrics.

## 10. MVP 4: bounded inference and rejection metrics

### 10.1 Observer contracts and exact completion seam

Keep Prometheus out of runtime interfaces by introducing two small domain
events. Completion and rejection have different fields and therefore must not
share one maximally labelled vector:

```go
type InferenceOutcome string
const (
    OutcomeOK    InferenceOutcome = "ok"
    OutcomeError InferenceOutcome = "error"
)

type CompletionObservation struct {
    Route            string
    Outcome          InferenceOutcome
    Streamed         bool
    PromptTokens     int64
    CompletionTokens int64
    CachedTokens     int64
    Duration         time.Duration
}

type RejectionObservation struct {
    Route  string
    Reason string
}

type Observer interface {
    ObserveCompletion(context.Context, CompletionObservation)
    ObserveRejection(context.Context, RejectionObservation)
}
```

An adapter in `pkg/byok/observability` converts observations to Prometheus.
Tests can use a recording fake without importing Prometheus.

The existing `runtime.UsageRecorder.RecordInference` cannot produce a correct
completion observation: it receives no route or duration, and Chat Completions
and legacy Completions intentionally call the same interface. Do **not** emit
completion metrics from `meter.Recorder`. Instead, add an `Observer` field to
both `GeppettoChatCompletionService` and `GeppettoCompletionService`. At each of
their non-streaming and streaming `RunInferenceWithResult` call sites:

1. capture `started := time.Now()` immediately before provider execution;
2. execute inference;
3. call the existing usage recorder with authoritative usage;
4. call `ObserveCompletion` with a compile-time route constant and
   `time.Since(started)`.

For streaming, observe when `RunInferenceWithResult` returns inside the producer
goroutine, not when the HTTP handler first returns the channel. This measures
provider execution through the final provider result. Route constants are
`chat_completions` and `completions`; they are never derived from raw URL text.

### 10.2 Exact rejection seam and avoiding double counting

`TokenAuthWithMeterHealth` protects every `/v1/*` route, including `/v1/models`.
Its current `rejected` helper is an audit helper for failures after a token has
been loaded; it does not run for metering-unavailable, missing-key, invalid-key,
or token-store errors. Therefore it is not a complete or correctly scoped
metrics seam.

Add an exact classifier such as:

```go
func inferenceRoute(r *http.Request) (string, bool) {
    if r.Method != http.MethodPost {
        return "", false
    }
    switch r.URL.Path {
    case "/v1/chat/completions":
        return "chat_completions", true
    case "/v1/completions":
        return "completions", true
    default:
        return "", false
    }
}
```

Classify once at middleware entry. At **every** early return in
`TokenAuthWithMeterHealth`, call a dedicated `observeRejection` only when the
classifier returned true. This includes metering unavailable, missing/invalid
API key, token/grant/counter-store internal errors, unusable token/grant, rate
limit, and budget failures. Keep the existing `rejected` audit helper for the
known-token cases it currently records; do not overload it with metrics.
`/v1/models` and any future non-inference `/v1/*` route must emit no inference
metric.

Double-counting rules are then mechanical:

- each provider attempt emits one completion observation at its runtime service
  call site;
- each pre-dispatch failure on one of the two exact inference routes emits one
  rejection observation in middleware;
- a provider error is `outcome="error"`, never a rejection;
- failures before the middleware or after the observer may belong to generic
  HTTP metrics later, but are outside BYOK inference metrics in MVP 4.

### 10.3 Label policy and hard series budgets

Allowed labels are intentionally distributed across separate vectors:

| Label | Values | Used by | Bound |
| --- | --- | --- | --- |
| `route` | `chat_completions`, `completions` | completions, rejections, duration | 2 |
| `outcome` | `ok`, `error` | completions, tokens, duration | 2 |
| `reason` | closed rejection enum plus `other` | rejections only | ≤12 |
| `streamed` | `true`, `false` | completions and duration only | 2 |
| `kind` | `prompt`, `completion`, `cached` | tokens only | 3 |

MVP 4 intentionally omits `profile` and `issue_channel` from metrics. Exact
profile and channel breakdown remains available in the authenticated durable
usage summary. This avoids multiplying every process metric by the number of
configured profiles. If a later operational incident demonstrates a need for
another dimension, add a separate low-dimensional vector under a new reviewed
series budget rather than expanding the common vectors.

Forbidden labels include `user_id`, `token_id`, `grant_id`, `credential_id`,
`source_client_id`, `client_instance_id`, profile/model, issue channel, issuer,
subject, email, username, IP, raw URL/path, raw error, request ID, and provider
key suffix.

### 10.4 Metrics and explicit maximums

```text
# 2 routes × 2 outcomes × 2 stream values = at most 8 series
llm_proxy_byok_inference_completed_total{route,outcome,streamed}

# 2 routes × at most 12 normalized reasons = at most 24 series
llm_proxy_byok_inference_rejected_total{route,reason}

# 3 token kinds × 2 outcomes = at most 6 series
llm_proxy_byok_inference_tokens_total{kind,outcome}

# 2 routes × 2 outcomes × 2 stream values = 8 histogram label sets
llm_proxy_byok_inference_duration_seconds{route,outcome,streamed}
```

With nine configured buckets, Prometheus also exports the mandatory `+Inf`
bucket, `_sum`, and `_count`. The histogram therefore contributes at most 96
series; all four families together remain at or below 134 process series before
standard Go/process collectors. A test must gather the complete closed
vocabulary and fail if this reviewed ceiling is exceeded.

Use a histogram only after choosing stable buckets from measured provider
latency. A conservative initial set might be
`0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60` seconds. Document bucket changes as a
metrics contract change.

### 10.5 Rejection reason normalization

Map typed internal codes, never error text:

```go
func normalizeReason(code string) string {
    switch code {
    case "budget_exhausted",
         "rate_limit_exceeded",
         "token_revoked",
         "token_expired",
         "model_not_allowed",
         "metering_unavailable",
         "missing_api_key",
         "invalid_api_key",
         "internal_error":
        return code
    default:
        return "other"
    }
}
```

Keep the allowlist at 12 or fewer values. If more typed codes appear, map them
to `other` unless an operator action differs. Never label with API error
messages.

## 11. Later increment: OIDC, device, and inventory metrics

Do not include these in the first metrics PR. Add one subsystem at a time.

### 11.1 Introspection

Candidate metrics:

```text
llm_proxy_byok_introspection_requests_total{result="active|inactive|scope|unavailable"}
llm_proxy_byok_introspection_cache_total{result="positive_hit|negative_hit|miss"}
llm_proxy_byok_introspection_duration_seconds{result}
```

`pkg/byok/oidcauth/oidcauth.go:167-205` has the cache hit/miss and introspection
seam. Do not label issuer, subject, client ID, scope string, or audience.

### 11.2 Device exchange

```text
llm_proxy_byok_agent_exchange_total{result="issued|no_grant|ambiguous|denied|error"}
llm_proxy_byok_agent_grants_returned{bucket=...}  # optional histogram
```

Instrument the agent API, not the CLI, so one server-side event is counted.
Never label grant ID, client instance, subject, or source client ID.

### 11.3 Inventory gauges

Potential gauges:

```text
llm_proxy_byok_tokens{state="active|expired|revoked",channel="..."}
llm_proxy_byok_agent_grants{state="active|expired|revoked"}
llm_proxy_byok_sessions{state="active|expired|revoked"}
```

These require new aggregate store queries. Do not scan all rows on every scrape.
A collector may use a short cache (for example, 15 seconds) and a bounded query
timeout. Scrape failure must not affect readiness or inference.

## 12. Summary API evolution after MVP

Only add these after MVP 1 usage proves a need:

### 12.1 Time buckets

Optional query:

```http
GET /api/usage-summary?since=...&until=...&bucket=day
```

Allowed buckets are a closed enum: `none`, `hour`, `day`. The server normalizes
bucket timestamps in UTC. Never accept arbitrary SQL interval text.

### 12.2 Grant-oriented summaries

An owner may need a view across rotated capabilities. Add a dedicated endpoint
rather than leaking grant IDs through general metrics:

```http
GET /api/agent-grants/{id}/usage-summary?since=...&until=...
```

The server must load the grant by `(user_id, id)` before aggregation. The result
may include exact profile names but never credential bindings or secrets.

### 12.3 Export

CSV/JSON export is a later user feature. Apply maximum windows and row limits;
do not make `/metrics` a user export mechanism.

## 13. Security and privacy model

### 13.1 Data classification

| Data | Owner API | Operator metrics | Logs |
| --- | --- | --- | --- |
| exact profile slug | yes | bounded/normalized only | sparingly |
| token/grant ID | existing owned APIs only | never | avoid |
| user/subject/email | session UI only | never | avoid |
| aggregate token count | yes | yes | avoid per request |
| provider credential | never | never | never |
| bearer capability | mint response once | never | never |
| raw error string | controlled API response | never | structured redacted only |

### 13.2 Metrics endpoint threats

Even bounded metrics reveal traffic volume and health. Therefore:

- disabled by default;
- loopback-only in the first release;
- separate mux and listener;
- no permissive CORS;
- no query parameters that change collection;
- no debug dump of registry internals;
- bounded response through client library behavior and fixed collectors.

### 13.3 Summary endpoint threats

- enforce session and exact owner scope before querying;
- cap time windows;
- fixed grouping contract;
- no arbitrary sort/field/group SQL;
- context timeout for database query;
- consistent 400/500 envelopes without SQL text;
- tests with two issuers and two users.

## 14. Decision records

### Decision: Separate durable summaries from operational metrics

- **Context:** The same usage events are interesting to users and operators, but
  they have different retention, cardinality, and access rules.
- **Options considered:** Put everything in Prometheus; put everything in
  SQLite; separate derived views.
- **Decision:** Owner summaries query durable SQLite; operator metrics are
  bounded process-local time series.
- **Rationale:** This preserves exact owner detail without exposing identity in
  monitoring and avoids treating ephemeral metrics as billing truth.
- **Consequences:** Two APIs/adapters exist, but each has a clear contract.
- **Status:** proposed.

### Decision: Start with summary API before Prometheus

- **Context:** The ticket asks for incremental MVPs rather than a full platform.
- **Options considered:** Build all metrics first; build UI and summaries first;
  implement dashboards and exporter together.
- **Decision:** MVP 1 is a read-only authenticated aggregate API.
- **Rationale:** It reuses durable data, adds immediate user value, and validates
  query semantics without adding a listener or dependency.
- **Consequences:** Operator alerting arrives in MVP 3, not the first PR.
- **Status:** proposed.

### Decision: Use a custom Prometheus registry

- **Context:** Global collectors make tests and multiple server instances in one
  process interfere.
- **Options considered:** default registry; custom registry; handwritten text
  exposition.
- **Decision:** `prometheus.NewRegistry()` plus `promhttp.HandlerFor`.
- **Rationale:** Standard encoding with explicit collector ownership and no
  duplicate-registration globals.
- **Consequences:** Collectors must be registered during composition, which is
  desirable and testable.
- **Status:** proposed.

### Decision: Dedicated loopback listener, disabled by default

- **Context:** Main Caddy routes are browser/data-plane trust boundaries and
  metrics reveal operations.
- **Options considered:** public `/metrics` on main mux; session-protected
  browser route; dedicated listener.
- **Decision:** optional loopback-only listener in the first metrics MVP.
- **Rationale:** Least exposure and no interaction with token/OIDC middleware.
- **Consequences:** Containers need an explicit private scrape configuration in
  a later deployment increment.
- **Status:** proposed.

### Decision: Split metric families with small fixed vocabularies

- **Context:** IDs and arbitrary strings leak privacy, while multiplying even
  individually bounded dimensions can create tens of thousands of series.
- **Options considered:** one fully labelled request vector; capped profile
  labels; separate low-dimensional completion, rejection, token, and duration
  vectors.
- **Decision:** split families by semantic event; omit profile and issue channel
  from MVP metrics; use only closed route/outcome/reason/stream/kind values.
- **Rationale:** The complete MVP contract remains at or below 134 process series,
  while exact profile/channel detail remains in authenticated durable summaries.
- **Consequences:** Prometheus cannot break traffic down by profile or channel;
  adding any dimension later requires a new reviewed series budget.
- **Status:** proposed.

### Decision: Query ledger directly before adding rollup tables

- **Context:** Rollups improve large-history reads but add migrations,
  reconciliation, and write-path coupling.
- **Options considered:** direct indexed query; hourly rollup table; external
  analytics pipeline.
- **Decision:** direct owner/time aggregate query for MVP 1.
- **Rationale:** Current deployment is singleton SQLite and expected volume is
  small; measurement should precede denormalization.
- **Consequences:** Add query-plan and latency tests; introduce rollups only with
  evidence.
- **Status:** proposed.

### Decision: Metrics cannot affect inference readiness

- **Context:** Monitoring is diagnostic, while durable metering is mandatory.
- **Options considered:** fail readiness when scrape collection fails; log and
  continue; couple exporter to circuit.
- **Decision:** exporter/collector failure does not open the meter circuit or
  block inference. The exporter reports its own collection error where safe.
- **Rationale:** A broken dashboard must not become an availability dependency.
- **Consequences:** Durable metering health remains the authoritative readiness
  dependency.
- **Status:** proposed.

## 15. Detailed implementation phases

### Phase 0: vocabulary and guardrail tests (smallest change)

**Deliverable:** no production behavior change.

1. Add `pkg/byok/usage/models.go` summary types.
2. Add `pkg/byok/observability/labels.go` closed enums and forbidden-label list.
3. Add tests that reject forbidden label names and normalize unknown values.
4. Document metric names as a reviewed contract.

**Exit criteria:** package tests pass; no new endpoint/dependency.

### Phase 1: usage-summary backend MVP

1. Add schema migration v4 and required-index validation.
2. Add `UsageSummaryStore` to `store.Store`.
3. Implement memory and SQLite aggregation.
4. Add shared conformance tests.
5. Add `GET /api/usage-summary` and ownership/window tests.
6. Update README API documentation.

**Exit criteria:** API works for one owner across web/device tokens; old usage API
unchanged; query plan uses owner/time index.

### Phase 2: usage-summary UI MVP

1. Add range presets and summary table HTML.
2. Add fetch/render logic using text nodes.
3. Add empty/error/loading states.
4. Add web tests and `node --check`.
5. Render and inspect default, empty, mixed-status, and near-limit states.

**Exit criteria:** an owner can answer “what did I use in 24h/7d/30d?” without
opening individual tokens.

### Phase 3: metering-health metrics MVP

1. Add pinned `client_golang` dependency.
2. Implement custom registry and health collector.
3. Add `--metrics-listen`, disabled by default and loopback-validated.
4. Wire second server lifecycle in `main.go`.
5. Add exact exposition/gather tests and shutdown tests.
6. Add private Compose scrape example without Caddy publication.

**Exit criteria:** loopback scrape shows bounded health metrics; no BYOK IDs or
secrets; main listener behavior unchanged.

### Phase 4: inference/rejection metrics

1. Add domain observer interface and no-op implementation.
2. Instrument completed provider attempts at all four runtime service call
   sites, where route and provider duration are available.
3. Classify the two exact inference routes at middleware entry and observe every
   early rejection return without counting `/v1/models`.
4. Normalize only route, outcome, reason, stream, and token-kind labels.
5. Add split counters; add duration histogram only with reviewed buckets.
6. Add integration tests proving success/error/rejection counts and no double
   counting.
7. Gather the complete vocabularies and assert the total stays at or below the
   reviewed 134-series ceiling.

**Exit criteria:** metrics distinguish successful provider calls, provider
errors, and pre-dispatch rejections without identity labels.

### Phase 5: auth/device metrics

1. Instrument introspection hit/miss/outcomes.
2. Instrument agent grant listing/exchange outcomes.
3. Add bounded cache-size gauge if operationally useful.
4. Prove raw token, issuer subject, client ID, and scopes never enter labels.

**Exit criteria:** operators can distinguish IdP outage, invalid tokens, scope
failures, and grant selection failures.

### Phase 6: inventory, dashboards, and retention (optional/full)

1. Add cached aggregate inventory collectors only if operators need them.
2. Publish example Prometheus scrape and alert rules.
3. Publish a minimal dashboard JSON only after metric names stabilize.
4. Define ledger/audit retention separately; never infer retention from
   Prometheus.
5. Measure query latency and decide whether hourly durable rollups are needed.

**Exit criteria:** operational package is production-documented, but no phase is
required merely to keep earlier MVPs useful.

## 16. Testing strategy

### 16.1 Store conformance

Run each summary fixture against memory and SQLite:

- multiple users;
- multiple tokens and rotations;
- every issue channel;
- ok/error/rejected rows;
- streamed and non-streamed;
- exact since/until boundaries;
- large integer sums;
- deterministic ordering;
- canceled context and SQLite errors.

### 16.2 API tests

- unauthenticated request redirects/rejects through existing session policy;
- one user cannot query another's data;
- invalid RFC3339 returns 400;
- reversed/oversized range returns 400;
- empty results return stable zero structure;
- response contains no IDs or secrets;
- fixed response content type and body limit behavior.

### 16.3 Metric tests

Use registry gather output, not fragile string ordering, for semantic checks:

```go
families, err := registry.Gather()
require.NoError(t, err)
requireNoLabelNames(t, families,
    "user_id", "token_id", "grant_id", "subject", "email")
requireSeriesBound(t, families, expectedMaximum)
```

Also test OpenMetrics/text negotiation through `promhttp` and confirm a scrape
contains no canary credential or bearer value.

### 16.4 Runtime integration

- BYOK disabled with metrics disabled;
- BYOK enabled with metrics disabled;
- loopback metrics enabled;
- non-loopback metrics address fails startup;
- health open/half-open/closed snapshots;
- success, provider error, metering unavailable, missing/invalid key, token-store
  error, budget rejection, rate rejection, and model rejection;
- `/v1/models` rejection produces no BYOK inference metric;
- streaming disconnect still records authoritative usage;
- metrics scrape while SQLite is locked does not panic or alter readiness;
- graceful shutdown stops both servers.

### 16.5 Performance tests

Seed representative ledger sizes (10k, 100k, 1m rows) and record:

- summary query p50/p95;
- `EXPLAIN QUERY PLAN` output;
- scrape duration;
- allocations in observer hot path;
- number of emitted series.

Do not add rollups based on synthetic fear; add them when measured latency
violates an agreed target.

## 17. Acceptance matrix by increment

| Capability | P0 | P1 | P2 | P3 | P4 | P5+ |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| vocabulary/cardinality guard | ✓ | ✓ | ✓ | ✓ | ✓ | ✓ |
| durable owner aggregate API |  | ✓ | ✓ | ✓ | ✓ | ✓ |
| browser summary |  |  | ✓ | ✓ | ✓ | ✓ |
| meter health scrape |  |  |  | ✓ | ✓ | ✓ |
| inference/rejection metrics |  |  |  |  | ✓ | ✓ |
| OIDC/device metrics |  |  |  |  |  | ✓ |
| dashboards/rollups |  |  |  |  |  | optional |

Each column is independently releasable. Do not combine all phases into one PR.

## 18. Operational guidance

### 18.1 Example local scrape

```yaml
scrape_configs:
  - job_name: llm-proxy
    static_configs:
      - targets: ["127.0.0.1:19090"]
```

Start with:

```bash
llm-proxy-server serve \
  --listen 127.0.0.1:8080 \
  --metrics-listen 127.0.0.1:19090 \
  ...
```

The flag does not exist until Phase 3. Do not add a public Caddy route in the
same change.

### 18.2 Initial alerts

Only after Phase 3/4 metrics stabilize:

- metering circuit state is not closed;
- persistent metering failures increase;
- inference error ratio exceeds a measured baseline;
- identity-service unavailable rejections increase;
- scrape itself is down.

Avoid alerts on raw request volume without a baseline.

## 19. Risks and mitigations

| Risk | Mitigation |
| --- | --- |
| metric label leaks identity | compile-time vocabulary, gather tests, canary scans |
| series explosion from profiles | startup allowlist cap and `other` fallback |
| summary query degrades SQLite | owner/time index, max 90-day window, query-plan/perf tests |
| scrape blocks service | separate listener, bounded collectors, timeouts/caching |
| metrics become billing truth | docs and APIs identify SQLite ledger as authoritative |
| duplicate request counts | one observation contract and end-to-end count tests |
| exporter failure opens meter circuit | explicit separation; only durable write failures affect health |
| public endpoint exposure | disabled default, loopback validation, no Caddy route |
| model rename breaks continuity | summaries retain exact historical slug; metrics may aggregate old/new separately or `other` |
| restart resets counters | expected Prometheus counter semantics; durable summaries survive |

## 20. Alternatives considered

### 20.1 OpenTelemetry metrics first

OpenTelemetry libraries are currently indirect dependencies, but there is no
project-level provider/exporter lifecycle. Starting there would require SDK,
resource, exporter, temporality, and shutdown decisions before any user value.
A custom Prometheus registry is the smaller initial operational contract.
Future adapters can consume the domain observer without changing policy/store
code.

### 20.2 Handwritten Prometheus text

This avoids a dependency but shifts escaping, HELP/TYPE metadata, content
negotiation, concurrency, and OpenMetrics correctness into llm-proxy. Use the
maintained Go client instead.

### 20.3 Metrics on the main listener

Operationally easy, but it exposes traffic/health data through the same public
edge and complicates token/session middleware exclusions. Reject for MVP.

### 20.4 Persist every metric

Duplicates ledger and audit, adds write load, and blurs semantics. Persist only
facts that are already durable domain records; derive metrics in memory.

### 20.5 Build hourly rollups immediately

Rollups can be valuable at large volume, but they require backfill,
reconciliation, migration, and repair logic. Begin with an indexed query and
measure.

## 21. Open questions for implementation kickoff

These are intentionally deferred decisions, not blockers for writing this
research ticket:

1. What summary latency target and representative ledger volume should gate a
   rollup table?
2. Should `until > now` clamp or return 400? Pick one and test it.
3. Should cached tokens contribute to a separate UI total only, or also to a
   future effective-cost estimate?
4. Which exact rejection reasons deserve separate time series rather than
   `other`, while retaining the 12-value reason ceiling?
6. Should standard Go/process collectors be enabled by default when the metrics
   listener is enabled?
7. What private-network/TLS policy is required before allowing non-loopback
   metrics addresses in containers?
8. Are inventory gauges actually actionable, or can they remain durable UI
   summaries?

## 22. Intern kickoff checklist

Before coding:

1. Read this guide fully.
2. Read `pkg/byok/store/models.go`, `store.go`, both store implementations, and
   shared conformance tests.
3. Trace one Chat Completions request from `pkg/server` through `authmw`,
   runtime, `meter.Recorder`, and SQLite.
4. Trace one browser `/api/usage` request and its ownership check.
5. Run the existing full tests and focused BYOK race tests.
6. Implement only Phase 0/1 in the first PR unless scope is explicitly changed.
7. Keep a diary of query plans, fixtures, failures, and any vocabulary changes.

## 23. File-level implementation map

| File | Phase | Expected change |
| --- | --- | --- |
| `pkg/byok/usage/models.go` | 0 | new summary domain types |
| `pkg/byok/observability/labels.go` | 0 | closed label vocabulary and series-budget tests |
| `pkg/byok/store/store.go` | 1 | add narrow summary interface |
| `pkg/byok/store/models.go` | 1 | alias/import summary types only if needed; avoid metrics types |
| `pkg/byok/store/memory/store.go` | 1 | deterministic in-memory aggregate |
| `pkg/byok/store/sqlite/schema.go` | 1 | migration v4 owner/time index + validation |
| `pkg/byok/store/sqlite/store.go` | 1 | grouped owner/time SQL query |
| `pkg/byok/store/conformance_test.go` | 1 | parity fixtures |
| `pkg/byok/web/api.go` | 1 | authenticated summary endpoint |
| `pkg/byok/web/web.go` | 1 | route registration |
| `pkg/byok/web/static/index.html` | 2 | summary card/range controls |
| `pkg/byok/web/static/app.js` | 2 | summary fetch/render |
| `pkg/byok/observability/*` | 3–5 | registry, collectors, observers |
| `cmd/llm-proxy-server/main.go` | 3 | metrics flag, composition, lifecycle |
| `pkg/byok/meter/health.go` | 3 | no semantic rewrite; collector reads Snapshot |
| `pkg/runtime/chat_service.go` | 4 | emit route-aware timed chat completion observations at both provider call sites |
| `pkg/runtime/completion_service.go` | 4 | emit route-aware timed legacy completion observations at both provider call sites |
| `pkg/byok/meter/meter.go` | 4 | preserve durable accounting only; do not infer missing route/duration here |
| `pkg/byok/authmw/middleware.go` | 4 | classify exact inference routes and observe every early rejection return separately from audit |
| `pkg/byok/oidcauth/oidcauth.go` | 5 | cache/outcome observations |
| `pkg/byok/agentapi/server.go` | 5 | grant/exchange observations |
| `deploy/docker-compose.yaml` | 3+ | optional private scrape wiring, never public by default |
| `README.md` | every phase | exact supported API/metric contract |

## 24. Validation commands

```bash
GOWORK=off go test ./... -count=1
GOWORK=off go test -race ./pkg/byok/... ./cmd/llm-proxy-server/... -count=1
GOWORK=off go vet ./...
make lint
make glazed-lint
make logcopter-check
make gosec
make govulncheck
node --check pkg/byok/web/static/app.js
git diff --check
docmgr doctor --ticket LLM-PROXY-BYOK-OBSERVABILITY --stale-after 30
```

Phase 3 also needs a scrape golden/semantic test and a manual loopback smoke.
Phase 2 needs rendered browser inspection.

## 25. References

### Repository evidence

- `pkg/byok/meter/meter.go:1-56` — detached authoritative usage recording.
- `pkg/byok/meter/health.go:20-226` — circuit state and bounded snapshot.
- `pkg/byok/store/models.go:83-164` — grant/token counters, ledger, audit.
- `pkg/byok/store/store.go:70-118` — metering and audit interfaces.
- `pkg/byok/store/sqlite/schema.go:35-103` — durable tables/indexes.
- `pkg/byok/store/sqlite/store.go:1044-1139` — atomic usage writes and reads.
- `pkg/byok/web/api.go:340-521` — token DTOs and owned raw usage endpoint.
- `pkg/byok/web/static/app.js:76-180` — current per-token usage UI.
- `pkg/byok/authmw/middleware.go:36-145` — checks, locks, and rejection audit.
- `pkg/runtime/usage.go:10-21` — usage observer seam.
- `pkg/runtime/chat_service.go:18-103` — completion and streaming call sites.
- `pkg/server/server.go:78-103` — current liveness/readiness routes.
- `cmd/llm-proxy-server/main.go:328-500` — runtime composition/lifecycle.

### External API references

- Prometheus metric and label naming:
  https://prometheus.io/docs/practices/naming/
- Prometheus instrumentation practices and cardinality guidance:
  https://prometheus.io/docs/practices/instrumentation/
- Official Go instrumentation guide:
  https://prometheus.io/docs/guides/go-application/
- `client_golang/prometheus` custom registries and collectors:
  https://pkg.go.dev/github.com/prometheus/client_golang/prometheus
- `promhttp.HandlerFor` exposition:
  https://pkg.go.dev/github.com/prometheus/client_golang/prometheus/promhttp
- OpenMetrics 1.0 exposition/data model:
  https://prometheus.io/docs/specs/om/open_metrics_spec/
