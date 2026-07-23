---
Title: Phase 3-5 completion audit
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
    - Path: repo://deploy/docker-compose.yaml
      Note: Pinned release acceptance boundary
    - Path: repo://deploy/tinyidp/bootstrap.sh
      Note: No-grant introspection client bootstrap
    - Path: repo://pkg/byok/deviceclient/client.go
      Note: Phase 5 RFC 8628 and destination validation evidence
    - Path: repo://pkg/byok/oidcauth/oidcauth.go
      Note: Phase 4 strict introspection evidence
    - Path: repo://pkg/byok/store/sqlite/schema.go
      Note: Phase 3 schema migration evidence
    - Path: repo://ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/reference/phase45-grant-ui-final.png
      Note: Final rendered empty-state agent grant UI evidence
ExternalSources:
    - https://github.com/go-go-golems/tiny-idp/pull/15
Summary: Final evidence map proving every canonical Phase 3-5 requirement against the immutable tiny-idp v0.0.5 deployment.
LastUpdated: 2026-07-23T14:25:00-04:00
WhatFor: Verify Phase 3-5 completion without narrowing requirements or relying on inference.
WhenToUse: Read when reviewing the Phase 3-5 implementation or its release evidence.
---



# Phase 3-5 completion audit

## Audit rule

A requirement is complete only when the implementation and fresh validation evidence both exist. Every row below passed; no development-image inference is used for the release boundary.

## Phase 3 — persisted browser-managed grants

| Requirement | Implementation evidence | Validation evidence | State |
| --- | --- | --- | --- |
| Forward-only persisted schema | `pkg/byok/store/sqlite/schema.go` migration v3; grant, binding, counter, and token-provenance columns/tables | `schema_test.go`, including migration and post-migration schema/FK validation | PASS |
| Memory/SQLite parity | `pkg/byok/store/store.go`, `memory/store.go`, `sqlite/store.go` | shared `store/conformance_test.go` on both backends | PASS |
| Owned credential bindings and concrete profiles | `web/api.go` ownership checks and configured-profile allowlist | browser API tests reject foreign credentials and unconfigured models | PASS |
| Per-capability policy | grant-derived credential/model, request/token, rate, TTL policy in store and `authmw` | `authmw/middleware_test.go`, policy tests | PASS |
| Non-resettable cumulative budgets | grant-owned durable counters; issue/rotate never copies or resets them | conformance update/reissue tests and rotated-token aggregate-budget middleware test | PASS |
| Expiry, disable/revoke, and cascades | grant liveness checks; credential-delete and grant-revoke child-token cascades | conformance and SQLite lifecycle tests | PASS |
| Atomic mutation plus typed audit | audited create/update/revoke/issue and credential-delete transactions | injected audit-trigger failures prove rollback | PASS |
| Browser management and understandable policy UX | `/api/agent-grants`, `/api/grant-models`, `static/app.js`, labeled/grouped form in `index.html` | API/UI tests, JS syntax check, Playwright render, direct screenshot review | PASS |
| Provider credentials never reach agents | agent DTO omits credential bindings and secret metadata | agent API tests assert IDs/labels absent | PASS |

## Phase 4 — strict `/agent/v1/*` authentication

| Requirement | Implementation evidence | Validation evidence | State |
| --- | --- | --- | --- |
| Provider-neutral discovery and RFC 7662 | `pkg/byok/oidcauth/oidcauth.go` | protocol fixtures in `oidcauth_test.go` | PASS |
| Strict issuer/endpoint/resource/client/expiry/type/scope checks | `pkg/byok/urlpolicy` validates credential destinations before use; exact issuer and issuer-origin endpoint, active subject, client allowlist, exact audience, future expiry, Bearer type, required scope | URL-policy and positive/negative protocol tests | PASS |
| Safe confidential-client authentication | resource secret/file flags in `main.go`; POSIX descriptor-level no-follow bounded deployment-secret reads; RFC 6749 escaping before Basic encoding | symlink/size and reserved-character secret regression tests | PASS |
| Bounded cache without raw token retention | keyed HMAC digest cache, 1024-entry bound, token-expiry cap | cache hit/negative/expiry tests and source inspection | PASS |
| Correct error classes and fail-closed behavior | invalid token 401, insufficient scope 403, unavailable IdP 503 | agent API and introspection tests | PASS |
| Tiny tokens only on agent routes; broker tokens only on inference routes | separate mux/authenticators | unit route-separation test and live four-direction smoke | PASS |
| Operator-managed resource-client provisioning | `deploy/tinyidp/bootstrap.sh`; tiny-idp v0.0.5 adds atomic `--secret-file` input and introspection-only clients with no token grant types | clean-volume bootstrap and live introspection against the pinned release image | PASS |

## Phase 5 — RFC 8628 device client and broker exchange

| Requirement | Implementation evidence | Validation evidence | State |
| --- | --- | --- | --- |
| Strict RFC 8628 discovery/start/poll | `pkg/byok/deviceclient/client.go`, shared HTTPS/loopback-only URL policy, RFC 8707 `resource`, required scope, pending/slow/denial/expiry handling | destination, polling, terminal-error fixture tests and live tiny-idp flow | PASS |
| Pre-approved grant only | grant listing hides bindings; explicit requested ID; multiple grants never auto-selected | ambiguous/no/missing grant tests | PASS |
| Exact identity mapping | agent API resolves `(issuer, subject)` only | identity isolation and agent API tests | PASS |
| Atomic rotated scoped capability | `IssueAgentTokenAudited` derives all policy from grant and rotates by stable installation ID | issue/rotation/rollback/conformance tests; live exchange | PASS |
| Secure POSIX cache | `cache.go`, `cache_unix.go`: stable random ID persisted before login, flock, `O_NOFOLLOW`, descriptor checks, 0600/0700, atomic rename/fsync | lifecycle, retry stability, mode, deletion, symlink tests and focused race tests | PASS |
| Unsupported platforms fail closed | `cache_nonunix.go` | Windows cross-compilation and exact error contract | PASS |
| CLI lifecycle | `cmd/llm-proxy-server/cmds/byok/agent.go`, registered under `byok agent` | command package build, rendered help, underlying protocol/cache tests | PASS |
| No credential/token disclosure | CLI prints approval instruction and non-secret metadata, never the broker token; provider secret remains server-side | source scan and route/API tests | PASS |
| Real local device acceptance | production-shaped TLS stack with persistent trusted CA, real browser authentication and device approval, introspection, exchange, four-direction route check | transient harness emitted only final boolean success and was deleted; no provider dispatch occurred | PASS |
| Immutable tiny-idp deployment | `deploy/docker-compose.yaml` pins tiny-idp v0.0.5 commit `486a3e3108f3eeda3d100f3db613aecc74f4d13d` at OCI digest `sha256:d5d9b78ff2eb6adb2e6d984ee9e913bf9570eea38380f153ca87a8a639e9a629` | clean-volume pull/start/readiness/browser/device/exchange acceptance used that exact container image reference | PASS |

## Preserved constraints

| Constraint | Evidence | State |
| --- | --- | --- |
| Phase 0–2 behavior preserved | full repository tests, browser OIDC/session tests, schema v1→v2→v3 tests | PASS |
| Single-active-broker assumption remains explicit | SQLite/process-local accounting and deployment remain singleton; no horizontal claim added | PASS |
| Audit/accounting/introspection failures fail closed | trigger failure tests, meter circuit tests, unavailable-introspection tests | PASS |
| No unsupported compatibility claims | README explicitly excludes Responses, Anthropic-native Messages, arbitrary coding agents, and live providers | PASS |
| No live provider dispatch | acceptance ended at `/v1/models` authorization and did not invoke a provider | PASS |
| No mutable tiny-idp deployment shortcut | default Compose pins approved v0.0.5 by immutable OCI digest, not `latest` or a mutable development tag | PASS |
| Unrelated user changes preserved | no reset/checkout of the llm-proxy worktree or `ttmp/vocabulary.yaml`; tiny-idp PR built from a separate clean worktree and excludes unrelated `go.sum` changes | PASS |

## Fresh validation inventory

- `GOWORK=off go test ./... -count=1` passed in llm-proxy and tiny-idp.
- `GOWORK=off go test -race ./pkg/byok/... ./cmd/llm-proxy-server/... -count=1` passed.
- Tiny-idp command and targeted device/resource tests passed; its provider-clock expiry test has a pre-existing timing flake seen under race and once under a normal focused-package run, while complete PR CI passes. The caveat is recorded in the diary.
- llm-proxy lint, vet, Glazed lint, generated logcopter check, gosec policy, govulncheck, JS syntax, and `git diff --check` passed.
- tiny-idp lint, vet, Glazed lint, gosec, govulncheck, PR CI, and `git diff --check` passed.
- Source/diff scans found no credential, private-key, bearer-token, or client-secret material.
- Automated review on tiny-idp PR #15 identified bcrypt-length, RFC 8707 error-class/mixed-parameter, and secret-file TOCTOU issues; commits `18ff495`, `73dd644`, `c93f385`, `b66bdbc`, and `7a210d9` resolve them with shared boundary validation, descriptor-level no-follow bounded reads, key-presence checks, and HTTP-level regressions. Commit `2ae3b21` additionally removes every token grant type from introspection-only resource clients.
- Clean-volume local TLS/device acceptance passed against tiny-idp v0.0.5 at immutable OCI digest `sha256:d5d9b78ff2eb6adb2e6d984ee9e913bf9570eea38380f153ca87a8a639e9a629`: readiness returned 200, browser OIDC and grant creation succeeded, RFC 8628 approval exchanged through RFC 7662 into a cached capability, token routes returned `200/401/200/401` for tiny→agent/tiny→v1/llmp→v1/llmp→agent, grant revocation invalidated the child token, and CLI logout removed its mode-0600 cache. No provider request was dispatched.
- `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30` passed.

## Completion decision

All release gates are satisfied: PR #15 is merged, v0.0.5 is released, Compose mounts the fourth operator secret and pins the immutable digest, clean-volume acceptance passed, rendered config and runtime logs contained no secret values, and the complete repository validation suite passed. Canonical Phases 3–5 are complete without broadening compatibility claims.
