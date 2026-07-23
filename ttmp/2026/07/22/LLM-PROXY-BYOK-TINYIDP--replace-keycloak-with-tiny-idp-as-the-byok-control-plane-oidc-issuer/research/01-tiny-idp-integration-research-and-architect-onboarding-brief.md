---
Title: tiny-idp integration research and architect onboarding brief
Ticket: LLM-PROXY-BYOK-TINYIDP
Status: active
Topics:
    - byok
    - auth
    - security
    - oidc
    - identity
    - integration
    - llm-proxy
DocType: research
Intent: long-term
Owners: []
RelatedFiles:
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/cmd/llm-proxy-server/main.go
      Note: BYOK flag wiring (--byok-oidc-issuer-url etc.)
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/docker-compose.yaml
      Note: |-
        Keycloak dev compose — to be replaced by an embedded tiny-idp
        Phase 1 replacement for the removed Keycloak realm import
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/api.go
      Note: Management JSON API (credential CRUD, token mint/revoke)
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/oidc.go
      Note: Current OIDC relying-party implementation (Keycloak/any OIDC) — the primary file to rebind
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/session.go
      Note: HMAC session cookie handling to preserve
    - Path: /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/web/web.go
      Note: Control-plane server assembly and route registration
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/cmd/tinyidp-xapp/development_app.go
      Note: Reference embedder composition (in-process issuer + RP)
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/docs/embedding-foundations.md
      Note: tiny-idp's supported public composition boundary
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/examples/embedded/main.go
      Note: Minimal end-to-end embedded example
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/bootstrap.go
      Note: BrowserClient/DeviceClient declarations and Bootstrap() reconciliation
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/inprocess_transport.go
      Note: InProcessIssuerTransport — back-channel HTTP without a network hop
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/options.go
      Note: tiny-idp embedding API surface (Options, Mode, CookieConfig, TokenConfig)
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/embeddedidp/provider.go
      Note: |-
        Provider construction and Handler() mounting
        Provider construction and Handler mounting
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/idpaccounts
      Note: |-
        Account creation, password auth, lockout — replaces Keycloak user store
        Account creation password auth lockout replaces Keycloak user store
    - Path: /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/pkg/sqlitestore
      Note: Persistent identity store (SQLite) backing the embedded provider
ExternalSources:
    - https://openid.net/specs/openid-connect-core-1_0.html
    - https://www.rfc-editor.org/info/rfc7636
    - https://www.rfc-editor.org/info/rfc7662
    - https://www.rfc-editor.org/rfc/rfc8707.html
    - https://www.rfc-editor.org/info/rfc8628
Summary: 'Evidence-backed inventory of everything an architect needs to replace Keycloak with tiny-idp as the BYOK control-plane OIDC issuer: current llm-proxy integration points, tiny-idp embedding API, discovery shape, client model, in-process transport option, relevant commits, and an open-questions and decision list.'
LastUpdated: 2026-07-22T18:00:00-04:00
WhatFor: Orient the system architect before designing the tiny-idp integration. Read this first, then the design-doc it will inform.
WhenToUse: Before writing the integration design doc, before touching pkg/byok/web, and before any decision about single-process vs separate tiny-idp deployment.
---














# tiny-idp integration research and architect onboarding brief

## 1. Purpose and scope

This document is a research and onboarding brief for the system architect who
will design the integration of **tiny-idp** as the OpenID Connect identity
provider (IdP) for the **llm-proxy BYOK control plane**, replacing the current
**Keycloak** dev dependency.

It is deliberately evidence-first: every claim is anchored to concrete files,
commits, or documented APIs so the architect can verify quickly and start
designing without re-discovering the system.

It is **not** the design document. It exists to make the design document fast to
write.

### Out of scope

- Writing the integration design (separate design-doc in this ticket).
- Changing the BYOK vault, token, metering, or enforcement layers — those are
  unaffected by the IdP swap.
- Replacing llm-proxy's own session cookies or management API.
- Coding the integration.

### Companion tickets to read first

- `LLM-PROXY-BYOK` (2026-07-05) — the BYOK control-plane + enforcement design.
  Especially `design-doc/01-...-prior-art-analysis-and-architecture-proposal.md`
  (§Proposed Solution, §Phase 3).
- `LLM-PROXY-BYOK-PROD-READINESS` (2026-07-06) — the open task that motivates
  this ticket: its first task is literally "stand up Keycloak compose + drive
  full OIDC browser flow against `pkg/byok/web/oidc.go`". This ticket redirects
  that effort from Keycloak to tiny-idp.

## 2. Executive summary

llm-proxy's BYOK control plane already speaks standard OIDC as a relying party
(RP) against any discoverable issuer. Today that issuer is Keycloak 26.2 in a
dev Docker Compose. tiny-idp is a production-shaped, embeddable OIDC provider
written in Go, with SQLite persistence, Argon2id passwords, auth-code + PKCE,
refresh rotation, device authorization (RFC 8628), token introspection
(RFC 7662), resource indicators (RFC 8707), DPoP, audit, and an in-process issuer
transport that avoids a network hop when the RP and IdP share a process.

The integration is therefore **not a protocol-translation problem**. It is an
**operational topology decision**: embed tiny-idp inside the llm-proxy process
(single binary, in-process transport), run tiny-idp as a separate issuer that
llm-proxy discovers over HTTP, or run it as a sidecar in compose/k3s. Each has
different tradeoffs for state, scaling, and blast radius, detailed in §7.

The key technical work, regardless of topology:

1. Replace `deploy/docker-compose.yaml` (Keycloak) with a tiny-idp bootstrap —
   either an embedded provider in `llm-proxy-server` or a standalone
   `tinyidp-xapp`/`tinyidp` process.
2. Replace `deploy/keycloak/realm-byok.json` (realm import) with a tiny-idp
   `embeddedidp.BootstrapConfig` declaring a browser client whose redirect URIs
   match llm-proxy's `/auth/callback`, plus an initial admin account via
   `idpaccounts.Service`.
3. Point `--byok-oidc-issuer-url` at the tiny-idp issuer and set
   `--byok-oidc-client-id` to the declared browser client ID. No changes to
   `pkg/byok/web/oidc.go` are required if tiny-idp is run as a separate HTTP
   issuer; the in-process topology needs the `go-oidc` provider's HTTP client
   wired to `InProcessIssuerTransport`.
4. Write the live OIDC smoke playbook that the `LLM-PROXY-BYOK-PROD-READINESS`
   ticket has been waiting on.

## 3. Repositories and working locations

| Repo | Path | Role |
| --- | --- | --- |
| llm-proxy | `/home/manuel/code/wesen/go-go-golems/llm-proxy` | BYOK broker; the integration target. Contains `pkg/byok/web/*` RP code. |
| tiny-idp | `/home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp` | Embeddable OIDC provider; the new IdP. |
| byok-host | `/home/manuel/code/wesen/2026-04-17--byok-host` | Doc-first origin (design only); reference for prior-art decisions. |
| geppetto | `/home/manuel/code/wesen/go-go-golems/geppetto` | Inference engine + reusable OAuth credential lifecycle (v0.13.7). Not touched by this integration. |
| pinocchio | `/home/manuel/code/wesen/go-go-golems/pinocchio` | Host-side OAuth profiles (v0.11.6). Not touched by this integration. |
| go-go-goja | `/home/manuel/code/wesen/go-go-golems/go-go-goja` | Express auth DSL + hostauth (the xgoja application layer tiny-idp serves in xapp). Relevant only if llm-proxy adopts an xgoja front-end later. |
| go-go-parc (Obsidian vault) | `/home/manuel/code/wesen/go-go-golems/go-go-parc` | `Projects/2026/07/05/PROJ - LLM-Proxy BYOK - Credential Vault, Token Minting, and Metered Proxy Enforcement.md` — the original project note. |

`ws://...` prefixes in `RelatedFiles` mean the tiny-idp working tree, to make
clear those paths are not inside the llm-proxy repo.

## 4. Current state: how llm-proxy talks to Keycloak today

All of the following is verified in the llm-proxy repo at `main`
(commit `c898aae`, PR #5 merged 2026-07-06).

### 4.1 The relying-party code is standard OIDC

`pkg/byok/web/oidc.go` (221 lines) implements an authorization-code RP with
state and OIDC nonce using `github.com/coreos/go-oidc/v3/oidc` and
`golang.org/x/oauth2`. It **does not currently implement PKCE**. In particular,
`gooidc.Nonce(nonce)` is an OIDC nonce option, not a PKCE challenge. It:

- builds an OIDC provider from `--byok-oidc-issuer-url` via discovery
  (`gooidc.NewProvider`, `oidc.go:54`),
- requests scopes `openid profile email` (`oidc.go:60`),
- generates a random `state` and `nonce`, stores them in 10-minute cookies,
  redirects to `AuthCodeURL` with `gooidc.Nonce(nonce)` (`handleLogin`,
  `oidc.go:113`),
- on `/auth/callback`: validates `state` cookie, exchanges the code, extracts
  `id_token`, verifies signature + nonce via `provider.Verifier`
  (`handleAuthCallback`, `oidc.go:134`),
- reads `email`, `preferred_username`, `name` claims and upserts a `store.User`
  keyed by `OIDCSubject = idToken.Subject` (`oidc.go:173`),
- sets an HMAC session cookie via `pkg/byok/web/session.go`,
- redirects to `return_to` (sanitized to local absolute paths only,
  `sanitizeReturnTo`, `oidc.go:99`).

**Corrected integration implication:** discovery and ID-token verification are
issuer-agnostic, but the code will **not** work unchanged with tiny-idp's
supported `BrowserClient`, which requires PKCE. The RP must generate a verifier,
pass `oauth2.S256ChallengeOption(verifier)` to `AuthCodeURL`, and pass
`oauth2.VerifierOption(verifier)` to `Exchange`. The architecture guide in this
ticket specifies a one-time server-side auth transaction for the verifier,
state, nonce, and return path.

### 4.2 Configuration surface (main.go)

`cmd/llm-proxy-server/main.go` wires these Glazed flags (lines 40–48, 144–168):

| Flag | Env | Purpose |
| --- | --- | --- |
| `--byok-db` | `LLM_PROXY_BYOK_DB` | SQLite path; enables bearer enforcement on `/v1/*` |
| `--byok-master-key` | `LLM_PROXY_BYOK_MASTER_KEY` | AES-GCM vault master key (base64) |
| `--byok-session-secret` | `LLM_PROXY_BYOK_SESSION_SECRET` | HMAC cookie secret; enables `/app` webapp |
| `--byok-oidc-issuer-url` | `LLM_PROXY_BYOK_OIDC_ISSUER_URL` | OIDC issuer (e.g. `http://127.0.0.1:18080/realms/byok`) |
| `--byok-oidc-client-id` | `LLM_PROXY_BYOK_OIDC_CLIENT_ID` | Confidential client id (`llm-proxy-web`) |
| `--byok-oidc-client-secret` | `LLM_PROXY_BYOK_OIDC_CLIENT_SECRET` | Client secret |
| `--byok-public-url` | `LLM_PROXY_BYOK_PUBLIC_URL` | Externally visible llm-proxy base URL |
| `--byok-dev-user` | `LLM_PROXY_BYOK_DEV_USER` | Skip OIDC, log in as a local user (dev only) |

Control-plane mounting (`main.go:238–262`): when `--byok-db` and
`--byok-session-secret` are both set, `byokweb.NewServer` is constructed and
`Register`ed on the outer mux; the OIDC config is passed through only if
`--byok-oidc-issuer-url` is non-empty.

A pending hardening task (`LLM-PROXY-BYOK-HARDENING`) will make `--byok-dev-user`
refuse to start on non-loopback listeners. Keep that in mind when designing the
dev story for tiny-idp.

### 4.3 The Keycloak dev compose

`deploy/docker-compose.yaml` runs `quay.io/keycloak/keycloak:26.2` on
`http://127.0.0.1:18080`, importing `deploy/keycloak/realm-byok.json`. That realm
defines:

- realm `byok`,
- one **confidential** client `llm-proxy-web` with a checked-in development
  secret (value intentionally omitted here), `standardFlowEnabled`, redirect
  URIs `http://127.0.0.1:8080/*` and `http://localhost:8080/*`,
- one test user with a checked-in development password (identity and value
  intentionally omitted here).

This is the entire Keycloak footprint. tiny-idp needs to replace exactly these
three things: the issuer URL, the client declaration, and the initial user.

### 4.4 The user model the RP persists

`pkg/byok/store/models.go`:

```go
type User struct {
    ID          string
    OIDCSubject string // idToken.Subject
    Username    string // preferred_username | name | sub fallback
    Email       string
    CreatedAt, UpdatedAt time.Time
}
```

`UpsertUser` (store.go:17) keys on `OIDCSubject`. tiny-idp's ID token `sub` will
be the tiny-idp account ID; `preferred_username` and `email` claims are produced
by tiny-idp's claims policy. Nothing in the store layer needs to change.

## 5. tiny-idp: what it is and how it embeds

All paths below are in the tiny-idp working tree
`/home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp`. The repo has 729
commits, ~35k Go LOC, 38 TINYIDP-* tickets, and `go test ./...` passes (52
packages).

### 5.1 Supported public composition boundary

Documented in `docs/embedding-foundations.md`. An embedder should use only:

| Package | Responsibility |
| --- | --- |
| `pkg/sqlitestore` | Persistent identity, credential, protocol, consent, session, client, and signing-key state |
| `pkg/idpstore` | Store interfaces and domain records |
| `pkg/idpaccounts` | Account creation, password replacement, password auth, lockout, password-work reporting |
| `pkg/embeddedidp` | Client/key bootstrap, provider construction, lifecycle, readiness, maintenance, in-process issuer HTTP |
| `pkg/idp` | Password policy, audit, rate-limit, client-address, consent, readiness, maintenance contracts |
| `pkg/idpui` | Optional login and consent renderer contract |

Application code **must not** import `internal/authn`, `internal/admin`,
`internal/passwordhash`, `internal/keys`, or `internal/fositeadapter`. Those are
implementation details.

### 5.2 The embedding API surface (`pkg/embeddedidp`)

`Options` (`pkg/embeddedidp/options.go`) is the struct the embedder fills. The
fields an integrator cares about:

```go
type Options struct {
    Issuer         string                 // e.g. "http://127.0.0.1:8080/idp" (dev) or https URL (prod)
    Mode           Mode                   // DevMode | ProductionMode
    Store          idpstore.Store         // from sqlitestore.Open
    Cookie        CookieConfig            // Secure, SameSite, SessionName, CSRFName, Path
    Token         TokenConfig             // SecretKey []byte (>=32 bytes)
    Audit          idp.Sink               // e.g. idp.NewFileAuditSink(path)
    Consent        idp.ConsentPolicy
    Authorization  idp.AuthorizationPolicy
    Claims         idp.ClaimsPolicy       // governs preferred_username/email in ID token
    Presentation   idp.PresentationPolicy
    RateLimiter    idp.RateLimiter
    ClientAddress  idp.ClientAddressResolver
    Authenticator  idp.PasswordAuthenticator // = idpaccounts.Service
    PasswordPolicy idp.PasswordAcceptancePolicy
    PasswordWork   idp.PasswordWorkConfig
    Maintenance    MaintenanceConfig
    UI             UIConfig
    AccountChooser AccountChooserConfig
    Registration   RegistrationConfig
    ScriptedSignup ScriptedSignupConfig
}
```

`ProductionMode` requires an `https` issuer; `DevMode` requires a loopback `http`
issuer (enforced in `internal/oidcmeta/issuer.go:18–34`).

### 5.3 Client declarations (`pkg/embeddedidp/bootstrap.go`)

tiny-idp reconciles declared clients against the store at startup. The helper
constructors an integrator uses:

```go
// Public browser client: authorization_code + refresh_token, PKCE required.
embeddedidp.BrowserClient(
    "llm-proxy-web",                                    // client id
    []string{"http://127.0.0.1:8080/auth/callback"},    // redirect URIs
    []string{"http://127.0.0.1:8080/"},                 // post-logout redirect URIs
    []string{"openid", "profile", "email"},             // allowed scopes
)

// Public device client: device_code grant (RFC 8628) — for coding agents.
embeddedidp.DeviceClient("llm-proxy-device", []string{"openid", "profile", "email"})
```

`BrowserClient` produces a public client with `RequirePKCE: true`,
`AccessTokenTTL: 1h`, `RefreshTokenTTL: 24h`. This is the direct replacement
for the Keycloak confidential client `llm-proxy-web`.

**Notable difference from Keycloak:** tiny-idp browser clients are **public**
(PKCE-only), not confidential-with-secret. The RP code in `oidc.go` supports a
confidential client secret but does not yet implement PKCE; OIDC nonce and PKCE
are separate protections. The selected design uses the supported public client
and adds PKCE S256 to llm-proxy before integration.

### 5.4 Account creation (`pkg/idpaccounts`)

Replaces Keycloak's user store + admin console:

```go
accounts, _ := idpaccounts.NewService(store, idpaccounts.Options{Audit: audit})
accounts.Create(ctx, idpaccounts.CreateRequest{
    Login: "alice", Password: []byte("..."),
    Email: "alice@example.test", EmailVerified: true, Name: "Alice Example",
})
```

Argon2id hashing, lockout, and password-work reporting are built in. The
integrator decides whether to seed an admin account at bootstrap (like the
Keycloak `alice` user) or enable provider-owned signup (the
`RegistrationConfig` option, used by tiny-idp's signup flow).

### 5.5 Discovery shape (`internal/oidcmeta/discovery.go`)

tiny-idp publishes a standard `.well-known/openid-configuration` with:

```
issuer
authorization_endpoint
device_authorization_endpoint
token_endpoint
userinfo_endpoint
introspection_endpoint
introspection_endpoint_auth_methods_supported
jwks_uri
end_session_endpoint
token_endpoint_auth_methods_supported
```

This is a superset of what llm-proxy's `gooidc.NewProvider` discovery consumes
(it reads `issuer`, `authorization_endpoint`, `token_endpoint`, `jwks_uri`, and
the ID-token signing keys). The RP code needs no changes to consume tiny-idp
discovery.

### 5.6 The in-process transport option (`pkg/embeddedidp/inprocess_transport.go`)

This is the key enabler for a **single-binary** topology.
`InProcessIssuerTransport` is an `http.RoundTripper` that dispatches exact-issuer
back-channel requests directly to the provider's `http.Handler` in the same
process — no network hop, no loopback port, no TLS for the back channel. It has
no network fallback and bounds response size.

In the reference embedder (`cmd/tinyidp-xapp/development_app.go:167`):

```go
transport, err := embeddedidp.NewInProcessIssuerTransport(
    issuer, idpProvider.Handler(), embeddedidp.InProcessTransportOptions{},
)
relyingParty, _ := newRelyingParty(rpOptions{
    Issuer: issuer, ClientID: clientID, HTTPClient: &http.Client{Transport: transport},
})
```

For llm-proxy, this would mean: construct the tiny-idp provider inside
`llm-proxy-server`, mount `provider.Handler()` at e.g. `/idp/`, and wire the
`go-oidc` provider's HTTP client to the in-process transport so discovery,
token exchange, JWKS fetch, and userinfo all stay in-process. The browser
redirect flow still goes through the user's browser to the `/idp/` URL.

### 5.7 Reference embedders to copy from

- **Minimal:** `examples/embedded/main.go` — opens a store, bootstraps one
  browser client, constructs the provider, builds an in-process transport, and
  runs an RP. ~120 lines. Start here.
- **xapp (development):** `cmd/tinyidp-xapp/development_app.go` — full
  composition with browser client, device client, in-process issuer transport,
  and a resource API. This is the closest structural analog to what llm-proxy
  would become.
- **xapp (production):** `cmd/tinyidp-xapp/production_app.go` — production mode,
  file audit sink, manifest-driven state, resource auth with introspection.

### 5.8 tiny-idp deployment options today

- **In-process embedded** (the xapp pattern): one binary serves the IdP and the
  RP/application on one listener. tiny-idp's `InProcessIssuerTransport` makes
  this safe and loop-free for the back channel.
- **Standalone process:** the `tinyidp` CLI and `tinyidp-xapp` command both run
  as independent HTTP servers. There is a k3s deployment design
  (`TINYIDP-K3S-MSGDESK-PROD-001`) and a local compose design
  (`TINYIDP-LOCAL-COMPOSE-001`) for multi-app shapes.
- **Docker image:** `TINYIDP-K3S-MSGDESK-001` documents a production image flow.
  tiny-idp's `dist/` has build artifacts.

### 5.9 tiny-idp's own production-readiness status

`TINYIDP-PROD-IMPL-001` tracks the release program. As of the last review
(`TINYIDP-PROD-XGOJA-REVIEW-001`, 2026-07-18), phases 0–4 are complete
(dependency baseline, consumable embedding API, transactional persistence +
backup/restore, mandatory authentication + abuse controls, keys/audit/readiness/
maintenance). Phase 5 (external conformance, signed evidence, independent review,
release-owner approval) is in progress. The candidate build is source `2930981`.

**Implication for llm-proxy:** tiny-idp is production-shaped but not yet
release-stamped. An integration that runs tiny-idp as the BYOK control-plane
IdP should track the Phase 5 completion before any production deploy; for dev
and design work it is usable today.

## 6. The exact integration surface (file by file)

What changes, what stays, by file:

| llm-proxy file | Change? | Notes |
| --- | --- | --- |
| `pkg/byok/web/oidc.go` | **Yes** | Add PKCE S256, issuer-aware identity, and one-time auth transactions. For an in-process topology, also wire the `go-oidc` HTTP client to `InProcessIssuerTransport`. |
| `pkg/byok/web/session.go` | No change | HMAC session cookies are IdP-independent. |
| `pkg/byok/web/api.go` | No change | Management JSON API (credential CRUD, token mint/revoke). |
| `pkg/byok/web/web.go` | Minor | Route registration may need to mount tiny-idp's `Handler()` at `/idp/` for the in-process topology. |
| `cmd/llm-proxy-server/main.go` | **Yes** | Add tiny-idp construction (store, accounts, bootstrap, provider) and either start it as a goroutine or point flags at an external issuer. |
| `deploy/docker-compose.yaml` | **Replace** | Drop Keycloak service; either embed tiny-idp in llm-proxy or add a tiny-idp service. |
| `deploy/keycloak/realm-byok.json` | **Delete** | Replaced by a tiny-idp `BootstrapConfig` (Go) or a tiny-idp state manifest. |
| `pkg/byok/store/*` | No change | User store keyed on `OIDCSubject`; tiny-idp `sub` works. |
| `pkg/byok/authmw/*`, `vault/*`, `meter/*`, `engines/*` | No change | Enforcement data plane is IdP-independent. |
| `go.mod` | **Yes** | Add `github.com/go-go-golems/tiny-idp` dependency (for the in-process topology). |

New files likely needed:

- A tiny-idp bootstrap helper, e.g. `pkg/byok/idp/tinyidp.go`, that owns the
  store/accounts/provider construction and returns an `http.Handler` + optional
  `http.RoundTripper`.
- A replacement for `deploy/keycloak/realm-byok.json` expressing the client +
  initial-account declarations (Go config or a tiny-idp manifest).
- `playbooks/01-tinyidp-oidc-smoke.md` — the live OIDC smoke playbook the
  `LLM-PROXY-BYOK-PROD-READINESS` ticket has been waiting on.

## 7. Topology options and tradeoffs

### 7.1 Option A — In-process embedded (single binary)

llm-proxy embeds tiny-idp via `pkg/embeddedidp`, mounts the provider at `/idp/`,
and uses `InProcessIssuerTransport` for the RP's back-channel HTTP.

**Pros:**
- Single binary, single process, single listener — simplest ops story.
- No network hop for discovery/token/JWKS/userinfo.
- Shares one SQLite store lifecycle with the broker.
- Matches the proven `tinyidp-xapp` pattern.

**Cons:**
- Couples llm-proxy release cadence to tiny-idp's.
- Single active IdP process → no horizontal scaling of the IdP without extra
  work (tiny-idp's SQLite + in-memory coordination components constrain to one
  active replica; documented in `TINYIDP-PROD-XGOJA-REVIEW-001` design-doc 01).
- Blurs the trust boundary: the broker and IdP share a process, so an RCE in
  one compromises both.
- Adds the tiny-idp Go dependency to llm-proxy's `go.mod`.

### 7.2 Option B — Separate tiny-idp process (HTTP discovery)

Run tiny-idp as its own process (standalone `tinyidp`/`tinyidp-xapp`), point
`--byok-oidc-issuer-url` at it. llm-proxy is unchanged except for config.

**Pros:**
- Zero llm-proxy code changes — the RP already handles any OIDC issuer.
- Clean trust boundary; independent release cadence.
- tiny-idp can be scaled/operated independently.
- No new dependency in llm-proxy `go.mod`.

**Cons:**
- Two processes to operate; needs a compose/k3s service for tiny-idp.
- Back-channel HTTP over loopback (fine for dev; needs TLS for prod).
- Must provision/seed tiny-idp with the `llm-proxy-web` client and an initial
  account out of band (tiny-idp CLI or manifest).

### 7.3 Option C — Sidecar in compose/k3s

tiny-idp runs next to llm-proxy in the same pod/compose project, reached over
loopback. Same RP story as Option B, but packaged together.

**Pros:**
- Operational simplicity of "one deploy unit" with the isolation of Option B.
- Matches the existing `deploy/docker-compose.yaml` shape (swap Keycloak for
  tiny-idp).

**Cons:**
- Same back-channel HTTP/TLS considerations.
- Still need a tiny-idp image and bootstrap config.

**Recommendation to evaluate:** Option C for dev (drop-in Keycloak replacement
in compose) and for initial prod; keep Option A open as a future simplification
once tiny-idp reaches release-stamp and the trust-boundary analysis is done.

## 8. Concrete migration steps (sketch, not the design)

1. **Decide topology** (§7). This drives everything else.
2. **Provision tiny-idp state:** open a SQLite store, declare the
   `llm-proxy-web` browser client with redirect URIs matching llm-proxy's
   `--byok-public-url` + `/auth/callback`, create an initial admin account.
3. **Replace `deploy/docker-compose.yaml`** Keycloak service with the chosen
   tiny-idp topology.
4. **Delete `deploy/keycloak/realm-byok.json`.**
5. **Update `cmd/llm-proxy-server/main.go`** wiring (Option A only) or just the
   flag defaults (Options B/C).
6. **Write `playbooks/01-tinyidp-oidc-smoke.md`:** start the stack, log in as
   the seeded user, complete the callback, verify a minted token works against
   `/v1/chat/completions` with the user's stored credential.
7. **Check the `LLM-PROXY-BYOK-PROD-READINESS` tasks** this unblocks: live OIDC
   e2e, OIDC callback check order, session store, meter circuit breaker. The
   first two are directly satisfied by this integration.
8. **Update the `LLM-PROXY-BYOK` architecture proposal** (§Phase 3) to reflect
   tiny-idp as the IdP instead of Keycloak.

## 9. Open questions for the architect

1. **Topology choice.** In-process (A) vs separate process (B) vs sidecar (C).
   What is the deployment target for the first prod — single binary, compose,
   or k3s?
2. **Client model.** Keep a confidential client (defense-in-depth, matches
   Keycloak today) or adopt tiny-idp's public+PKCE browser client (OAuth 2.1
   BCP)? If confidential, tiny-idp needs a confidential-client path; verify
   support (the `BrowserClient` helper is public, but `ClientSpec` allows
   `Public: false`).
3. **Account provisioning.** Seed a single admin account at bootstrap (like
   Keycloak `alice`), or enable tiny-idp's provider-owned signup flow
   (`RegistrationConfig`)? The BYOK control plane currently expects users to
   exist before they can manage credentials.
4. **Claims mapping.** tiny-idp's `ClaimsPolicy` governs what goes into the ID
   token. Confirm `preferred_username` and `email` are populated the way
   `oidc.go:173` expects.
5. **Session store.** The `LLM-PROXY-BYOK-PROD-READINESS` ticket wants a
   server-side session table with revocation. Does tiny-idp's session model
   interact, or does llm-proxy keep its own HMAC-cookie sessions entirely
   independent? (Likely independent — tiny-idp sessions are for the IdP's own
   login UI, llm-proxy's are for the `/app` control plane.)
6. **Device authorization.** tiny-idp supports RFC 8628 device flow. Should the
   BYOK broker offer device-authorized login for coding agents (CLI tools that
   can't open a browser)? This connects to the broader BYOK-for-agents story but
   is out of scope for the initial Keycloak-replacement.
7. **Refresh tokens.** tiny-idp browser clients issue refresh tokens (24h TTL).
   llm-proxy's RP currently only uses the ID token for login. Do we want silent
   refresh / SSO, or is the current "log in once per session" sufficient?
8. **Logout.** tiny-idp publishes `end_session_endpoint`. llm-proxy's
   `handleLogout` just clears its own cookie. Should it redirect through
   tiny-idp's end-session for single-logout?
9. **Production readiness gate.** tiny-idp Phase 5 is in progress. What is the
   gate for using it in a prod BYOK deployment — Phase 5 complete, or an
   independent security review specific to the BYOK use case?

## 10. Key commits and provenance

### llm-proxy (BYOK broker)

- `c898aae` — Merge PR #5 (BYOK control plane + enforcement, Phases 0–3).
- `fb991c7` — test: exercise BYOK through real Geppetto provider path.
- `3dcff7f` — fix: address BYOK review findings (OIDC return redirect hardening,
  per-token budget dispatch serialization, OpenAI Responses key mapping).
- `6b71c01` — byok: control-plane webapp — OIDC login, credential vault UI,
  token minting.
- `ba0fb4c` — byok: add store layer (SQLite + memory) with vault, tokens,
  ledger, audit.
- `e6b3b1f` — byok: enforce minted tokens on the data plane.
- `388cb6d` — byok: per-user credential vault, key injection, usage metering.
- `1327bef` — byok: rewrite CLI on Glazed.
- `044368f` — Docs: LLM-PROXY-BYOK ticket with architecture proposal.

### tiny-idp (IdP)

- 729 commits total; the relevant structural ones are in the
  `TINYIDP-PROD-IMPL-001` and `TINYIDP-PROD-XGOJA-REVIEW-001` tickets.
- Candidate production build: source `2930981`.
- The embedding API is the deliverable of Phase 1 of `TINYIDP-PROD-IMPL-001`.

### byok-host (prior art, design only)

- `d6b3941` (2026-04-20) — Add technical textbook covering broker, PKCE, chat.
- `b5a1218` — Document and prototype delegated BYOK broker.
- `3e22277` — Build delegated BYOK web UI demo.
- `71ebb38` — Implement Keycloak demo foundation with SQLite storage.
- All three byok-host tickets (`BYOK-BROKER`, `BYOK-BROKER-WEB-UI`,
  `BYOK-KEYCLOAK-STORAGE`) are complete as design + prototype and explicitly
  stopped at ticket-scoped smoke code.

## 11. Reference: where to read first

If the architect reads only five things, in order:

1. This document.
2. `LLM-PROXY-BYOK` `design-doc/01-...-prior-art-analysis-and-architecture-proposal.md`
   §Proposed Solution + §Phase 3 — the broker design this IdP serves.
3. tiny-idp `docs/embedding-foundations.md` — the supported embedding boundary.
4. tiny-idp `examples/embedded/main.go` — a complete minimal embedder.
5. llm-proxy `pkg/byok/web/oidc.go` — the RP code that will consume tiny-idp.

Then `cmd/tinyidp-xapp/development_app.go` for the full in-process composition
pattern, and `internal/oidcmeta/discovery.go` + `pkg/embeddedidp/bootstrap.go`
for the client/discovery contracts.

## 12. Validation commands

Quick checks an architect can run to confirm the state described here:

```bash
# llm-proxy builds and BYOK tests pass
cd /home/manuel/code/wesen/go-go-golems/llm-proxy
GOWORK=off go build ./... && GOWORK=off go test ./pkg/byok/... -count=1

# tiny-idp builds and all tests pass
cd /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp
GOWORK=off go build ./... && GOWORK=off go test ./... -count=1

# See the current Keycloak compose that will be replaced
cat /home/manuel/code/wesen/go-go-golems/llm-proxy/deploy/docker-compose.yaml

# See tiny-idp discovery shape
grep -n 'Endpoint\|issuer\|jwks' \
  /home/manuel/workspaces/2026-07-07/prod-tiny-idp/tiny-idp/internal/oidcmeta/discovery.go
```

## 13. What this ticket does NOT decide

- The topology (§7) — that is the architect's first decision.
- Whether to keep a confidential client or go public+PKCE.
- Whether to enable tiny-idp signup or seed accounts manually.
- Any code changes. This is research only; implementation is a separate
  design-doc and subsequent work.
