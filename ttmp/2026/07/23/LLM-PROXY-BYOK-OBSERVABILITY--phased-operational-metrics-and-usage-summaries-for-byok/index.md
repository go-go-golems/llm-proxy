---
Title: Phased operational metrics and usage summaries for BYOK
Ticket: LLM-PROXY-BYOK-OBSERVABILITY
Status: active
Topics:
    - byok
    - security
    - llm-proxy
    - integration
DocType: index
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/byok/meter/meter.go
      Note: Authoritative completed inference accounting seam
    - Path: repo://pkg/byok/meter/health.go
      Note: Existing bounded health snapshot for the first metrics MVP
    - Path: repo://pkg/byok/store/sqlite/store.go
      Note: Durable ledger and future owner summary query
    - Path: repo://pkg/byok/web/api.go
      Note: Existing owner-scoped usage API boundary
ExternalSources:
    - https://prometheus.io/docs/practices/naming/
    - https://prometheus.io/docs/practices/instrumentation/
Summary: Incremental design ticket for durable owner usage summaries and privacy-safe bounded operational metrics, intentionally split into independently valuable MVPs.
LastUpdated: 2026-07-23T16:45:00-04:00
WhatFor: Preserve an implementation-ready observability plan that can be resumed later without committing to a full monitoring platform.
WhenToUse: Start here when selecting an observability MVP or reviewing metrics, summary queries, cardinality, privacy, and listener exposure.
---

# Phased operational metrics and usage summaries for BYOK

## Overview

This ticket contains the evidence-backed follow-up design for the operational
metrics and usage-summary work deferred from `LLM-PROXY-BYOK-TINYIDP`. Durable
usage accounting, token/grant counters, typed audit, and fail-closed metering
health are already implemented. This ticket adds read-side owner summaries and
bounded operator visibility without changing those security semantics.

The plan is deliberately incremental:

1. define vocabulary and guardrail tests;
2. ship an authenticated owner usage-summary API;
3. add a simple table-based browser summary;
4. expose metering health on an optional loopback-only metrics listener;
5. add bounded inference/rejection metrics;
6. add OIDC/device metrics and optional dashboards only when needed.

No implementation phase is active yet. The documents are intended to make a
later intern kickoff low-risk and to allow stopping after any useful MVP.

## Key links

- [Intern implementation guide](design-doc/01-byok-operational-metrics-and-usage-summaries-intern-implementation-guide.md)
- [Investigation diary](reference/01-investigation-diary.md)
- [Incremental tasks](tasks.md)

## Status

Current status: **research and design complete; implementation deferred**

## Scope boundary

This ticket does not add billing, provider price tables, distributed tracing,
multi-broker aggregation, a Prometheus/Grafana deployment, or public metrics.
Metric labels must never contain owner or credential identifiers. Exact owner
detail belongs only in authenticated durable summary APIs.

## Structure

- `design-doc/` — primary architecture and phased implementation guide
- `reference/` — chronological investigation diary
- `tasks.md` — independently releasable MVP backlog
- `changelog.md` — ticket updates and delivery evidence
