# Changelog

## 2026-07-22

Created ticket `LLM-PROXY-BYOK-TINYIDP`. Motivation: the `LLM-PROXY-BYOK-PROD-READINESS` ticket's first task is to stand up Keycloak compose and drive the full OIDC browser flow against `pkg/byok/web/oidc.go`. We have since built tiny-idp, a production-shaped embeddable OIDC provider, as part of the tiny-idp/tiny-idp-xapp work. This ticket redirects that prod-readiness task from Keycloak to tiny-idp.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/research/01-tiny-idp-integration-research-and-architect-onboarding-brief.md — Evidence-backed architect onboarding brief

## 2026-07-22

Wrote the research brief: current llm-proxy Keycloak integration points (oidc.go RP, main.go flags, docker-compose, realm import), tiny-idp embedding API (embeddedidp Options/Bootstrap/Provider/InProcessIssuerTransport, idpaccounts, sqlitestore, discovery shape, client model), three topology options with tradeoffs (in-process embedded, separate process, sidecar), file-by-file integration surface, migration sketch, and open questions for the architect. Relates 8 key source files across llm-proxy and tiny-idp.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/oidc.go — Current OIDC relying-party implementation
- /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/options.go — tiny-idp embedding API surface
- /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/inprocess_transport.go — In-process issuer transport

## 2026-07-22

Architecture takeover: verified llm-proxy and tiny-idp contracts, corrected the missing-PKCE assumption, selected a separate tiny-idp plus device-token-to-llmp capability exchange architecture, and added a 1,800+ line intern implementation guide with agent grants, cumulative budgets, audit, usage, metrics, phases, and tests.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/design-doc/01-tinyidp-byok-coding-agent-architecture-and-intern-implementation-guide.md — Primary architecture and implementation contract
- /home/manuel/code/wesen/go-go-golems/llm-proxy/ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/reference/01-implementation-diary.md — Chronological evidence, decisions, and review guidance


## 2026-07-22

Validated the architecture package (frontmatter and docmgr doctor clean), completed the required reMarkable dry run, and uploaded the four-document bundle to /ai/2026/07/22/LLM-PROXY-BYOK-TINYIDP.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/ttmp/2026/07/22/LLM-PROXY-BYOK-TINYIDP--replace-keycloak-with-tiny-idp-as-the-byok-control-plane-oidc-issuer/reference/01-implementation-diary.md — Validation and reMarkable delivery evidence


## 2026-07-22

Completed Phase 0: added validated forward-only SQLite user_version migrations; atomic typed audit for credential/token lifecycle including cascade revocations; committed-write metering health with transient/persistent classification, fail-closed 503 enforcement, single-probe recovery, readiness, and configuration; upgraded vulnerable toolchain/dependencies; all tests, race, build, lint, security, vulnerability, and local smoke checks pass.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/meter/health.go — Shared fail-closed circuit and committed recovery probe
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/store/audit.go — Closed typed lifecycle and meter circuit audit payloads
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/store/sqlite/schema.go — Forward-only migration runner and schema security validation


## 2026-07-22

Completed Phase 1 local deployment foundation: normalized roadmap numbering; replaced Keycloak Compose with Caddy/TLS, CA export, non-root tiny-idp v0.0.4 pinned by immutable digest, durable volumes, idempotent bootstrap, public browser/device clients, secret-file proxy configuration, and CA-verified readiness gating. The full ephemeral-secret Compose smoke passed; PKCE/browser login and confidential introspection client remain later phases.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/cmd/llm-proxy-server/main.go — File-based deployment secret resolution
- /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/docker-compose.yaml — Phase 1 pinned tiny-idp topology and readiness dependencies
- /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/tinyidp/bootstrap.sh — Idempotent local owner and public device-client bootstrap


## 2026-07-22

Completed core Phase 2 browser OIDC: schema-v2 issuer identities, one-time PKCE transactions, opaque revocable sessions, atomic auth/session audit, callback-order tests, and real tiny-idp browser acceptance. BYOK TLS now uses a bounded leaf issued from the retained workstation Caddy authority while long-running Caddy remains non-root; tiny-idp end-session remains open.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/tinyidp/issue-local-cert.sh — Shared persistent CA leaf issuance
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/oidc.go — PKCE and callback security boundary


## 2026-07-22

Completed Phase 2 logout: local audited session revocation now fails closed before redirecting through the discovered tiny-idp end-session endpoint; endpoint origin/path and the registered post-logout redirect are validated, and the dashboard exposes a same-origin POST sign-out action.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/oidc.go — Fail-closed local and provider logout sequence
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/static/index.html — Browser sign-out action


## 2026-07-23

Step 8: implemented and live-validated roadmap Phase 3 grant policy/accounting, Phase 4 strict RFC 7662 agent authentication, and Phase 5 RFC 8628 capability exchange/secure CLI cache; tiny-idp dependency is PR #15 and remains unmerged pending explicit approval.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/cmd/llm-proxy-server/cmds/byok/agent.go — Agent login status and logout CLI
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/agentapi/server.go — Strict agent grant listing and capability exchange boundary
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/deviceclient/client.go — RFC 8628 client and grant exchange
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/oidcauth/oidcauth.go — Provider-neutral RFC 7662 validation and bounded keyed cache
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/store/grants.go — Agent grant validation and issuance policy


## 2026-07-23

Step 9: pinned tiny-idp v0.0.5 immutable OCI digest and completed clean-volume browser/device/introspection/exchange/route/revocation acceptance (commit 7cebfac008d957a0d2a79d8fd8f158298d988e1b)

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/docker-compose.yaml — Immutable v0.0.5 image and Phase 4 resource-client wiring
- /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/tinyidp/bootstrap.sh — Introspection-only confidential client provisioning

## 2026-07-23

Step 10: validated one bounded real Umans GLM 5.2 Chat Completions request with durable usage, revocation, secret scan, credential deletion, and cleanup

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/README.md — Exact live-provider compatibility evidence and limits
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/engines/provider.go — Credential injection path exercised by the live smoke
