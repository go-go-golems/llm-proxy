# Tasks

## Canonical roadmap

The numbered implementation phases are the Phase 0–7 roadmap in
`design-doc/01-tinyidp-byok-coding-agent-architecture-and-intern-implementation-guide.md`
(§45–§52). The former “Phase 0 — Research,” “Phase 1 — Design,”
“Phase 2 — Implementation,” and “Phase 3 — Validation” headings described
workstreams completed before that roadmap; they are not implementation phase
numbers.

## Completed pre-roadmap discovery and design

- [x] Create the `LLM-PROXY-BYOK-TINYIDP` ticket workspace.
- [x] Inventory current Keycloak integration points in llm-proxy.
- [x] Inventory the tiny-idp embedding and production-host APIs.
- [x] Write and relate the architect onboarding research brief.
- [x] Decide separate tiny-idp service topology, public PKCE browser client,
  public device client, confidential resource client, and issuer-aware identity.
- [x] Map the design onto the prior BYOK production-readiness tasks.

## Roadmap Phase 0 — production prerequisites and migrations (complete)

- [x] Add forward-only BYOK SQLite schema migrations before issuer-aware
  identities, sessions, auth transactions, agent grants, and token provenance.
  <!-- t:ygm7 -->
- [x] Make existing credential/token lifecycle audit atomic; add committed-write
  meter health, fail-closed 503 enforcement, recovery, readiness,
  configuration, tests, and security validation. <!-- t:n4uv -->

## Roadmap Phase 1 — tiny-idp production-shaped Compose

- [x] Replace `deploy/docker-compose.yaml` Keycloak service with the approved
  tiny-idp v0.0.4 digest, Caddy, CA export, llm-proxy, durable volumes,
  non-root services, and readiness-gated startup.
- [x] Add the reviewed browser client catalog, theme catalog and stylesheet,
  startup configuration, and idempotent bootstrap job.
- [x] Bootstrap a local development account and public device client from
  Docker secrets only; never check in credentials or client secrets.
- [x] Remove `deploy/keycloak/realm-byok.json` after the replacement Compose
  smoke succeeds.
- [x] Ensure llm-proxy back-channel discovery uses the exported Caddy local CA
  and exact TLS issuer URL; do not weaken certificate verification.
- [x] Issue BYOK leaf certificates from the manually retained external
  `tinyidp-local-caddy-pki` authority through a bounded root-only job while
  keeping long-running Caddy non-root.
- [x] Write and run the Phase 1 deployment smoke/playbook: all services ready,
  no mutable image references, no secrets in rendered config/logs, and Caddy
  terminates browser TLS.
- [x] Provision the confidential resource/introspection client in Phase 4 with
  an operator-managed secret file and no token grant types.

## Roadmap Phase 2 — PKCE-correct browser OIDC

- [x] Implement PKCE S256 and one-time server-side OIDC auth transactions;
  migrate user identity to exact `(issuer, subject)`. <!-- t:d311 -->
- [x] Implement opaque server-side browser sessions with per-session
  revocation and idle/absolute expiry. <!-- t:7tno -->
- [x] Integrate discovered tiny-idp end-session with exact same-origin endpoint
  validation and the registered post-logout redirect; local revocation commits
  before browser navigation to the IdP.
- [x] Verify callback order: atomically consume state/auth transaction → code
  exchange with PKCE verifier → ID-token signature/audience/issuer → nonce →
  issuer-aware user/session creation.

## Roadmap Phase 3 — agent-grant domain

- [x] Implement browser-managed agent grants with credential/model bindings,
  per-token limits, cumulative grant budgets, and revocation cascades.
  <!-- t:9qwx -->

## Roadmap Phase 4 — IdP access-token introspection

- [x] Implement provider-neutral RFC 7662 introspection for `/agent/v1` with
  exact issuer, audience, client, expiry, token type, and scope validation.
  <!-- t:3rg8 -->

## Roadmap Phase 5 — device exchange and client CLI

- [x] Implement RFC 8628 agent CLI and secure token cache, then exchange
  tiny-idp identity tokens for rotated `llmp_...` capability tokens.
  <!-- t:gs8l -->

## Roadmap Phase 6 — grant accounting, audit, metrics, and UI

- [x] Extend usage accounting to cumulative agent-grant counters. Phase 0
  completed the fail-closed metering circuit breaker. <!-- t:ynm9 -->
- [ ] Add bounded-cardinality operational metrics and usage summaries. Phase 0
  completed typed lifecycle/circuit audit events and readiness integration.
  <!-- t:i4mq -->

## Roadmap Phase 7 — acceptance and release gate

- [ ] Write and run the tiny-idp OIDC and coding-agent acceptance playbooks.
- [ ] Update the `LLM-PROXY-BYOK` architecture proposal §Phase 3 to reflect
  tiny-idp as the IdP.
- [ ] Check off corresponding prior BYOK production-readiness tasks.
- [ ] Select and test one concrete OpenAI Chat Completions-compatible
  coding-agent client before claiming coding-agent support. <!-- t:mxrl -->
- [x] Run `docmgr doctor --ticket LLM-PROXY-BYOK-TINYIDP --stale-after 30`.
