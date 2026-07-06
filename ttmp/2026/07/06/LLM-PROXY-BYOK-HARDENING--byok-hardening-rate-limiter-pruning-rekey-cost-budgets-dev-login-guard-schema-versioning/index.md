---
Title: 'BYOK hardening: rate-limiter pruning, rekey, cost budgets, dev-login guard, schema versioning'
Ticket: LLM-PROXY-BYOK-HARDENING
Status: active
Topics:
    - byok
    - llm-proxy
    - security
    - metering
DocType: index
Intent: long-term
Owners: []
RelatedFiles:
    - Path: llm-proxy/pkg/byok/authmw/ratelimit.go
      Note: Fixed-window rate limiter — pruning + 2x boundary comment
    - Path: llm-proxy/pkg/byok/store/sqlite/store.go
      Note: SQLite schema — schema_version target
    - Path: llm-proxy/pkg/byok/vault/vault.go
      Note: AES-256-GCM vault with version byte — rekey target
ExternalSources: []
Summary: ""
LastUpdated: 2026-07-06T11:09:01.733890778-04:00
WhatFor: ""
WhenToUse: ""
---




# BYOK hardening: rate-limiter pruning, rekey, cost budgets, dev-login guard, schema versioning

## Overview

<!-- Provide a brief overview of the ticket, its goals, and current status -->

## Key Links

- **Related Files**: See frontmatter RelatedFiles field
- **External Sources**: See frontmatter ExternalSources field

## Status

Current status: **active**

## Topics

- byok
- llm-proxy
- security
- metering

## Tasks

See [tasks.md](./tasks.md) for the current task list.

## Changelog

See [changelog.md](./changelog.md) for recent changes and decisions.

## Structure

- design/ - Architecture and design documents
- reference/ - Prompt packs, API contracts, context summaries
- playbooks/ - Command sequences and test procedures
- scripts/ - Temporary code and tooling
- various/ - Working notes and research
- archive/ - Deprecated or reference-only artifacts
