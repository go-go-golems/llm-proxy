---
Title: Replace Keycloak with tiny-idp as the BYOK control-plane OIDC issuer
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
DocType: index
Intent: long-term
Owners: []
RelatedFiles: []
ExternalSources: []
Summary: "Architecture and implementation ticket for replacing Keycloak with a separately deployed tiny-idp, completing PKCE-correct browser login, adding RFC 8628 coding-agent authorization and scoped llmp capability exchange, and preserving encrypted provider credentials, revocation, cumulative budgets, audit, usage, and operational metrics."
LastUpdated: 2026-07-22T15:42:54-04:00
WhatFor: "Give implementers an evidence-backed design for the complete human-to-device-to-LLM authority chain and track its production prerequisites, implementation, validation, and delivery."
WhenToUse: "Read the architecture guide first when implementing or reviewing tiny-idp browser login, coding-agent token acquisition, agent grants, metering, audit, or deployment."
---

# Replace Keycloak with tiny-idp as the BYOK control-plane OIDC issuer

## Overview

llm-proxy's BYOK control plane now implements PKCE-correct browser OIDC,
issuer-aware identities, revocable server-side sessions, encrypted provider
credentials, persisted browser-managed agent grants, cumulative non-resettable
grant budgets, RFC 7662 agent authentication, RFC 8628 device authorization,
rotated scoped capabilities, request-time provider-key injection, fail-closed
usage accounting, and typed audit. tiny-idp supplies identity protocols;
llm-proxy remains the owner of LLM capability policy.

This ticket replaces Keycloak with a separately deployed tiny-idp and adds a
coding-agent path. The agent completes RFC 8628 Device Authorization, presents
a short-lived tiny-idp token to `/agent/v1/*`, and exchanges it for an
`llmp_...` capability derived from a browser-approved grant. The capability—not
the IdP token—is used on `/v1/*` and is enforced by the existing BYOK data
plane.

Motivation: the `LLM-PROXY-BYOK-PROD-READINESS` ticket's first task is to
stand up Keycloak compose and drive the full OIDC browser flow. This ticket
redirects that effort from Keycloak to tiny-idp, which we have since built as
part of the tiny-idp/tiny-idp-xapp work.

## Key links

### Core docs

- `design-doc/01-tinyidp-byok-coding-agent-architecture-and-intern-implementation-guide.md`
  — the primary implementation contract. Read this first.
- `research/01-tiny-idp-integration-research-and-architect-onboarding-brief.md`
  — the evidence inventory, corrected to note that the current RP lacks PKCE.
- `reference/01-implementation-diary.md` — chronological research, decisions,
  corrections, validation, and delivery evidence.

### Companion tickets

- `ttmp/2026/07/05/LLM-PROXY-BYOK--...` — the BYOK control-plane + enforcement
  design (especially `design-doc/01-...-prior-art-analysis-and-architecture-proposal.md`
  §Proposed Solution, §Phase 3).
- `ttmp/2026/07/06/LLM-PROXY-BYOK-PROD-READINESS--...` — the open prod-readiness
  tasks this integration unblocks (live OIDC e2e, callback check order).

### Related repos

- llm-proxy: `/home/manuel/code/wesen/go-go-golems/llm-proxy` (integration target)
- tiny-idp: `/home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp` (new IdP)
- byok-host: `/home/manuel/code/wesen/2026-04-17--byok-host` (prior-art design only)

## Status

Current status: **canonical Phases 3–5 complete**

Research, architecture, and roadmap Phases 0–5 are complete. tiny-idp PR #15
was merged as commit `486a3e3108f3eeda3d100f3db613aecc74f4d13d`, released as
`v0.0.5`, and pinned in Compose by immutable OCI digest. Fresh clean-volume
acceptance passed CA-verified readiness, browser PKCE OIDC, persisted grant
creation, RFC 8628 device approval, RFC 7662 introspection, exact identity and
pre-approved grant exchange, secure CLI caching/logout, four-direction token
route separation, and grant-revocation cascade without provider dispatch.
The accepted topology remains a separate singleton tiny-idp service with
long-running services non-root and all secrets file-mounted. Agent grants bind
owned credentials to concrete profiles, enforce per-capability and cumulative
budgets across rotation, and audit revocation atomically. Broader observability
and named coding-agent compatibility remain later roadmap work and are not
claimed by this completion.

## Topics

- auth
- security
- byok
- oidc
- identity
- llm-proxy
- integration

## Tasks

See [tasks.md](./tasks.md) for the current task list.

## Changelog

See [changelog.md](./changelog.md) for recent changes and decisions.

## Structure

- research/ - Evidence-gathering and source inventory documents
- design-doc/ - Accepted architecture and intern implementation guide
- reference/ - Chronological implementation diary
- playbooks/ - Command sequences and test procedures (to be written)
- scripts/ - Temporary code and tooling
- various/ - Working notes and research
- archive/ - Deprecated or reference-only artifacts
