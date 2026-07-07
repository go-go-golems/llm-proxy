---
Title: 'Bring Your Own Key: OAuth webapp for credential management and scoped token minting, enforced by llm-proxy'
Ticket: LLM-PROXY-BYOK
Status: active
Topics:
    - byok
    - auth
    - security
    - metering
    - llm-proxy
DocType: index
Intent: long-term
Owners: []
RelatedFiles:
    - Path: ttmp/2026/07/05/LLM-PROXY-BYOK--bring-your-own-key-oauth-webapp-for-credential-management-and-scoped-token-minting-enforced-by-llm-proxy/design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md
      Note: Primary analysis and architecture proposal
    - Path: ttmp/2026/07/05/LLM-PROXY-BYOK--bring-your-own-key-oauth-webapp-for-credential-management-and-scoped-token-minting-enforced-by-llm-proxy/design-doc/02-intern-guide-byok-system-analysis-design-and-implementation.md
      Note: Full intern-facing analysis/design/implementation guide
    - Path: ttmp/2026/07/05/LLM-PROXY-BYOK--bring-your-own-key-oauth-webapp-for-credential-management-and-scoped-token-minting-enforced-by-llm-proxy/reference/01-investigation-diary.md
      Note: Chronological investigation record
ExternalSources: []
Summary: Add BYOK functionality around llm-proxy — an OAuth/OIDC webapp for managing per-user LLM provider credentials and minting restricted broker tokens (usage budgets, model allowlists, expiry, rate limits), with llm-proxy validating, scoping, and metering requests against those tokens before running upstream inference through Geppetto.
LastUpdated: 2026-07-05T19:15:00-04:00
WhatFor: Track the design and implementation of the BYOK credential/token control plane and the llm-proxy enforcement data plane.
WhenToUse: Use this ticket as the landing page for all BYOK-on-llm-proxy work; read the design doc first, then the diary.
---



# Bring Your Own Key: OAuth webapp for credential management and scoped token minting, enforced by llm-proxy

## Overview

Users log in via OAuth (OIDC, Keycloak-backed per prior design), store their LLM provider credentials (OpenAI, Anthropic, Gemini, …) in an encrypted vault, and mint **broker tokens** with explicit restrictions: usage/token budgets, model allowlists, expiry, and rate limits. Clients then call llm-proxy's existing OpenAI-compatible API with a minted token; llm-proxy validates the token, restricts models to the token's scope, meters usage against the budget, and performs the actual inference through Geppetto using the stored provider credential — the raw provider key is never exposed to the client.

**Prior art (verified):** the `2026-04-17--byok-host` workspace is this exact idea — a doc-first workspace with three tickets (BYOK-BROKER: delegated broker security model + bearer-token smoke prototype; BYOK-BROKER-WEB-UI: login/credential/consent web UI; BYOK-KEYCLOAK-STORAGE: Keycloak OIDC + pluggable SQLite storage). It stopped at ticket-scoped prototypes against a fake provider. This ticket merges that design with llm-proxy, which currently has no auth layer at all.

See `design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md` for the full analysis, architecture, and phased plan.

## Key Links

- **Related Files**: See frontmatter RelatedFiles field
- **External Sources**: See frontmatter ExternalSources field

## Status

Current status: **active**

## Topics

- byok
- auth
- security
- metering
- llm-proxy

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
