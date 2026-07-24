# Tasks

## Research and design delivery

- [x] Create the focused observability ticket, primary design doc, and diary.
- [x] Map current usage ledger, counters, audit, health, API, UI, and runtime wiring.
- [x] Define security/privacy/cardinality boundaries and official API references.
- [x] Produce phased intern implementation guide with APIs, SQL, pseudocode, diagrams, decisions, tests, and file map.
- [x] Validate ticket metadata and upload the design bundle to reMarkable.

## Phase 0 — vocabulary and guardrails

- [ ] Add summary domain types and a closed operational label vocabulary.
- [ ] Add forbidden-label and cardinality-bound tests without runtime behavior changes.

## Phase 1 — owner usage-summary API MVP

- [ ] Add forward schema migration and validated `(user_id, created_at)` ledger index.
- [ ] Add memory/SQLite `UsageSummaryStore` parity and query-plan tests.
- [ ] Add authenticated bounded `GET /api/usage-summary` and ownership tests.

## Phase 2 — browser summary MVP

- [ ] Add 24h/7d/30d range controls, totals, and table-based grouped usage.
- [ ] Validate empty, mixed-status, near-limit, and error UI states.

## Phase 3 — metering-health metrics MVP

- [ ] Add pinned Prometheus Go client and custom registry.
- [ ] Add disabled-by-default loopback-only metrics listener and lifecycle tests.
- [ ] Export bounded meter health, Go, and process metrics without a public Caddy route.

## Phase 4 — inference and rejection metrics

- [ ] Add domain observer and exactly-once success/error/rejection instrumentation.
- [ ] Add bounded route/outcome/reason/channel/profile metrics and cardinality tests.

## Phase 5 — identity and device metrics

- [ ] Add bounded introspection cache/outcome metrics.
- [ ] Add bounded agent grant/exchange outcome metrics.

## Phase 6 — optional full observability

- [ ] Measure scrape/query performance and decide whether inventory caching or durable rollups are justified.
- [ ] Add example scrape/alerts and dashboards only after metric contracts stabilize.
- [ ] Define ledger/audit retention independently from Prometheus retention.
