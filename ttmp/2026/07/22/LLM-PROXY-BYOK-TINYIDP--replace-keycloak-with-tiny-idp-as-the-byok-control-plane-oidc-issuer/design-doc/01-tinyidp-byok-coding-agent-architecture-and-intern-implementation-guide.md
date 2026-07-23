---
Title: TinyIDP BYOK coding-agent architecture and intern implementation guide
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
    - metering
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/cmd/llm-proxy-server/main.go
      Note: |-
        Composition root for stores, vault, middleware, services, and control plane
        Deployment secret-file flags
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/authmw/middleware.go
      Note: |-
        Existing llmp bearer validation and budget preflight
        Existing llmp capability validation, rate, budget, and request-context boundary
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/engines/provider.go
      Note: |-
        Credential decryption and request-time Geppetto key injection
        Request-time credential decryption and fallback-key scrubbing boundary
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/meter/meter.go
      Note: |-
        Existing usage recorder and production fail-open gap
        Usage recorder and fail-open production gap
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/store/models.go
      Note: Existing users, credentials, tokens, ledger, counters, and audit domain model
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/store/sqlite/store.go
      Note: |-
        Existing unversioned SQLite schema and transactional usage accounting
        Schema migration, agent-grant, audit, and cumulative-counter implementation target
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/api.go
      Note: Existing credential, token, and usage management API
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/oidc.go
      Note: |-
        Existing browser OIDC RP; must gain PKCE and issuer-aware identity handling
        Current RP; source evidence for missing PKCE and issuer-only-subject identity
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/server/server.go
      Note: Actual OpenAI-compatible surface; currently models, completions, and chat completions only
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/cmd/tinyidp-xapp/device_cli.go
      Note: |-
        Working RFC 8628 client, polling behavior, and owner-only token cache reference
        Working RFC 8628 client and secure-cache reference
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/cmd/tinyidp-xapp/internal/resourceauth/resourceauth.go
      Note: |-
        Hardened reference for opaque token introspection and principal construction
        Hardened RFC 7662 resource-server reference
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/examples/tinyidp-shared-two-apps/compose.yaml
      Note: Production-shaped local HTTPS, durable IdP, client bootstrap, and trust-store reference
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/internal/oidcmeta/discovery.go
      Note: Discovery metadata for authorization, device, token, introspection, JWKS, and logout endpoints
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/bootstrap.go
      Note: Browser and device client security profiles
    - Path: repo://deploy/docker-compose.yaml
      Note: Validated Phase 1 Compose topology
    - Path: repo://deploy/tinyidp/Caddyfile
      Note: Browser TLS boundary and local CA source
    - Path: repo://deploy/tinyidp/bootstrap.sh
      Note: Idempotent non-secret bootstrap logic
    - Path: repo://pkg/byok/meter/health.go
      Note: Shared fail-closed metering circuit and committed recovery probe
    - Path: repo://pkg/byok/meter/health_test.go
      Note: Circuit threshold recovery concurrency and recorder tests
    - Path: repo://pkg/byok/store/audit.go
      Note: Typed lifecycle and meter-circuit audit payloads
    - Path: repo://pkg/byok/store/sqlite/lifecycle_test.go
      Note: Injected audit failure rollback evidence
    - Path: repo://pkg/byok/store/sqlite/schema.go
      Note: Phase 0 forward-only migration runner and schema constraint validator
ExternalSources:
    - https://openid.net/specs/openid-connect-core-1_0.html
    - https://www.rfc-editor.org/rfc/rfc7636.html
    - https://www.rfc-editor.org/rfc/rfc8628.html
    - https://www.rfc-editor.org/rfc/rfc7662.html
    - https://www.rfc-editor.org/rfc/rfc8707.html
    - https://www.rfc-editor.org/rfc/rfc6750.html
Summary: Detailed architecture and implementation guide for replacing Keycloak with a separately deployed tiny-idp, adding PKCE-correct browser login and RFC 8628 coding-agent identity, exchanging short-lived IdP access tokens for broker-owned scoped llmp capabilities, and preserving credential secrecy, revocation, audits, metering, budgets, and operational metrics.
LastUpdated: 2026-07-22T15:42:54-04:00
WhatFor: Give a new intern enough conceptual, API, file-level, security, migration, testing, and operational detail to implement and review the complete tiny-idp-backed BYOK coding-agent path.
WhenToUse: Read before implementing LLM-PROXY-BYOK-TINYIDP; use the phase plan, API contracts, and validation matrix during development and review.
---

















# TinyIDP BYOK coding-agent architecture and intern implementation guide

> **Audience.** This is an intern-facing implementation guide. It assumes
> familiarity with Go, HTTP, JSON, and basic OAuth vocabulary, but no prior
> knowledge of llm-proxy, tiny-idp, Geppetto, or this ticket. Read Parts I–IV
> before changing code. Use Parts V–IX as the working contract during
> implementation.

## Executive summary

The finished system lets a person authenticate through tiny-idp, store an LLM
provider credential in llm-proxy's encrypted vault, define a reusable coding-
agent grant, and approve a terminal through OAuth Device Authorization. The
terminal receives a short-lived tiny-idp access token proving identity and
consent. It exchanges that token at llm-proxy for an opaque `llmp_...` token
whose model allowlist, credential bindings, expiry, request budget, token
budget, and rate limit are owned and enforced by the broker. The coding agent
uses the `llmp_...` token as an OpenAI-compatible API key. It never receives the
provider credential.

The system deliberately has two token classes:

1. A **tiny-idp access token** answers “who approved this terminal, for which
   audience and OAuth scopes?” It is short-lived and accepted only on
   `/agent/v1/*` acquisition endpoints.
2. An **llm-proxy capability token** answers “which stored credential and model
   profiles may this agent use, under which budgets?” It is accepted only on
   `/v1/*` inference endpoints.

Do not collapse these tokens. Identity-provider scopes cannot represent all of
the broker's mutable domain policy, and an `llmp_...` token must remain
instantly revocable and metered in llm-proxy's database.

The selected topology is a **separate tiny-idp service**, deployed beside
llm-proxy in local Compose and as an independently operated singleton in the
first production deployment. The services have separate SQLite databases and
separate secret sets. They communicate through standards: browser OIDC,
RFC 8628 Device Authorization, OIDC discovery, RFC 8707 resource indicators,
and RFC 7662 introspection. llm-proxy does not import tiny-idp internals.

There is one blocking correction to the prior research brief. The current
`pkg/byok/web/oidc.go` implements state and nonce but **does not implement
PKCE**. `gooidc.Nonce(nonce)` is an OIDC nonce option; it is not a PKCE
challenge. tiny-idp's `BrowserClient` requires PKCE. The RP must generate a
verifier, send `oauth2.S256ChallengeOption(verifier)` at authorization, and
send `oauth2.VerifierOption(verifier)` during code exchange. A production
implementation should store the verifier in a one-time server-side auth
transaction rather than another self-contained browser cookie.

The existing BYOK data plane already contains the useful core:

- AES-256-GCM provider-key storage in `pkg/byok/vault`;
- one-time plaintext `llmp_...` token minting and hash-only persistence in
  `pkg/byok/tokens`;
- revocation, expiry, rate, and budget checks in `pkg/byok/authmw`;
- model filtering and request-time credential injection through
  `pkg/byok/engines.VaultEngineProvider`;
- authoritative post-inference usage recording in `pkg/byok/meter`;
- a usage ledger, denormalized counters, and audit events in the BYOK store.

The integration must extend those pieces, not replace them.

---

# Part I — The system and its security model

## 1. What the product does

A user owns one or more provider credentials: for example, an Anthropic API key
and an OpenAI API key. The user stores those credentials in llm-proxy. The
browser sends the plaintext credential once over HTTPS; llm-proxy encrypts it
with AES-GCM before persistence. The provider key is decrypted only while
building a request-specific Geppetto engine.

A coding agent does not receive those keys. Instead, the user creates an agent
grant such as:

```text
Name:                 laptop-coding-agent
Credentials:          personal-openai, personal-anthropic
Allowed models:       gpt-responses, sonnet
Per-token lifetime:   8 hours
Rate limit:           30 requests/minute
Grant request budget: 2,000 requests
Grant token budget:   5,000,000 input+output tokens
Max active tokens:    1 per client instance
```

The grant is a reusable policy template and cumulative accounting boundary. A
new `llmp_...` token inherits its restrictions, but reacquiring a token does not
reset the grant's cumulative budget.

## 2. Actors

| Actor | What it owns | What it may see | What it must not see |
| --- | --- | --- | --- |
| Human user | IdP account, provider credentials, grants | Own credentials' labels/suffixes, token metadata, own usage | Other users' objects |
| Coding agent | Device instance ID, acquired `llmp_...` token, prompts/results | Allowed model list, own inference results, optional own usage | Provider key, vault ciphertext, other grants |
| tiny-idp | Identity accounts, login state, OAuth clients, device grants, opaque IdP tokens | Authentication and consent events | Provider API keys and LLM prompts |
| llm-proxy control plane | BYOK users, encrypted credentials, grants, sessions | Management requests and token issuance | Plaintext secrets after request completion |
| llm-proxy data plane | `llmp_...` token hash lookup, policy, decrypted key for one request | Prompts, completions, provider usage | Raw IdP client secret in responses/logs |
| Upstream LLM provider | Provider credential and inference content | Provider-authenticated request | tiny-idp token, `llmp_...` token, grant metadata |
| Operator | Deployment secrets and databases | Operational metrics and audited administrative actions | Plaintext credentials in normal diagnostics |

## 3. Trust boundaries

```text
                Browser / terminal (untrusted token holders)
                          |                  |
              browser OIDC|                  |RFC 8628 device flow
                          v                  v
                 +-------------------------------+
                 | tiny-idp identity service     |
                 | accounts, consent, OAuth      |
                 | device grants, IdP audit      |
                 +---------------+---------------+
                                 |
                 RFC 7662 introspection (Basic-authenticated,
                 exact issuer + audience + scope validation)
                                 |
                                 v
+-----------------------------------------------------------------------+
| llm-proxy                                                            |
|                                                                       |
|  control plane                    agent acquisition plane              |
|  /app, /api/*                     /agent/v1/*                          |
|  browser session                  tiny-idp bearer token                |
|         |                                  |                           |
|         +----------------+-----------------+                           |
|                          v                                             |
|          users, encrypted credentials, agent grants,                  |
|          capability tokens, sessions, audit, ledger, counters         |
|                          |                                             |
|                          v                                             |
|  data plane /v1/* -- Authorization: Bearer llmp_...                   |
|  policy -> profile -> decrypt key -> Geppetto -> usage recorder       |
+--------------------------------------+--------------------------------+
                                       |
                                       v
                              upstream LLM providers
```

The IdP and broker are trusted for different purposes. tiny-idp establishes
identity and interactive consent. llm-proxy owns LLM authorization because only
llm-proxy understands credential IDs, profile slugs, provider compatibility,
budgets, and usage.

## 4. Security invariants

These invariants are acceptance criteria, not suggestions:

- A browser, terminal, or coding agent never receives a provider credential
  after initial credential enrollment.
- `/v1/*` accepts only recognizable `llmp_...` tokens. It never treats a
  tiny-idp token or provider key as a fallback credential.
- `/agent/v1/*` accepts only a validated tiny-idp access token with the exact
  configured issuer, audience, allowed device client, unexpired bearer type,
  and required scope.
- No endpoint accepts both token classes. Route separation prevents confused
  deputy behavior.
- The identity key is `(issuer, subject)`, not `subject` alone.
- Browser authorization uses state, nonce, exact redirect matching, and PKCE
  S256. Auth transactions are one-time and expire in ten minutes.
- A device access token does not authorize arbitrary credential or model
  selection. It authorizes issuance from a pre-existing user-owned agent grant.
- Reissuing for the same grant and client instance revokes the prior active
  capability token before returning the replacement.
- Reissuing never resets grant-level counters.
- Provider keys, IdP access tokens, `llmp_...` plaintext tokens, PKCE verifiers,
  authorization codes, client secrets, and session secrets never enter logs,
  metrics labels, audit payloads, or checked-in fixtures.
- Failed audit delivery for a privileged control-plane mutation fails closed or
  is committed atomically with that mutation. Best-effort audit is not an
  acceptable production policy for credential and token lifecycle actions.
- Persistent metering failure opens a circuit that rejects new inference before
  more unaccounted upstream spend occurs.
- Live LLM-provider smoke tests require explicit approval and use a redacted,
  bounded plan. CI uses a fake local provider.

## 5. Honest compatibility boundary

The current HTTP server exposes only:

- `GET /v1/models`;
- `POST /v1/completions`;
- `POST /v1/chat/completions`.

This is verified in `pkg/server/server.go:Handler`. It does not expose
`POST /v1/responses` or Anthropic's native `/v1/messages` API. Therefore the
first supported coding-agent target must be an agent configurable for an
OpenAI-compatible **Chat Completions** base URL. Do not claim compatibility
with every coding agent.

A profile named `gpt-responses` can use Geppetto's OpenAI Responses engine
upstream while llm-proxy still accepts Chat Completions from the client. That
is different from exposing the OpenAI Responses wire API. A Codex client that
requires `/v1/responses` needs a separate adapter and acceptance suite.

---

# Part II — What exists today

## 6. llm-proxy composition

`cmd/llm-proxy-server/main.go:runServer` is the composition root.

When `--byok-db` is configured, it:

1. Opens `pkg/byok/store/sqlite`.
2. Opens the AES-GCM vault from `--byok-master-key`.
3. Constructs `VaultEngineProvider` for per-request credential injection.
4. Constructs `meter.Recorder` for post-inference usage.
5. Wraps the model lister with `authmw.ScopedModelLister`.
6. Wraps the data-plane handler with `authmw.TokenAuth`.
7. Optionally mounts the browser control plane when a session secret exists.

This is a good boundary. New components should be injected here:

```go
identityAuthenticator := oidcauth.NewIntrospector(...)
agentService := agenttokens.NewService(store, identityAuthenticator, audit)
meterHealth := meter.NewHealth(...)

webServer, err := web.NewServer(ctx, web.Config{
    Store: store,
    Vault: vault,
    OIDC: oidcConfig,
    AgentTokens: agentService,
})
```

## 7. Browser OIDC RP: implemented boundary and remaining gap

Phase 2 is implemented in `pkg/byok/web/oidc.go`, `session.go`, and the store
backends:

- discovery uses `gooidc.NewProvider` and scopes `openid profile email`;
- authorization uses a server-side one-time transaction with hashed browser ID
  and state, nonce, PKCE verifier, sanitized return target, and expiry;
- authorization sends an S256 challenge and code exchange sends the stored
  verifier only after atomic transaction consumption;
- ID-token signature, audience, and issuer are verified before nonce comparison;
- identities are upserted by exact `(issuer, subject)`;
- cookies contain only a signed opaque session ID whose hash resolves a
  revocable server-side session with idle and absolute expiry;
- users can list sessions and revoke one by its non-secret public ID;
- successful callbacks expire the short-lived auth-transaction cookie;
- logout revokes the current server-side session before clearing its cookie,
  then navigates through the discovered, same-origin-validated tiny-idp
  end-session endpoint with the exact registered post-logout redirect;
- the real Compose browser flow completed against tiny-idp over host-verified
  TLS without disabling certificate checks.

Remaining browser-control-plane gaps:

- **Proxy trust is deployment-bound.** `isSecureRequest` trusts
  `X-Forwarded-Proto: https`; the Compose backend is private, but a future
  directly exposed deployment must normalize or reject untrusted forwarding
  headers at its ingress boundary.

## 8. Existing capability token and enforcement

`pkg/byok/tokens.Mint` generates 256 random bits, prefixes `llmp_`, returns the
plaintext once, and persists only SHA-256 hex. This is appropriate for a
high-entropy opaque token.

`pkg/byok/authmw.TokenAuth` currently performs:

```text
extract bearer -> hash lookup -> revoked/expired -> per-token lock
-> rate limit -> counter read -> budget check -> touch last_used_at
-> attach Token to context -> continue
```

The per-token lock remains held until the request returns. That serializes
inferences for one token so concurrent requests cannot all pass a stale budget
pre-check. It also means one slow stream blocks the same token's next request.
This is acceptable for the first single-instance deployment but must be
measured and documented.

The limiter and lock maps are process-local and never pruned. Multi-instance
llm-proxy deployment is therefore unsupported until policy reservation moves
into a shared transactional store.

## 9. Existing provider-key injection

`pkg/byok/engines.VaultEngineProvider` is the main credential boundary. It:

- requires a validated token in context;
- checks the model/profile allowlist again;
- reads the profile's Geppetto `api_type`;
- selects the first enabled token-bound credential with a matching API type;
- decrypts the credential using its ID as AES-GCM AAD;
- clones profile settings;
- replaces the complete API-key map rather than merging it;
- clears chat fallback keys;
- asks the inner Geppetto factory to create the engine.

The replacement behavior is essential: a server-side key from profile YAML
must never subsidize a BYOK caller.

## 10. Existing accounting and audit

`pkg/byok/meter.Recorder` records input, output, and cache tokens after the
provider call. It uses `context.WithoutCancel` so a disconnected streaming
client is still billed. `pkg/byok/store/sqlite.RecordUsage` inserts a ledger
row and increments denormalized token counters in one transaction. Rejected
model decisions produce `status=rejected` rows but do not advance counters.

The production gap is error handling. `meter.Recorder` logs a write failure and
returns nothing. If the database stays unhealthy, requests continue while
budgets stop advancing. The `LLM-PROXY-BYOK-PROD-READINESS` ticket already
identifies this as a blocker.

Audit events currently cover credential creation/deletion, token mint/revoke,
and some inference rejection paths. Audit appends from web and CLI lifecycle
code are best-effort. Successful inference is represented in the usage ledger,
not duplicated into the audit table. This split is sound, but privileged
mutation audit must become transactional or fail closed.

## 11. tiny-idp contracts we will use

The source tree at
`/home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp` currently has module
`github.com/go-go-golems/tiny-idp`, HEAD `f640eb6`, and published tags through
`v0.0.3`. The production-shaped work is newer than the last tag. Deployment
must pin an approved tag or image digest; it must not silently build an
arbitrary branch head.

Relevant contracts:

- `embeddedidp.BrowserClient` creates a public authorization-code + refresh
  client with PKCE required.
- `embeddedidp.DeviceClient` creates a public device-code client with no
  redirect URI.
- A device client can have exact `AllowedAudiences` resource indicators and
  custom allowed scopes.
- A confidential `idpstore.Client` with `CanIntrospect=true` and an exact
  allowed audience authenticates at RFC 7662 introspection using
  `client_secret_basic`.
- Discovery advertises authorization, device authorization, token, userinfo,
  introspection, JWKS, and end-session endpoints.
- `cmd/tinyidp-xapp/device_cli.go` is a working client reference for
  `authorization_pending`, `slow_down`, expiry, and mode-0600 token caches.
- `cmd/tinyidp-xapp/internal/resourceauth/resourceauth.go` is a hardened
  resource-server reference. It verifies discovery issuer equality,
  introspection endpoint origin, Basic support, `active`, `iss`, `sub`, `aud`,
  `exp`, token type, and scopes. llm-proxy should reproduce this provider-
  neutral contract in its own public package; it must not import a `cmd/.../internal`
  package.

---

# Part III — Selected architecture

## 12. Topology decision

The initial production topology is two services behind normal HTTPS routing:

```text
https://idp.example.test  -> tiny-idp singleton -> tiny-idp.sqlite + audit.jsonl
https://llm.example.test  -> llm-proxy singleton -> byok.sqlite + vault key
```

Local development should use the same shape with Caddy and a local CA, following
`examples/tinyidp-shared-two-apps/compose.yaml` rather than falling back to
Keycloak or an insecure exposed dev issuer.

The services do **not** share a database. Their only join key is the validated
OIDC identity `(issuer, subject)`.

### Why not embed tiny-idp now?

Embedding is technically supported, but it would:

- couple release cadence and Go module versions;
- place identity keys and LLM vault authority in one process;
- increase the blast radius of an RCE;
- require special in-process HTTP wiring in `go-oidc` and introspection;
- make it easier to bypass the standards boundary accidentally.

Separate deployment exercises the actual protocols we need and keeps the
integration provider-neutral.

## 13. Two tokens, two planes

```text
Token A: tiny-idp opaque access token
  issuer:   https://idp.example.test
  subject:  user's stable tiny-idp subject
  client:   llm-proxy-agent
  audience: https://llm.example.test/agent/v1
  scopes:   openid llm.tokens.issue
  lifetime: about 1 hour
  accepted: /agent/v1/* only

Token B: llm-proxy opaque capability
  format:   llmp_<256 random bits>
  storage:  SHA-256 digest only
  owner:    BYOK user mapped from (issuer, subject)
  policy:   agent grant + credential/model/budget/rate restrictions
  lifetime: grant policy, recommended 8 hours initially
  accepted: /v1/* only
```

Token A proves a recent IdP-mediated approval. Token B is the actual LLM API
credential. The agent should discard Token A after exchange unless it needs to
list grants or reacquire during its one-hour lifetime.

## 14. Browser setup flow

The human setup path remains browser-based because adding and selecting LLM
credentials is a high-value action that deserves an interactive control plane.

```text
Browser        llm-proxy RP        tiny-idp          BYOK DB
   | GET /app       |                  |                 |
   |--------------->|                  |                 |
   | 302 /authorize + state + nonce + S256 challenge    |
   |<---------------|                  |                 |
   |---------------------------------->| login/consent   |
   |<----------------------------------| code + state    |
   | callback        |                  |                 |
   |--------------->| consume auth tx  |                 |
   |                 | POST /token(code, verifier)       |
   |                 |----------------------------------->|
   |                 | verify ID token + nonce           |
   |                 | upsert user by (iss, sub)-------->|
   |                 | create server session------------>|
   | session cookie  |                  |                 |
   |<---------------|                  |                 |
   | POST credential (secret write-only)                 |
   |--------------->| encrypt + atomic audit----------->|
   | POST agent grant                  |                 |
   |--------------->| validate credentials/models------>|
```

## 15. Coding-agent acquisition flow

```text
Agent CLI          tiny-idp            Browser/User       llm-proxy
   | POST /device_authorization             |                 |
   | client_id, audience, scope              |                 |
   |--------------------->|                  |                 |
   | device_code, user_code, verification URI                  |
   |<---------------------|                  |                 |
   | print URI + code     |                  |                 |
   |                      |<-----------------| open URI        |
   |                      | authenticate + approve             |
   | poll /token          |                  |                 |
   |--------------------->|                  |                 |
   | tiny access token    |                  |                 |
   |<---------------------|                  |                 |
   | GET /agent/v1/grants Authorization: Bearer <tiny token>   |
   |---------------------------------------------------------->|
   |                      |<-- RFC 7662 introspection (Basic) --|
   | eligible grants                                           |
   |<----------------------------------------------------------|
   | POST /agent/v1/tokens {grant_id, client_instance_id}      |
   |---------------------------------------------------------->|
   |                      |<-- fresh/cached introspection ------|
   |                      | map (iss,sub), rotate old token,    |
   |                      | mint llmp capability, atomic audit  |
   | llmp token shown once + metadata                           |
   |<----------------------------------------------------------|
   | write mode-0600 cache; discard tiny token                  |
```

## 16. Inference flow

```text
Coding agent      llm-proxy auth/policy      vault/Geppetto      provider
    | POST /v1/chat/completions |                   |               |
    | Bearer llmp_...           |                   |               |
    |-------------------------->| hash lookup       |               |
    |                           | token + grant live?                |
    |                           | rate/budget/circuit checks         |
    |                           | profile allowed?   |               |
    |                           | load + decrypt credential          |
    |                           |------------------->|               |
    |                           | clone settings, replace key        |
    |                           |------------------------------->    |
    |                           |<-------------------------------    |
    |                           | record usage + counters atomically |
    |<--------------------------| response / stream                  |
```

The raw `llmp_...` token is not forwarded. The provider receives only its own
credential in the provider-specific header created by Geppetto.

---

# Part IV — Domain model and API contracts

## 17. Identity must be issuer-aware

OIDC defines `sub` relative to `iss`. Change the user storage contract from:

```go
GetUserBySubject(ctx, subject string)
```

to:

```go
type ExternalIdentity struct {
    Issuer  string
    Subject string
}

GetUserByIdentity(ctx context.Context, issuer, subject string) (User, error)
UpsertUser(ctx context.Context, user User) (User, error) // unique (issuer, subject)
```

Suggested schema migration:

```sql
ALTER TABLE users ADD COLUMN oidc_issuer TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX idx_users_oidc_identity
  ON users(oidc_issuer, oidc_subject);
```

Do not apply this with ad hoc `ALTER TABLE` inside the current DDL. First add a
forward-only schema migration runner (`PRAGMA user_version` or a migration
table), as already required by `LLM-PROXY-BYOK-HARDENING`.

For an existing database, the migration command must require the operator to
supply the legacy issuer. An empty issuer is not a valid production identity.

## 18. Server-side sessions and auth transactions

### Browser sessions

```go
type Session struct {
    ID         string
    UserID     string
    CreatedAt  time.Time
    LastSeenAt time.Time
    ExpiresAt  time.Time
    RevokedAt  *time.Time
}
```

The browser cookie carries a random session ID plus an HMAC, not all user
claims. Every authenticated control-plane request loads the session and user.
Absolute lifetime and idle lifetime are enforced server-side. Logout revokes
that session, clears the cookie, and may redirect through tiny-idp's
`end_session_endpoint`.

### OIDC auth transactions

```go
type AuthTransaction struct {
    IDHash       string    // hash of random browser cookie value
    StateHash    string
    Nonce        string
    PKCEVerifier string    // short-lived, never logged
    ReturnTo     string
    ExpiresAt    time.Time
    ConsumedAt   *time.Time
}
```

`ConsumeAuthTransaction(cookie, state)` must atomically validate expiry and
state and mark/delete the row. The callback then exchanges the code with the
stored verifier.

Pseudocode:

```go
verifier := oauth2.GenerateVerifier()
tx := NewAuthTransaction(
    randomCookie(), randomState(), randomNonce(), verifier, returnTo,
)
store.CreateAuthTransaction(ctx, tx)
url := oauthConfig.AuthCodeURL(
    tx.State,
    gooidc.Nonce(tx.Nonce),
    oauth2.S256ChallengeOption(verifier),
)

// callback
stored := store.ConsumeAuthTransaction(ctx, cookieValue, queryState)
token, err := oauthConfig.Exchange(
    ctx, queryCode,
    oauth2.VerifierOption(stored.PKCEVerifier),
)
verifyIDTokenAndNonce(token, stored.Nonce)
```

## 19. Agent grants

An agent grant is a user-approved template and cumulative budget boundary.

```go
type AgentGrant struct {
    ID                     string
    UserID                 string
    Name                   string
    CredentialIDs          []string
    AllowedModels          []string
    PerTokenMaxTokens      *int64
    PerTokenMaxRequests    *int64
    RateLimitRPM           *int64
    TokenTTL               time.Duration
    MaxActivePerInstance   int
    GrantMaxTokens         *int64
    GrantMaxRequests       *int64
    Enabled                bool
    CreatedAt              time.Time
    UpdatedAt              time.Time
    RevokedAt              *time.Time
}

type AgentGrantCounters struct {
    GrantID       string
    TotalTokens   int64
    TotalRequests int64
}
```

Rules:

- Every credential belongs to the grant owner and is enabled.
- Every allowed model resolves to a known profile or an explicitly accepted
  glob. The UI should prefer concrete profile selection over free-form globs.
- `TokenTTL` has an operator-configured maximum; recommend 8 hours initially.
- Disabling/revoking a grant revokes every child token transactionally.
- Deleting a credential disables or rewrites affected grants and revokes child
  tokens; do not leave a grant that silently falls back to another user's or a
  server-side credential.
- Usage updates both token counters and grant counters in one transaction.
- Grant budget checks happen in addition to token budget checks.

Suggested tables:

```sql
CREATE TABLE agent_grants (
  id                       TEXT PRIMARY KEY,
  user_id                  TEXT NOT NULL REFERENCES users(id),
  name                     TEXT NOT NULL,
  credential_ids           TEXT NOT NULL,
  allowed_models           TEXT NOT NULL,
  per_token_max_tokens     INTEGER,
  per_token_max_requests   INTEGER,
  rate_limit_rpm           INTEGER,
  token_ttl_seconds        INTEGER NOT NULL,
  max_active_per_instance  INTEGER NOT NULL DEFAULT 1,
  grant_max_tokens         INTEGER,
  grant_max_requests       INTEGER,
  enabled                  INTEGER NOT NULL DEFAULT 1,
  created_at               TIMESTAMP NOT NULL,
  updated_at               TIMESTAMP NOT NULL,
  revoked_at               TIMESTAMP
);
CREATE INDEX idx_agent_grants_user ON agent_grants(user_id);

CREATE TABLE agent_grant_counters (
  grant_id       TEXT PRIMARY KEY REFERENCES agent_grants(id),
  total_tokens   INTEGER NOT NULL DEFAULT 0,
  total_requests INTEGER NOT NULL DEFAULT 0
);
```

## 20. Token provenance and rotation

Extend `store.Token` and the `tokens` table:

```go
type IssueChannel string
const (
    IssueChannelWeb    IssueChannel = "web"
    IssueChannelCLI    IssueChannel = "operator_cli"
    IssueChannelDevice IssueChannel = "device_exchange"
)

AgentGrantID     string
IssueChannel     IssueChannel
SourceClientID   string // tiny-idp device client; non-secret
ClientInstanceID string // random stable local installation ID; non-secret
```

For device exchange, the transaction should:

1. Load and validate the owner and grant.
2. Validate grant-level counters.
3. Revoke prior active child tokens for the same
   `(user, grant, source_client, client_instance)` until within the active cap.
4. Mint the new hash-only token row.
5. Append `token.device_issued` and any `token.rotated` audit events.
6. Commit.
7. Return plaintext once.

This gives retry-safe behavior. If the first response is lost, retrying with the
same stable client instance revokes the unreachable first token and returns a
new sole active token. No plaintext token needs to be stored for replay.

## 21. Agent resource authentication

Create `pkg/byok/oidcauth`, modeled on tiny-idp's resource-auth reference but
provider-neutral.

```go
type Config struct {
    IssuerURL       string
    ResourceClientID string
    ClientSecret    []byte
    Audience        string
    AllowedClients  []string // e.g. llm-proxy-agent
    HTTPClient      *http.Client
    PositiveCacheTTL time.Duration
    NegativeCacheTTL time.Duration
}

type Principal struct {
    Issuer    string
    Subject   string
    ClientID  string
    Scopes    []string
    ExpiresAt time.Time
}

Authenticate(ctx, authorizationHeaders, requiredScopes) (Principal, Outcome)
```

Validation order:

1. Exactly one syntactically valid bearer header.
2. Discovery document issuer equals configured canonical issuer.
3. Introspection endpoint is on the configured issuer origin.
4. Discovery advertises `client_secret_basic` introspection.
5. POST token to introspection using the owner-only resource client secret.
6. Response is active.
7. `iss` equals configured issuer.
8. `sub` is non-empty.
9. `client_id` is in the allowed device-client set.
10. `aud` contains the exact agent API audience.
11. `token_type` is Bearer.
12. `exp` is in the future.
13. Required scope `llm.tokens.issue` is present.

Cache keys are HMAC digests of raw access tokens using an in-memory random key;
raw tokens are never map keys. For token issuance, use a short positive cache
(0–5 seconds) or no cache if the implementation supports it. The cache TTL is
a documented IdP revocation-latency bound.

An unavailable introspection service returns `503`, not `401`. Invalid identity
returns `401`; missing scope returns `403`. Do not turn introspection failures
into a token oracle with detailed external errors.

## 22. HTTP API

### Browser control plane

Existing APIs remain, with agent grants added:

| Method | Path | Authentication | Purpose |
| --- | --- | --- | --- |
| GET | `/api/credentials` | browser session | List labels and suffixes only |
| POST | `/api/credentials` | browser session + CSRF/origin | Store a write-only provider credential |
| DELETE | `/api/credentials/{id}` | browser session + CSRF/origin | Delete and cascade policy invalidation |
| GET | `/api/tokens` | browser session | List capability metadata and counters |
| POST | `/api/tokens` | browser session + CSRF/origin | Manual token mint |
| POST | `/api/tokens/{id}/revoke` | browser session + CSRF/origin | Immediate token revocation |
| GET | `/api/usage` | browser session | Raw usage rows for owned token |
| GET | `/api/agent-grants` | browser session | List grants and cumulative usage |
| POST | `/api/agent-grants` | browser session + CSRF/origin | Create a grant |
| PATCH | `/api/agent-grants/{id}` | browser session + CSRF/origin | Tighten/update policy |
| POST | `/api/agent-grants/{id}/revoke` | browser session + CSRF/origin | Revoke grant and child tokens |

Example grant creation request:

```json
{
  "name": "laptop-coding-agent",
  "credential_ids": ["credential-id"],
  "allowed_models": ["gpt-responses", "sonnet"],
  "per_token_max_total_tokens": 500000,
  "per_token_max_requests": 500,
  "rate_limit_rpm": 30,
  "token_ttl_seconds": 28800,
  "grant_max_total_tokens": 5000000,
  "grant_max_requests": 2000,
  "max_active_per_instance": 1
}
```

Response never includes a provider secret or token plaintext.

### Agent acquisition plane

| Method | Path | Authentication | Scope | Purpose |
| --- | --- | --- | --- | --- |
| GET | `/agent/v1/grants` | tiny-idp bearer | `llm.tokens.issue` | List eligible grants and non-secret policy |
| POST | `/agent/v1/tokens` | tiny-idp bearer | `llm.tokens.issue` | Issue/rotate an `llmp_...` capability |
| GET | `/agent/v1/tokens/{id}/usage` | tiny-idp bearer | `llm.usage.read` | Optional owner usage view |
| POST | `/agent/v1/tokens/{id}/revoke` | tiny-idp bearer | `llm.tokens.revoke` | Optional agent-initiated revocation |

Initial scope can be only `llm.tokens.issue`; usage/revoke can remain browser-
only until there is a concrete CLI need.

Token issuance request:

```json
{
  "grant_id": "grant-id",
  "client_instance_id": "random-stable-installation-id",
  "name": "workstation-codex"
}
```

Token issuance response:

```json
{
  "id": "token-id",
  "token": "<capability plaintext shown once>",
  "expires_at": "2026-07-22T23:00:00Z",
  "allowed_models": ["gpt-responses", "sonnet"],
  "max_total_tokens": 500000,
  "max_requests": 500,
  "rate_limit_rpm": 30,
  "base_url": "https://llm.example.test/v1"
}
```

Use a generic response externally; never return credential IDs to the agent
unless a reviewed client use case needs them.

### Data plane

No token-exchange behavior is added to `/v1/*`.

```http
POST /v1/chat/completions HTTP/1.1
Host: llm.example.test
Authorization: Bearer llmp_<capability>
Content-Type: application/json

{"model":"gpt-responses","messages":[{"role":"user","content":"..."}]}
```

## 23. Coding-agent CLI

Add a dedicated command group. It may live initially under
`llm-proxy-server agent`, but a small standalone `llm-proxy-agent` binary is a
better eventual UX.

Suggested commands:

```text
llm-proxy-server agent login
  --issuer https://idp.example.test
  --broker https://llm.example.test
  --client-id llm-proxy-agent
  --audience https://llm.example.test/agent/v1
  --grant laptop-coding-agent
  --cache ~/.config/llm-proxy/agent.json

llm-proxy-server agent status --cache ...
llm-proxy-server agent revoke --cache ...
llm-proxy-server agent print-env --cache ...   # explicit plaintext output
```

The cache is an atomic mode-0600 file in a mode-0700 directory. It contains the
`llmp_...` token, expiry, broker base URL, token ID, and client instance ID. It
must not contain the provider credential. On non-POSIX systems, do not claim
permission guarantees without native tests; use an OS credential store or fail
closed.

Pseudocode:

```go
discovery := DiscoverAndValidate(issuer)
grant := StartDeviceAuthorization(
    discovery.DeviceEndpoint,
    clientID,
    scopes=["openid", "llm.tokens.issue"],
    audience=agentAudience,
)
PrintVerificationURI(grant) // no device_code
idpToken := PollWithPendingAndSlowDown(grant)
grants := ListAgentGrants(broker, idpToken)
selected := ResolveGrantFlagOrPrompt(grants)
capability := ExchangeForCapability(broker, idpToken, selected, instanceID)
AtomicWrite0600(cache, capability)
ZeroOrDiscard(idpToken)
```

For a generic OpenAI-compatible coding agent:

```text
OPENAI_BASE_URL=https://llm.example.test/v1
OPENAI_API_KEY=<read from owner-only agent cache>
OPENAI_MODEL=gpt-responses
```

Do not print the token by default. `print-env` is an explicit operator action
because shell output can enter scrollback or process logs.

## 24. Credential type registry

The current credential API accepts arbitrary `provider` and `api_type` strings.
A production setup should expose a server-owned registry derived from allowed
Geppetto profiles:

```go
type CredentialType struct {
    Provider    string
    APIType     string
    DisplayName string
    SecretKind  string // api_key initially
}
```

At credential creation:

- reject unknown provider/API-type pairs;
- cap label and secret lengths;
- never echo the secret;
- optionally run a separate explicit credential test against a harmless
  provider endpoint, only with user consent;
- store the key encrypted even if the test fails;
- avoid logging provider error bodies that may reflect credentials.

The first release supports static API keys. tiny-idp OAuth authenticates the
human and terminal; it is unrelated to provider OAuth. Renewable provider
credentials from Geppetto v0.13.7 are a later integration after llm-proxy is
upgraded from its current Geppetto v0.13.4 and an exact provider contract is
approved. Do not guess provider endpoints, scopes, or refresh rotation.

---

# Part V — Audit, accounting, and operational metrics

## 25. Three observability records, three purposes

Do not use one table for everything.

| Record | Purpose | Cardinality | Retention |
| --- | --- | --- | --- |
| Audit event | Who changed security-sensitive state and what decision was made | Low/medium | Long, append-only |
| Usage ledger | Authoritative per-inference accounting and billing evidence | High | Policy-driven |
| Operational metric | Aggregate service health, latency, and failure alerting | Bounded labels | Monitoring retention |

### Audit event examples

```text
oidc.login.succeeded
oidc.login.failed
session.created
session.revoked
credential.created
credential.deleted
agent_grant.created
agent_grant.updated
agent_grant.revoked
token.device_issued
token.rotated
token.revoked
agent.introspection.unavailable
inference.rejected
meter.circuit_opened
meter.circuit_closed
```

Audit fields should include non-secret identifiers, actor type, outcome,
reason code, source client ID, request/correlation ID, and timestamp. They must
not include raw tokens, secrets, prompts, completions, authorization codes, or
PKCE material.

### Usage ledger fields

Extend ledger entries where useful:

```go
RequestID        string
AgentGrantID     string
Provider         string // bounded registry value
Model            string // profile slug
PromptTokens     int64
CompletionTokens int64
CachedTokens     int64
Streamed         bool
Status           string // ok, error, rejected, unknown
StartedAt        time.Time
CompletedAt      time.Time
```

A raw provider model name may have uncontrolled cardinality; prefer the broker
profile slug.

## 26. Atomic accounting

`RecordUsage` currently updates token counters in the same transaction as the
ledger insert. Extend that transaction to grant counters:

```text
BEGIN
  INSERT usage_ledger(... token_id, grant_id ...)
  UPSERT token_counters += actual usage and one request
  IF grant_id != '' THEN
      UPSERT agent_grant_counters += actual usage and one request
  END
COMMIT
```

Budget preflight checks both rows. A rejected request writes a rejected ledger
row but does not advance spend counters.

Because exact usage is known only after inference, one request can overshoot a
remaining token budget. This is an explicit v1 property. Per-token dispatch
serialization bounds concurrent overshoot to one request for a single process.
Strict reservation requires model-specific input estimation and output-token
reservation and is a separate design.

## 27. Metering circuit breaker

Create a shared `meter.Health` observed by middleware and recorder:

```go
type Health interface {
    AllowInference() bool
    RecordSuccess()
    RecordFailure(class FailureClass)
    Snapshot() Snapshot
}
```

Behavior:

- Retry bounded transient `SQLITE_BUSY` according to the store's busy timeout.
- A persistent/durable failure such as disk full opens immediately or after a
  very small configured threshold.
- While open, new `/v1/*` requests return OpenAI-shaped `503 metering_unavailable`
  before provider dispatch.
- A successful health probe or ledger write closes the circuit according to a
  defined half-open policy.
- Opening and closing produce audit events and metrics.
- Readiness is false while the circuit is open.

The first upstream call that discovers the failure may already have spent
provider tokens. The circuit prevents unbounded continuation; it cannot undo
that call.

## 28. Metrics

Expose Prometheus-style metrics on an operator-only listener or protected
endpoint. Recommended names:

```text
llm_proxy_oidc_callbacks_total{outcome}
llm_proxy_device_introspections_total{outcome}
llm_proxy_agent_token_issuance_total{outcome,client_id}
llm_proxy_inference_requests_total{outcome,profile,streamed}
llm_proxy_inference_tokens_total{kind,profile}
llm_proxy_inference_duration_seconds{profile,streamed}
llm_proxy_meter_writes_total{outcome}
llm_proxy_meter_circuit_open
llm_proxy_vault_operations_total{operation,outcome}
llm_proxy_db_operation_duration_seconds{operation}
llm_proxy_active_sessions
llm_proxy_active_capability_tokens
```

Never use user IDs, token IDs, credential IDs, subjects, emails, raw model input,
or IP addresses as metric labels. Profile and client labels must come from
bounded configuration registries.

## 29. Usage APIs and dashboard

The existing `/api/usage?token_id=&since=` returns up to 200 raw rows. Add a
summary query for an intern to implement after the core flow:

```http
GET /api/usage/summary?grant_id=<id>&from=<RFC3339>&to=<RFC3339>&bucket=day
```

Response:

```json
{
  "total_prompt_tokens": 1200,
  "total_completion_tokens": 450,
  "total_cached_tokens": 300,
  "total_requests": 18,
  "by_model": [
    {"model":"gpt-responses","requests":12,"total_tokens":1100},
    {"model":"sonnet","requests":6,"total_tokens":550}
  ]
}
```

The browser dashboard should show token and grant counters, expiry, last use,
revocation, and the metering-health state. It must not render audit payloads as
raw HTML.

---

# Part VI — tiny-idp provisioning and deployment

## 30. OAuth client inventory

### Browser client

```text
ID:                    llm-proxy-web
Type:                  public
Grant types:           authorization_code, refresh_token
PKCE:                  required, S256
Redirect URI:          https://llm.example.test/auth/callback
Post-logout URI:       https://llm.example.test/app
Scopes:                openid, profile, email
```

This client belongs in tiny-idp's reviewed production browser client catalog.
There is no browser client secret.

### Device client

```text
ID:                    llm-proxy-agent
Type:                  public
Grant type:            urn:ietf:params:oauth:grant-type:device_code
Redirect URIs:         none
Audience:              https://llm.example.test/agent/v1
Scopes:                openid, llm.tokens.issue
Refresh grant:         disabled initially
```

Provision it with the tiny-idp admin command because the current production
client catalog accepts browser profiles only.

### Resource/introspection client

```text
ID:                    llm-proxy-resource
Type:                  confidential
Can introspect:        true
Allowed audience:      https://llm.example.test/agent/v1
Client secret:         generated once; hash in tiny-idp DB, plaintext in an
                       owner-only secret mounted only into llm-proxy
```

Illustrative provisioning commands; confirm flags against the pinned tiny-idp
release before scripting:

```bash
tinyidp admin --db=/state/tinyidp.sqlite client create \
  --id=llm-proxy-agent --public \
  --grant-type=urn:ietf:params:oauth:grant-type:device_code \
  --scope=openid --scope=llm.tokens.issue \
  --audience=https://llm.example.test/agent/v1

tinyidp admin --db=/state/tinyidp.sqlite client create \
  --id=llm-proxy-resource --generate-secret --can-introspect \
  --grant-type=authorization_code \
  --audience=https://llm.example.test/agent/v1
```

Capture the generated resource secret without printing it into CI logs. The
bootstrap job writes it directly to a secret manager or mode-0400 file.

## 31. Account provisioning

Use two policies:

- **Local production-shaped development:** seed an account from a Docker secret
  or operator-provided stdin. Do not check a password into Compose or JSON.
- **Production:** use an invitation or reviewed signup program, or create the
  first account through `tinyidp admin user create --password-from-stdin`.

The old checked-in Keycloak test credential must not survive the migration.

## 32. Reverse proxy and back-channel rules

Both browsers and llm-proxy must resolve the same canonical IdP issuer. OIDC
issuer equality is exact. Do not use one issuer for the browser and a different
container URL for token exchange without a transport specifically designed to
preserve the canonical origin and TLS verification.

For local Compose, follow tiny-idp's shared-app example:

- Caddy terminates HTTPS for `idp.localhost` and `llm.localhost`;
- the local root CA is exported into the llm-proxy container trust store;
- llm-proxy resolves `idp.localhost` to Caddy through a controlled network;
- TLS verification remains enabled;
- services run as non-root with owner-only state volumes;
- health checks use `/readyz` and the real TLS path.

## 33. Secret inventory

| Secret | Owner | Storage |
| --- | --- | --- |
| tiny-idp token secret | tiny-idp | owner-only file / secret manager |
| tiny-idp signing private keys | tiny-idp | tiny-idp SQLite |
| resource introspection client secret | tiny-idp hash + llm-proxy plaintext | DB hash and broker secret mount |
| llm-proxy vault master key | llm-proxy | secret manager, never BYOK DB |
| llm-proxy browser session HMAC key | llm-proxy | secret manager |
| provider API keys | user / llm-proxy vault | AES-GCM ciphertext in BYOK DB |
| `llmp_...` capability plaintext | coding agent only | mode-0600 client cache; hash in DB |
| tiny-idp device token | coding agent briefly | memory or short-lived owner-only cache |

Back up identity DB, identity audit, BYOK DB, and key material consistently.
A database backup without its corresponding key material is not recoverable; a
key backup without access controls defeats encryption at rest.

---

# Part VII — Decision records

## 34. Decision: separate tiny-idp service

- **Context:** tiny-idp can be embedded or deployed separately.
- **Options considered:** In-process embedding, pod sidecar, independent service.
- **Decision:** Use a separate service, colocated in local Compose and operated
  as an independent singleton in initial production.
- **Rationale:** Preserves trust and release boundaries, exercises standard
  protocols, and avoids importing tiny-idp internals into llm-proxy.
- **Consequences:** Requires HTTPS/DNS/back-channel configuration and separate
  backups. In-process transport remains a future option.
- **Status:** accepted.

## 35. Decision: public browser client with PKCE S256

- **Context:** tiny-idp's supported browser profile is public and requires PKCE;
  current llm-proxy RP has no PKCE.
- **Options considered:** Confidential client secret, public+PKCE, custom tiny-idp
  confidential browser profile.
- **Decision:** Public browser client with mandatory PKCE S256, state, and nonce.
- **Rationale:** No browser client secret is needed; PKCE binds the code to the
  RP transaction and matches tiny-idp's reviewed helper.
- **Consequences:** `pkg/byok/web/oidc.go` must change before live integration.
- **Status:** accepted.

## 36. Decision: device authorization for coding agents

- **Context:** Terminal agents cannot safely receive a browser session cookie or
  prompt users for provider keys.
- **Options considered:** Copy a manually minted token, loopback callback,
  password grant, device authorization.
- **Decision:** Use RFC 8628 Device Authorization with a public client and exact
  resource indicator.
- **Rationale:** It supports constrained terminals, keeps authentication in the
  browser, and has a working tiny-idp implementation and client reference.
- **Consequences:** Polling, expiry, denial, `slow_down`, and secure cache behavior
  require explicit tests.
- **Status:** accepted.

## 37. Decision: account provisioning differs by environment

- **Context:** The old Keycloak Compose checks in a predictable test account,
  while tiny-idp supports admin-created users, invitations, and reviewed signup
  programs.
- **Options considered:** Checked-in seed account, automatic open signup,
  operator-created account, invitation/reviewed signup.
- **Decision:** Local production-shaped development creates a user from an
  owner-supplied Docker secret or stdin. Production uses an operator-created
  first account and then invitation or reviewed signup policy. No default
  password is checked into source.
- **Rationale:** Keeps local onboarding deterministic without normalizing a
  production credential fixture or silently enabling public registration.
- **Consequences:** Compose needs a one-shot idempotent account bootstrap; the
  production operator must choose and configure an onboarding policy.
- **Status:** accepted.

## 38. Decision: exchange IdP token for broker capability

- **Context:** tiny-idp access tokens carry identity scopes but not dynamic
  credential bindings, profile policy, budgets, or broker revocation state.
- **Options considered:** Use tiny token directly at `/v1`, encode all policy in
  OAuth scopes, exchange for `llmp_...`.
- **Decision:** Exchange at `/agent/v1/tokens` for an ordinary broker token.
- **Rationale:** Reuses proven data-plane enforcement and keeps domain policy in
  its owning system.
- **Consequences:** Two token classes and an introspection client must be
  operated. Route boundaries must stay unambiguous.
- **Status:** accepted.

## 39. Decision: pre-approved agent grants

- **Context:** Letting a device-authenticated CLI request arbitrary credential
  IDs and models would turn broad identity into unrestricted LLM authority.
- **Options considered:** Arbitrary request, fixed global policy, pre-approved
  user-owned grant.
- **Decision:** Device exchange requires an enabled agent grant created in the
  browser control plane.
- **Rationale:** The user approves durable domain policy where credentials and
  profile metadata are visible.
- **Consequences:** Adds grant tables, UI, counters, and cascade behavior.
- **Status:** accepted.

## 40. Decision: cumulative grant budgets

- **Context:** A per-token budget can be reset by repeatedly reacquiring tokens.
- **Options considered:** Per-token only, rate-limit issuance, cumulative grant
  counters.
- **Decision:** Enforce both per-token and cumulative grant budgets.
- **Rationale:** Reissuance and rotation remain usable without becoming a budget
  reset mechanism.
- **Consequences:** Meter transactions update two counter rows.
- **Status:** accepted.

## 41. Decision: identity is `(issuer, subject)`

- **Context:** OIDC `sub` is not globally unique.
- **Options considered:** Subject alone, email, issuer+subject.
- **Decision:** Store and look up the exact canonical issuer and subject pair.
- **Rationale:** This is the OIDC identity contract; email and username are
  mutable profile attributes.
- **Consequences:** Requires schema versioning and migration.
- **Status:** accepted.

## 42. Decision: no provider OAuth in the first integration

- **Context:** tiny-idp OAuth and LLM-provider OAuth solve different problems;
  provider contracts are approval-gated.
- **Options considered:** Add provider OAuth now, static API keys first.
- **Decision:** First usable path supports typed static API-key credentials.
- **Rationale:** Existing vault/injection is implemented and tested. Guessing
  provider OAuth contracts is unsafe.
- **Consequences:** Renewable credentials remain a follow-up after Geppetto
  upgrade and provider selection.
- **Status:** accepted.

## 43. Decision: single active broker instance initially

- **Context:** Rate windows, token dispatch locks, SQLite, and meter health are
  currently process-local.
- **Options considered:** Immediate distributed enforcement, single active
  instance.
- **Decision:** Operate one active llm-proxy instance for v1.
- **Rationale:** It matches current correctness assumptions and minimizes the
  first integration's scope.
- **Consequences:** Horizontal scaling requires transactional reservation and a
  shared store redesign.
- **Status:** accepted.

## 44. Decision: audits and usage have separate records

- **Context:** Security mutation history and high-volume token accounting have
  different retention and query patterns.
- **Options considered:** One event stream, separate audit and ledger.
- **Decision:** Keep audit events and usage ledger separate; add bounded
  operational metrics as a third channel.
- **Rationale:** Avoids duplicating every successful inference into the audit
  stream while preserving authoritative accounting.
- **Consequences:** Correlation IDs connect records when needed.
- **Status:** accepted.

---

# Part VIII — File-level implementation plan

## 45. Phase 0: production prerequisites and migrations

**Goal:** Make schema evolution, audit, and meter failure semantics safe before
adding tables.

Files:

- `pkg/byok/store/sqlite/store.go`: replace monolithic `ensureSchema` with a
  forward-only migration runner.
- `pkg/byok/store/store.go`: add transactional lifecycle methods where mutation
  and audit must commit together.
- `pkg/byok/meter/meter.go`: return/record health outcomes through shared state.
- `pkg/byok/authmw/middleware.go`: reject while meter circuit is open.
- `cmd/llm-proxy-server/main.go`: configure threshold and readiness.

Tests:

- migrate a v1 fixture to the new version;
- reject unknown future schema versions;
- rollback failed migrations;
- simulate meter store failure and assert subsequent inference returns 503;
- assert audit and mutation do not diverge under injected failure.

This phase closes prerequisites from `LLM-PROXY-BYOK-HARDENING` and
`LLM-PROXY-BYOK-PROD-READINESS`; do not create parallel incompatible migration
or circuit-breaker implementations in this ticket.

### Phase 0 implementation record (2026-07-22)

Phase 0 is implemented in the current working tree as follows:

- `pkg/byok/store/sqlite/schema.go` owns `PRAGMA user_version` and the ordered,
  forward-only migration list. Version 1 represents the pre-existing BYOK
  schema plus the committed-write `metering_health` probe row. Empty and legacy
  version-0 databases migrate transactionally. Current schemas are revalidated.
  Newer, partial, or malformed schemas fail closed. Validation covers required
  columns, NOT NULL and primary-key constraints, security-critical unique and
  lookup indexes, foreign keys, and the singleton probe row; a failed migration
  leaves both DDL and version unchanged.
- `pkg/byok/store.LifecycleStore` defines the audited credential and token
  mutation boundary. Web and operator CLI paths use only
  `CreateCredentialAudited`, `DeleteCredentialAudited`, `MintTokenAudited`, and
  `RevokeTokenAudited`. SQLite commits the domain mutation, cascade token
  revocations, and closed typed audit payloads in one transaction. Audit insert
  failure rolls back the complete mutation. The memory backend performs the
  same state changes under one lock for conformance tests.
- At Phase 0 completion, persisted agent grants and server-side sessions did
  not exist. Phase 2 now adds auth transactions and sessions with typed audit
  events committed atomically with create, consume, and revoke mutations;
  injected audit failures prove rollback. Agent grants must follow the same
  boundary in Phase 3.
- `pkg/byok/meter.Health` is shared by the recorder, auth middleware, readiness,
  and audit. Persistent write failures open immediately. SQLite busy/locked and
  context failures count toward a configurable transient threshold. An open
  circuit rejects `/v1/*` with `503 metering_unavailable`, leaves `/healthz`
  live, and makes `/readyz` unavailable.
- Recovery is half-open and single-probe. After the configured cooldown, one
  caller performs `Store.CheckMeteringHealth`, which commits an update to the
  singleton probe row. No provider path resumes before that write succeeds.
  A concurrent in-flight accounting failure wins over a successful probe.
- `HealthSnapshot` exposes bounded counters and state for metrics integration;
  typed `meter.circuit_opened` and `meter.circuit_closed` events record coarse
  transition reasons without raw database errors, request metadata, or secrets.
- `cmd/llm-proxy-server/main.go` performs a startup write probe and exposes
  `--byok-meter-transient-failure-threshold` (default 3) and
  `--byok-meter-recovery-cooldown` (default 5s).
- Required security validation found reachable vulnerabilities in the prior Go
  1.26.4 toolchain and `golang.org/x/text v0.37.0`; Phase 0 pins Go 1.26.5 and
  the compatible patched `golang.org/x/*` set. The post-upgrade vulnerability
  scan reports no reachable vulnerabilities.

The Phase 0 implementation deliberately did not introduce agent-grant or
session schema early. Phase 2 subsequently added the completed session and auth
transaction contracts through schema version 2. Phase 3 now owns the forward-only
schema version 3 migration for agent grants, cumulative counters, bindings, and
token provenance.

## 46. Phase 1: tiny-idp production-shaped Compose

**Goal:** Replace Keycloak with a pinned tiny-idp service and exact client
inventory.

Files to add/update:

- `deploy/docker-compose.yaml`: tiny-idp, llm-proxy, Caddy, CA export, one-shot
  bootstrap, durable volumes, health dependencies.
- `deploy/tinyidp/clients.json`: browser client only.
- `deploy/tinyidp/themes.json`, reviewed CSS, minimal signup program required by
  `serve-production`.
- `deploy/tinyidp/bootstrap.sh`: idempotently create device/resource clients and
  initial dev account from secrets.
- `deploy/tinyidp/Caddyfile` and `issue-local-cert.sh`.
- remove `deploy/keycloak/realm-byok.json` after replacement works.

Requirements:

- pin tiny-idp to an approved release/image digest;
- no checked-in passwords or generated client secrets;
- long-running services run non-root; root-only initialization and certificate
  issuance are bounded one-shot jobs;
- leaf certificates come from the external, manually retained
  `tinyidp-local-caddy-pki` authority without exposing its private keys to a
  long-running BYOK container;
- real certificate verification on back-channel discovery/introspection;
- `/readyz` gates startup.

## 47. Phase 2: PKCE-correct browser OIDC

**Status:** Implemented and validated.

**Goal:** Complete browser login against tiny-idp.

Files:

- `pkg/byok/web/oidc.go`: PKCE, issuer capture, one-time auth transaction.
- `pkg/byok/web/session.go`: server-side session ID transport.
- `pkg/byok/store/models.go`: issuer-aware user, Session, AuthTransaction.
- `pkg/byok/store/store.go`: identity/session/auth-flow interfaces.
- SQLite and memory stores.
- `pkg/byok/web/oidc_flow_test.go`: full PKCE, callback-order, replay, cookie,
  and session-revocation flow tests.
- `pkg/byok/store/sqlite/schema_test.go`, `lifecycle_test.go`, and backend
  conformance tests: migration, identity isolation, expiry, one-time consume,
  per-session revocation, and atomic audit rollback.

Test the exact order:

```text
consume/validate state transaction
-> exchange code with verifier
-> verify ID-token signature/audience/issuer
-> compare nonce
-> upsert (issuer, subject)
-> create session
```

Do not log token exchange bodies.

## 48. Phase 3: agent-grant domain

**Status:** Implemented and validated in memory, SQLite, browser API/UI, and
`/v1/*` policy enforcement.

**Goal:** Let a browser-authenticated user define safe coding-agent policy.

Files:

- `pkg/byok/store/models.go`: AgentGrant and counters.
- `pkg/byok/store/store.go`: grant CRUD and ownership methods.
- SQLite/memory store implementations and conformance tests.
- `pkg/byok/web/api.go`: grant endpoints.
- `pkg/byok/web/static/*`: grant form, model selector, budget and usage display.
- `pkg/byok/policy/policy.go`: grant liveness and budget checks.

Grant creation accepts only concrete profile IDs obtained from the profile
resolver; no free-form glob compatibility mode was added. Per-token limits,
cumulative grant budgets, expiry, update/revocation, credential-deletion
cascades, child-capability revocation, issuance/rotation, and typed audit events
share atomic store boundaries. Cumulative counters remain on the grant across
policy updates, rotation, and reissue.

## 49. Phase 4: IdP access-token introspection

**Status:** Complete. The provider-neutral consumer, strict validation,
bounded keyed cache, and route separation are implemented. tiny-idp PR #15 was
merged and released as `v0.0.5`; production-shaped Compose pins its immutable
OCI digest and passed clean-volume CA-verified browser/device/introspection
acceptance.

**Goal:** Authenticate `/agent/v1/*` using tiny-idp device tokens.

Files:

- `pkg/byok/oidcauth/oidcauth.go`: discovery, RFC 7662 client, strict claims,
  HMAC token keys, bounded positive/negative cache;
- tests copied conceptually, not mechanically, from tiny-idp
  `cmd/tinyidp-xapp/internal/resourceauth/resourceauth_test.go`;
- `pkg/byok/agentapi/server.go`: mount agent routes separately;
- `cmd/llm-proxy-server/main.go`: issuer, audience, resource client ID, and
  secret-file configuration.

Production configuration reads the resource secret from a file rather than a
CLI value that appears in process listings. RFC 6749 form-encoding is applied
before Basic encoding, including secrets containing `+`, `/`, or `%`. Validation
requires exact issuer, same-origin discovered endpoint, active token, subject,
authorized device client, exact resource audience, future expiry, Bearer token
type, and issuance scope.

## 50. Phase 5: device exchange and client CLI

**Status:** Complete. Unit/race tests and clean-volume acceptance passed the
live RFC 8628 → RFC 7662 → pre-approved grant exchange, secure cache lifecycle,
four-direction token separation, and grant-revocation cascade against the
pinned tiny-idp `v0.0.5` image.

**Goal:** A terminal can approve, acquire, cache, and use an `llmp_...` token.

Files:

- `pkg/byok/agentapi/server.go` maps exact `(issuer, subject)`, hides credential
  bindings, and exchanges approved grants through atomic issue/rotate;
- token persistence records grant, channel, source client, and stable client
  installation provenance;
- `pkg/byok/deviceclient/` implements strict discovery, RFC 8628 polling,
  explicit ambiguous-grant selection, exchange validation, and POSIX cache
  lifecycle;
- `cmd/llm-proxy-server/cmds/byok/agent.go` exposes login, status, and logout.

The cache uses a persistent random 128-bit installation ID, advisory locking,
`O_NOFOLLOW`, atomic replacement, file/directory sync, mode-`0600` regular files,
and mode-`0700` directories. Unsupported non-POSIX systems fail closed rather
than writing weakly protected credentials. The live smoke proved both token
classes in both route directions without dispatching to a live provider.

## 51. Phase 6: grant accounting, audit, metrics, and UI

**Goal:** Make usage and operations understandable and enforceable.

Files:

- `pkg/byok/store/sqlite/store.go`: update token + grant counters atomically;
- `pkg/byok/meter/meter.go`: include grant ID and meter health;
- `pkg/byok/authmw/middleware.go`: grant preflight and circuit state;
- new metrics package or existing project metrics integration;
- summary queries and dashboard components;
- audit event helpers with typed payloads instead of hand-built strings.

## 52. Phase 7: acceptance and release gate

**Offline CI acceptance:**

1. Start in-process fake OIDC/introspection endpoints or tiny-idp test server.
2. Complete browser PKCE login.
3. Add fake-provider credential.
4. Create an agent grant.
5. Complete device authorization.
6. Introspect and exchange for `llmp_...`.
7. Call `/v1/models`; observe only allowed profiles.
8. Call chat completion through the real Geppetto provider packaging pointed at
   a local fake provider.
9. Assert the fake provider saw the user's credential and the client did not.
10. Assert ledger, token counters, grant counters, audit, and metrics.
11. Revoke grant; next inference is denied.
12. Break meter storage; circuit opens and next inference is 503.

**Live tiny-idp acceptance:** production-shaped Compose and browser/device
playbook. This is safe because tiny-idp is local.

**Live LLM-provider acceptance:** separate approval-gated smoke. It is not
implied by a successful IdP integration.

---

# Part IX — Test and review guide

## 53. Unit test matrix

### OIDC browser

- authorization URL contains state, nonce, `code_challenge`, and
  `code_challenge_method=S256`;
- verifier reaches token exchange;
- wrong/missing state fails before exchange;
- consumed transaction cannot replay;
- wrong nonce fails after ID-token verification;
- issuer mismatch fails;
- same subject from different issuers maps to different users;
- return URL remains local;
- exact callback URI is used.

### Introspection

- missing, multiple, malformed bearer headers;
- inactive token;
- wrong issuer, audience, client, token type, scope, or expiry;
- discovery issuer mismatch;
- foreign introspection endpoint;
- unsupported introspection auth method;
- oversized/malformed/multiple JSON response;
- provider unavailable yields 503 outcome;
- raw token absent from cache keys and error text;
- cache expiry and pruning.

### Grant and exchange

- cross-user grant access is 404;
- disabled/revoked grant cannot issue;
- no matching credential fails closed;
- unknown model fails at grant creation;
- repeated same-instance issuance revokes previous token;
- different instance respects active-token cap;
- grant counters survive token rotation;
- plaintext token appears only in successful issue response;
- transaction rollback leaves no token without audit;
- concurrent issue preserves one active token per instance.

### Metering

- streaming and non-streaming update token and grant counters;
- canceled client still records provider usage;
- rejected requests do not advance spend counters;
- provider omitted usage still increments request count;
- store failure opens circuit;
- open circuit prevents provider dispatch;
- recovery closes circuit according to policy.

## 54. Integration and adversarial tests

- Browser login and device approval for the same IdP account map to the same
  BYOK user via `(iss,sub)`.
- A tiny-idp token sent to `/v1/chat/completions` is rejected as an invalid
  `llmp_...` key without introspection.
- An `llmp_...` token sent to `/agent/v1/tokens` is rejected without DB fallback.
- A token for another resource audience cannot issue an LLM capability.
- A device client not in `AllowedClients` cannot issue even with the right
  audience and scope.
- Deleting a credential revokes child tokens and invalidates affected grants.
- A server profile key is scrubbed and never reaches the fake provider.
- Database and logs contain no known fixture plaintext secrets.
- Response errors do not reflect authorization headers or provider keys.

## 55. Commands for the intern

From llm-proxy:

```bash
GOWORK=off go test ./pkg/byok/... -count=1
GOWORK=off go test -race ./pkg/byok/... -count=1
GOWORK=off go test ./... -count=1
GOWORK=off go build ./...
make lint
make gosec
```

From tiny-idp, against the pinned integration revision:

```bash
GOWORK=off go test ./pkg/embeddedidp ./pkg/idpaccounts ./cmd/tinyidp-xapp -count=1
GOWORK=off go test -race ./pkg/embeddedidp ./cmd/tinyidp-xapp -count=1
GOWORK=off go test ./... -count=1
GOWORK=off go build ./...
```

Then run the production-shaped Compose playbook and inspect only redacted,
non-secret output.

## 56. Review order

A reviewer should inspect in this order:

1. Migration runner and new store invariants.
2. PKCE/auth transaction and issuer-aware identity.
3. Introspection validation and route separation.
4. Grant ownership, issuance transaction, and cumulative budgets.
5. Vault injection and fallback-key scrubbing.
6. Meter failure policy and atomic token/grant accounting.
7. Audit payloads and metric label cardinality.
8. CLI cache permissions and plaintext-output behavior.
9. Compose secret handling, TLS trust, and pinned artifacts.
10. End-to-end tests proving credential separation.

## 57. Common mistakes

- Calling an OIDC nonce “PKCE.” They are independent protections.
- Using email as the identity key. Email can change.
- Accepting tiny-idp access tokens directly on the LLM data plane.
- Letting the agent choose arbitrary credential IDs during exchange.
- Resetting budgets when a token rotates.
- Caching raw access tokens as map keys.
- Returning 401 when introspection is unavailable; that hides an outage as bad
  credentials.
- Logging token endpoint or introspection bodies.
- Keeping Keycloak's checked-in test password in the new Compose.
- Using the tiny-idp dev server as production because it is convenient.
- Exposing user/token IDs as Prometheus labels.
- Claiming Codex/Anthropic-native compatibility without implementing their wire
  APIs.
- Running more than one llm-proxy instance while relying on process-local locks.
- Treating metering errors as best-effort logs.

---

# Part X — Risks, gates, and open questions

## 58. Production gates

Before real deployment:

- tiny-idp must have an approved release/tag or immutable image digest containing
  the required production/device/introspection work;
- tiny-idp's outstanding external conformance/release review must be evaluated;
- llm-proxy schema migration, server-side sessions, PKCE, and meter circuit
  breaker must land;
- local production-shaped browser and device smoke must pass;
- backup/restore must cover both databases, both audits, and secret material;
- a threat review must verify route/token separation and exchange policy;
- an OpenAI-compatible coding-agent client must pass an explicit compatibility
  test;
- live provider smoke requires separate approval.

## 59. Residual risks

- llm-proxy sees prompts and completions. This system protects credentials and
  limits authority; it does not provide end-to-end prompt confidentiality.
- A compromised llm-proxy process can access vault plaintext during inference.
- SQLite and process-local locks constrain initial deployment to one active
  broker instance.
- Post-hoc token accounting permits one-request overshoot.
- Introspection caching creates a small IdP revocation-latency window.
- A stolen `llmp_...` token is a bearer credential until expiry or revocation.
  Short TTLs, narrow grants, and mode-0600 storage reduce impact.
- Provider usage reports can be missing or provider-specific. Request counters
  still advance; missing token counts must be visible as a metering-quality
  signal.

## 60. Open questions requiring owner decision

1. What exact coding agent is the first acceptance target: Pinocchio, a generic
   OpenAI SDK client, or a Codex-compatible client requiring `/v1/responses`?
2. What are default per-token and cumulative grant budgets?
3. Is an 8-hour capability TTL acceptable, or should the first default be
   shorter?
4. Should device-token introspection be cached at all on the privileged issuance
   endpoint?
5. Should the agent CLI support refresh/reacquisition automatically, or require
   an explicit human approval every time?
6. Is agent-initiated usage/revocation needed in v1, or can those stay browser-
   only?
7. What is the first approved tiny-idp release/image containing production
   device and introspection behavior?
8. Which metrics stack and operator endpoint policy does llm-proxy use?
9. Should credential verification be implemented, and for which provider under
   explicit approval?
10. When should llm-proxy upgrade from Geppetto v0.13.4 to v0.13.7 or newer?

---

# Part XI — Source map

## 61. llm-proxy files

| Path | Why to read it |
| --- | --- |
| `cmd/llm-proxy-server/main.go` | Runtime composition and flags |
| `pkg/server/server.go` | Actual client-facing API compatibility |
| `pkg/byok/web/oidc.go` | Browser OIDC flow and missing PKCE |
| `pkg/byok/web/session.go` | Stateless session being replaced |
| `pkg/byok/web/web.go` | Control-plane route registration |
| `pkg/byok/web/api.go` | Credential, token, and usage JSON APIs |
| `pkg/byok/tokens/tokens.go` | Opaque token mint/hash format |
| `pkg/byok/authmw/middleware.go` | Data-plane token validation and preflight |
| `pkg/byok/authmw/ratelimit.go` | Process-local limiter and dispatch locks |
| `pkg/byok/engines/provider.go` | Request-time key injection and model defense |
| `pkg/byok/meter/meter.go` | Authoritative usage hook and circuit gap |
| `pkg/byok/store/models.go` | Current domain model |
| `pkg/byok/store/store.go` | Store interfaces |
| `pkg/byok/store/sqlite/store.go` | Current DDL, transactions, audit behavior |
| `pkg/byok/integration_test.go` | Credential-separation and budget E2E proof |
| `pkg/runtime/chat_service.go` | Streaming/non-streaming usage hook points |
| `examples/profiles.yaml` | Profile slugs and API-type mapping |

## 62. tiny-idp files

| Path | Why to read it |
| --- | --- |
| `docs/embedding-foundations.md` | Supported packages and production contract |
| `pkg/embeddedidp/bootstrap.go` | Browser/device profiles and client drift checks |
| `pkg/idpstore/types.go` | Allowed audiences, grants, and introspection capability |
| `pkg/idpstore/claims.go` | `preferred_username` and email claim behavior |
| `internal/oidcmeta/discovery.go` | Exact discovery surface |
| `cmd/tinyidp-xapp/device_cli.go` | Working RFC 8628 polling and secure cache |
| `cmd/tinyidp-xapp/internal/resourceauth/resourceauth.go` | Hardened introspection reference |
| `internal/cmds/admin_client.go` | Device/resource client provisioning flags |
| `internal/cmds/serve_production.go` | Production host requirements |
| `examples/tinyidp-shared-two-apps/compose.yaml` | Local HTTPS and secret/bootstrap pattern |

## 63. Companion ticket documents

- `research/01-tiny-idp-integration-research-and-architect-onboarding-brief.md`
  in this ticket — evidence inventory; read with the PKCE correction in this
  guide.
- `LLM-PROXY-BYOK/design-doc/02-intern-guide-byok-system-analysis-design-and-implementation.md`
  — original BYOK intern guide and threat model.
- `LLM-PROXY-BYOK-HARDENING/design-doc/01-byok-hardening-plan.md` — migration,
  rekey, rate-limiter, cost, and dev-login work.
- `LLM-PROXY-BYOK-PROD-READINESS/design-doc/01-byok-production-readiness-plan.md`
  — live OIDC, sessions, and meter circuit breaker.

## 64. External protocol references

- OpenID Connect Core 1.0 — identity token and relying-party behavior.
- RFC 7636 — PKCE verifier and S256 challenge.
- RFC 8628 — Device Authorization Grant and polling errors.
- RFC 7662 — OAuth Token Introspection.
- RFC 8707 — Resource Indicators / audience request.
- RFC 6750 — Bearer token usage and error semantics.

---

# Conclusion

The useful part of this architecture is not merely replacing one IdP binary
with another. The integration turns the existing BYOK broker into a complete
authority chain:

```text
human authentication and consent
-> device identity token
-> broker grant selection
-> scoped capability token
-> request-time credential injection
-> provider inference
-> usage ledger + cumulative budgets + audit + metrics
```

Each arrow crosses an explicit API and each token has one job. tiny-idp owns
identity. llm-proxy owns LLM capability policy. Geppetto owns provider request
mechanics. The coding agent receives only the least authority needed for the
selected grant. Preserving those ownership boundaries is the main correctness
criterion for every implementation phase.
