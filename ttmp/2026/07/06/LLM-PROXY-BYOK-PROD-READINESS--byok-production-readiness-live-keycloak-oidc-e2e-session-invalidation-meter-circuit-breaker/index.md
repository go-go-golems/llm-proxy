---
Title: 'BYOK production readiness: live Keycloak OIDC e2e, session invalidation, meter circuit breaker'
Ticket: LLM-PROXY-BYOK-PROD-READINESS
Status: active
Topics:
    - byok
    - llm-proxy
    - auth
    - security
    - metering
DocType: index
Intent: long-term
Owners: []
RelatedFiles:
    - Path: repo://pkg/byok/meter/health.go
      Note: Completed committed-write meter circuit and recovery implementation
    - Path: repo://pkg/byok/web/oidc.go
      Note: OIDC RP pending live tiny-idp and PKCE work
    - Path: repo://pkg/byok/web/session.go
      Note: Current HMAC cookie pending server-side session invalidation
ExternalSources: []
Summary: ""
LastUpdated: 2026-07-06T11:09:01.829822882-04:00
WhatFor: ""
WhenToUse: ""
---










# BYOK production readiness: live Keycloak OIDC e2e, session invalidation, meter circuit breaker

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
- auth
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
