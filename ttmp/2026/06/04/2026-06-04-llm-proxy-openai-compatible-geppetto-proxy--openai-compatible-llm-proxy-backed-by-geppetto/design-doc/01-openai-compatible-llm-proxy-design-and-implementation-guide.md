---
Title: OpenAI-compatible LLM Proxy Design and Implementation Guide
Ticket: 2026-06-04-llm-proxy-openai-compatible-geppetto-proxy
Status: active
Topics:
    - llm-proxy
    - inference
    - geppetto
    - openai
    - anthropic
DocType: design-doc
Intent: long-term
Owners: []
RelatedFiles:
    - Path: ../../../../../../../../../2026-05-04/llm-proxy-server/geppetto/ttmp/2026/05/04/2026-05-04-llm-proxy-server--generic-llm-proxy-server-design/design-doc/01-generic-llm-proxy-server-design-and-implementation-guide.md
      Note: Preliminary design used as historical context and updated for the new llm-proxy module and profile-store requirements.
    - Path: geppetto/pkg/engineprofiles/source_chain.go
      Note: |-
        Existing chained registry source loader for YAML and SQLite profile sources.
        Profile source chain for YAML/SQLite registry loading
    - Path: geppetto/pkg/engineprofiles/sqlite_store.go
      Note: |-
        Existing SQLite-backed profile store that proves profiles can already be loaded from DB-like persistence.
        SQLite-backed profile store evidence for DB-loadable profiles
    - Path: geppetto/pkg/engineprofiles/store.go
      Note: |-
        Store abstraction that makes profile persistence swappable.
        Swappable profile persistence interface for YAML/DB stores
    - Path: geppetto/pkg/engineprofiles/types.go
      Note: |-
        Existing profile data model with inference settings and extensions.
        Engine profile data model used by the design
    - Path: geppetto/pkg/steps/ai/claude/api/messages.go
      Note: |-
        Current Anthropic Messages request/client reference.
        Anthropic Messages API request/client reference
    - Path: geppetto/pkg/steps/ai/openai/chat_types.go
      Note: |-
        Current OpenAI Chat Completions request/type reference.
        OpenAI Chat Completions wire-shape reference
    - Path: geppetto/pkg/steps/ai/openai_responses/helpers.go
      Note: |-
        Current OpenAI Responses request mapping reference.
        OpenAI Responses request/response mapping reference
    - Path: geppetto/pkg/steps/ai/settings/settings-inference.go
      Note: |-
        Existing inference settings object containing provider credentials, chat defaults, and model metadata.
        Inference settings structure reused by route/profile resolution
    - Path: llm-proxy/cmd/XXX/main.go
      Note: |-
        Template command entrypoint to replace with cmd/llm-proxy-server.
        Template command entrypoint to replace with llm-proxy-server
    - Path: llm-proxy/go.mod
      Note: |-
        Current proxy module is a mostly empty Go module that should host the server binary and library packages.
        Current proxy module boundary and dependency target
ExternalSources:
    - https://platform.openai.com/docs/api-reference/chat/create
    - https://platform.openai.com/docs/api-reference/responses/create
    - https://docs.anthropic.com/en/api/messages
Summary: Design and implementation guide for an OpenAI-compatible proxy server that routes /v1/chat/completions requests to OpenAI Chat Completions, OpenAI Responses, and Anthropic Messages backends using Geppetto profile/settings infrastructure.
LastUpdated: 2026-06-04T19:45:00-04:00
WhatFor: Use this as the onboarding and implementation guide for building the first production-oriented llm-proxy server.
WhenToUse: Read before implementing proxy configuration, routing, auth, OpenAI-compatible ingress, backend adapters, streaming translation, profile storage, or future multi-user credential management.
---


# OpenAI-compatible LLM Proxy Design and Implementation Guide

## Executive Summary

Build `llm-proxy` as a small Go HTTP server that exposes an OpenAI-compatible API to clients and hides provider-specific backend protocols behind Geppetto-powered profile resolution. The first production target is `POST /v1/chat/completions`, because that endpoint is the compatibility contract expected by many tools. The proxy accepts an OpenAI-style request, authenticates the caller with a proxy bearer token, resolves `request.model` to a configured route and Geppetto inference profile, injects the correct upstream provider credential, forwards to OpenAI Chat Completions, OpenAI Responses, or Anthropic Messages, and translates the response back to OpenAI-compatible JSON or SSE.

The most important architectural choice is to split the system into two layers:

1. **Inference profile layer**: reusable, Geppetto-owned model/provider defaults. This layer should be backed by the existing `engineprofiles` abstractions so it can load profiles from YAML today and SQLite or another DB-backed store later.
2. **Proxy application layer**: llm-proxy-owned routes, public model aliases, client-facing auth keys, permission policy, request limits, logging, and adapter-specific behavior.

This separation lets v1 remain simple while avoiding dead-end configuration. Today, one operator may pass a list of profile sources and a YAML route table. Later, each user can own API keys and profiles in a DB, while the proxy exposes a controlled set of bearer keys with permissions, revocation, refresh, model allowlists, and audit trails.

The proxy should reuse Geppetto's settings/profile models and provider mapping knowledge, but it should not run normal HTTP proxy traffic through `engine.Engine` in v1. The `engine.Engine` interface processes Geppetto `Turn` objects and returns updated `Turn` objects (`geppetto/pkg/inference/engine/engine.go:9-15`). That is useful for Geppetto applications, but a protocol proxy must preserve OpenAI wire details: HTTP status codes, error shapes, streaming frame order, tool-call deltas, unknown request fields, and provider-specific response IDs. Direct HTTP adapters are easier to reason about and test for wire compatibility.

## Problem Statement and Scope

### User-facing problem

Client tools often know how to speak only one API shape, most commonly OpenAI Chat Completions. Operators, however, want to route requests to several provider families:

- OpenAI-compatible Chat Completions backends.
- OpenAI Responses backends, especially for modern reasoning models and encrypted reasoning continuation.
- Anthropic Claude Messages backends.

Without a proxy, every client must understand each provider's base URL, credential format, model names, request schema, streaming events, and error bodies. With the proxy, a client can use one base URL, one bearer key, and one model name namespace. The proxy performs routing, credential injection, protocol translation, observability, and policy enforcement.

### Implementation focus for this ticket

This ticket focuses on the actual proxy implementation path, not the full future account-management product. The implementation guide therefore prioritizes:

- HTTP server shape.
- OpenAI-compatible ingress and egress contract.
- Route resolution through Geppetto inference profiles.
- Backend adapters for OpenAI Chat, OpenAI Responses, and Anthropic Messages.
- Streaming translation.
- Operator configuration and testability.
- Early seams for future multi-user keys and permissions.

### In scope for v1

1. **Ingress compatibility**
   - `POST /v1/chat/completions`.
   - `GET /v1/models`.
   - `GET /healthz` and `GET /readyz`.
   - Non-streaming OpenAI-compatible JSON responses.
   - Streaming OpenAI-compatible `text/event-stream` responses when `stream: true`.

2. **Backend protocols**
   - OpenAI Chat Completions passthrough/rewrite.
   - OpenAI Responses translation.
   - Anthropic Messages translation.

3. **Profile-driven routing**
   - Load a list of Geppetto inference profile registries.
   - Resolve public model names to profile references.
   - Apply route-level overrides such as public model name, backend protocol, provider model name, upstream base URL, timeout, and credential reference.
   - Support YAML profile sources first, with SQLite/DB-ready store interfaces preserved.

4. **Security and operations**
   - Proxy bearer auth for clients.
   - Request body limits.
   - Context timeouts and connection cleanup.
   - Outbound URL validation.
   - Redacted structured logs with request IDs.
   - No provider API key leakage to clients or logs.

5. **Testing without live providers**
   - `httptest.Server` fake upstreams for all backend protocols.
   - Golden tests for request/response translation.
   - Streaming tests for SSE frame order, `[DONE]`, errors, and early disconnects.

### Out of scope for v1

- Full user dashboard or self-service key management UI.
- Billing dashboard.
- Organization/team hierarchy beyond a minimal internal `Principal` model.
- Cross-provider automatic failover.
- Full OpenAI Assistants API compatibility.
- Full legacy `/v1/completions` parity. Add only a small compatibility shim after `/v1/chat/completions` is stable, if a concrete client needs it.
- Server-side conversation memory. The proxy should remain stateless for chat requests.

## Terms and Mental Model

- **Ingress**: the protocol spoken by clients to the proxy. For v1 this is OpenAI Chat Completions.
- **Backend**: the upstream provider protocol used by the proxy.
- **Public model name**: the string clients put in `request.model`, such as `sonnet`, `gpt-fast`, or `team-a/coder`.
- **Provider model name**: the model string sent upstream, such as `claude-3-5-sonnet-20241022` or `gpt-4o-mini`.
- **Inference profile**: a Geppetto `EngineProfile` containing provider settings, chat defaults, model metadata, and extension data.
- **Route**: proxy-owned mapping from public model name to a profile reference plus backend protocol/credential policy.
- **Adapter**: code that converts the OpenAI-compatible ingress request to one backend protocol and converts the backend response back to OpenAI-compatible output.
- **Principal**: authenticated client identity derived from a proxy bearer key.
- **Credential resolver**: component that returns upstream provider credentials allowed for the principal and route.

## Current-State Architecture Evidence

### Workspace and module state

The current workspace is `/home/manuel/workspaces/2026-06-04/llm-proxy` and contains `geppetto`, `glazed`, `pinocchio`, and `llm-proxy` in one Go workspace (`go.work`). The `llm-proxy` module is currently a template-style module with `module github.com/go-go-golems/llm-proxy`, Go/toolchain declarations, and only `logcopter` as a direct dependency (`llm-proxy/go.mod:1-20`). Its command entrypoint is still `cmd/XXX/main.go` with an empty `main()` (`llm-proxy/cmd/XXX/main.go:1-5`), and its library package is effectively empty (`llm-proxy/pkg/doc.go:1-5`).

This means the proxy should be implemented in the `llm-proxy` module, not buried in Geppetto. Geppetto should remain the reusable inference/settings/profile library; llm-proxy should own server runtime, auth, HTTP compatibility, and deployment shape.

### Existing Geppetto inference settings

Geppetto already has an `InferenceSettings` object that groups the settings a proxy route needs. `APISettings` stores provider API keys and base URLs (`geppetto/pkg/steps/ai/settings/settings-inference.go:43-46`). `InferenceSettings` includes API, chat, OpenAI, Claude, Gemini, Ollama, embeddings, generic inference overrides, and model metadata (`settings-inference.go:59-80`). `ChatSettings` includes the model/engine name, API type, max response tokens, sampling defaults, stop sequences, and structured output fields (`geppetto/pkg/steps/ai/settings/settings-chat.go:22-44`).

The implication is that llm-proxy should not invent a second complete provider-settings model. It should use Geppetto's settings for provider defaults and add only proxy-specific configuration around them.

### Existing profile abstraction and DB readiness

Geppetto's `EngineProfile` is a named preset with a slug, optional stack references, `InferenceSettings`, metadata, and an `Extensions` map (`geppetto/pkg/engineprofiles/types.go:34-43`). A registry groups profiles and has a default profile selector (`types.go:45-53`). This is already the right shape for loading named model/provider profiles.

The persistence boundary is explicit. `EngineProfileStoreReader`, `EngineProfileStoreWriter`, and `EngineProfileStore` define list/get/upsert/delete operations (`geppetto/pkg/engineprofiles/store.go:12-32`). There is a YAML file store (`geppetto/pkg/engineprofiles/file_store_yaml.go:11-20`) and an SQLite store (`geppetto/pkg/engineprofiles/sqlite_store.go:27-64`). The SQLite store loads registry JSON from `profile_registries`, validates it, keeps an in-memory copy, and persists updates back to SQLite (`sqlite_store.go:164-233`). A source chain can load multiple registry sources and supports `yaml`, `sqlite`, and `sqlite-dsn` source kinds (`geppetto/pkg/engineprofiles/source_chain.go:14-20`), then aggregates them with explicit precedence (`source_chain.go:78-156`).

This directly satisfies the user's requirement that the inference profile layer be configurable so profiles can later be loaded from a DB. The proxy should depend on the `Registry`/store interfaces, not concrete YAML files.

### Existing provider engine factory

Geppetto's `StandardEngineFactory` chooses provider implementations based on `settings.Chat.ApiType` (`geppetto/pkg/inference/engine/factory/factory.go:95-107`). It can instantiate OpenAI Chat, OpenAI Responses, Claude/Anthropic, and Gemini engines (`factory.go:124-137`) and reports provider names including `openai`, `open-responses`, `openai-responses`, `claude`, and `anthropic` (`factory.go:158-169`).

This is important as provider vocabulary evidence, but it should not be the proxy request path. The factory returns `engine.Engine`, whose interface is `RunInference(ctx, *turns.Turn) (*turns.Turn, error)` (`geppetto/pkg/inference/engine/engine.go:11-15`). The proxy needs HTTP wire fidelity, not only final turn state.

### Existing OpenAI Chat support

Geppetto already models many OpenAI Chat Completions request fields: `model`, `messages`, token limits, sampling fields, `stream`, `stop`, `stream_options`, penalties, seed, response format, tools, tool choice, parallel tool calls, reasoning effort, and thinking controls (`geppetto/pkg/steps/ai/openai/chat_types.go:13-34`). Message/tool structs include text or multimodal content, tool calls, tool-call IDs, and function definitions (`chat_types.go:42-120`).

The existing OpenAI streaming helper resolves API key/base URL, appends `/chat/completions`, validates the outbound URL, sets `Content-Type`, accepts `text/event-stream`, and injects `Authorization: Bearer ...` (`geppetto/pkg/steps/ai/openai/chat_stream.go:54-102`). This is a useful reference for outbound behavior.

### Existing OpenAI Responses support

The Responses package models a request with `model`, `input`, text format, `max_output_tokens`, sampling, stop sequences, reasoning, include, tools, tool choice, parallel tool calls, `stream`, store, and service tier (`geppetto/pkg/steps/ai/openai_responses/helpers.go:16-31`). It also models input message/function-call items and output items (`helpers.go:52-123`). Its engine builds a request from Geppetto turns and settings, including model, input, token/sampling fields, reasoning effort, reasoning summary, encrypted reasoning include, structured output, and per-turn overrides (`helpers.go:181-311`). Streaming code normalizes older reasoning event names such as `response.reasoning.delta` to `response.reasoning_text.delta` (`geppetto/pkg/steps/ai/openai_responses/stream_events.go:15-24`).

The proxy adapter should borrow these mapping rules, but it should expose its own request/response conversion directly between OpenAI Chat wire structs and Responses wire structs.

### Existing Anthropic Messages support

Geppetto's Anthropic API model has `MessageRequest` fields for `model`, `messages`, `max_tokens`, stop sequences, `stream`, `system`, sampling, thinking, tools, top-k/top-p, and output format (`geppetto/pkg/steps/ai/claude/api/messages.go:18-33`). Its client sends to `/v1/messages`, validates outbound URLs, sets provider headers, and supports both non-streaming and streaming calls (`messages.go:165-245`).

The proxy adapter should translate OpenAI Chat messages to this Messages shape and translate Anthropic content blocks/events back into OpenAI-style chat completions.

### Existing outbound URL safety

Geppetto already has `security.ValidateOutboundURL`, which accepts HTTPS by default, optionally allows HTTP, rejects unsupported schemes, rejects empty hosts, and rejects local/private/link-local targets unless local networks are explicitly allowed (`geppetto/pkg/security/outbound_url.go:10-58`). The proxy should reuse this for upstream provider URLs because routes and profiles may eventually be user-controlled.

## Proposed Architecture

### High-level runtime diagram

```text
Client tool
  │
  │  Authorization: Bearer proxy_key_...
  │  POST /v1/chat/completions { model: "sonnet", stream: true, ... }
  ▼
┌──────────────────────────────────────────────────────────────────────┐
│ llm-proxy HTTP server                                                │
│                                                                      │
│  ┌──────────────┐    ┌──────────────┐    ┌───────────────────────┐  │
│  │ authn/authz  │ -> │ request parse│ -> │ route/profile resolve │  │
│  └──────────────┘    └──────────────┘    └──────────┬────────────┘  │
│                                                      │               │
│                                                      ▼               │
│                                          ┌───────────────────────┐  │
│                                          │ credential resolver   │  │
│                                          └──────────┬────────────┘  │
│                                                     │               │
│        ┌────────────────────────────────────────────┼─────────────┐ │
│        ▼                                            ▼             ▼ │
│ ┌──────────────┐                         ┌────────────────┐ ┌───────────────┐
│ │ OpenAI Chat  │                         │ OpenAI         │ │ Anthropic     │
│ │ adapter      │                         │ Responses      │ │ Messages      │
│ └──────┬───────┘                         │ adapter        │ │ adapter       │
│        │                                 └───────┬────────┘ └───────┬───────┘
└────────┼─────────────────────────────────────────┼──────────────────┼────────┘
         ▼                                         ▼                  ▼
  /v1/chat/completions                       /v1/responses       /v1/messages
  OpenAI-compatible backend                  OpenAI backend      Anthropic backend
```

### Layered configuration diagram

```text
┌────────────────────────────────────────────────────────────────────┐
│ Proxy app config                                                   │
│ - bind address, limits, timeouts                                   │
│ - route table: public model -> backend protocol + profile ref      │
│ - auth key sources and permissions                                 │
│ - credential source policy                                         │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ references
                               ▼
┌────────────────────────────────────────────────────────────────────┐
│ Geppetto profile registry chain                                    │
│ - YAML source today                                                │
│ - SQLite source today                                              │
│ - DB-backed EngineProfileStore later                               │
│ - stack merge and resolved InferenceSettings                       │
└──────────────────────────────┬─────────────────────────────────────┘
                               │ produces
                               ▼
┌────────────────────────────────────────────────────────────────────┐
│ Effective route runtime                                            │
│ - public model name                                                │
│ - backend protocol                                                 │
│ - provider model name and model capability metadata                │
│ - upstream base URL and credential ref                             │
│ - sampling/token defaults                                          │
│ - policy tags and audit metadata                                   │
└────────────────────────────────────────────────────────────────────┘
```

### Recommended package layout

Implement the proxy in the `llm-proxy` module, with internal packages that keep wire protocol code separate from auth/config/runtime concerns.

```text
llm-proxy/
  cmd/
    llm-proxy-server/
      main.go                         # cobra command or simple flags; starts Server
  pkg/
    proxy/
      config.go                       # app config structs, YAML load, env expansion
      server.go                       # http.Server construction and route registration
      middleware.go                   # request ID, auth, body limits, logging, panic recovery
      handler_chat.go                 # POST /v1/chat/completions
      handler_models.go               # GET /v1/models
      errors.go                       # OpenAI-compatible error helpers
      sse.go                          # SSE reader/writer helpers
      route.go                        # RouteResolver and EffectiveRoute
      profiles.go                     # thin wrapper around geppetto engineprofiles.Registry
      credentials.go                  # CredentialResolver interface and env/static implementations
      policy.go                       # Principal, Permission, Authorizer
      observability.go                # request/upstream metadata events
    openaiwire/
      chat.go                         # local OpenAI-compatible request/response structs
      stream.go                       # chat completion chunk structs
      errors.go                       # OpenAI error response structs
    adapters/
      backend.go                      # Backend interface
      openai_chat.go                  # OpenAI Chat backend adapter
      openai_responses.go             # Responses backend adapter
      anthropic_messages.go           # Anthropic Messages backend adapter
      mapping_tools.go                # shared tool-call mapping helpers
      mapping_usage.go                # usage normalization helpers
      testdata/                       # golden mapping fixtures
```

Use `pkg/openaiwire` even though Geppetto has `openai.ChatCompletionRequest`. The existing Geppetto structs are useful references, but a proxy wire package should preserve unknown fields and flexible content with `json.RawMessage` where needed. That prevents the proxy from accidentally dropping client fields before an adapter decides whether a backend supports them.

## Configuration Design

### Configuration principles

1. **Profiles describe model/provider defaults.** They should be reusable by other Geppetto apps.
2. **Routes describe proxy exposure.** They decide which public model names are visible and which backend adapter handles them.
3. **Credentials are resolved late.** Route/profile config can point at credential refs, but the adapter should receive only the resolved secret for the authenticated principal.
4. **Stores are injected by interface.** The proxy should not assume YAML forever; it should accept registry source specs and later a DB-backed `EngineProfileStore`.
5. **User and bearer-key concepts exist from day one.** v1 can implement a static key file, but the runtime should expose `Principal` and `Authorizer` interfaces so DB auth can replace static auth without rewiring handlers.

### Example v1 config

```yaml
server:
  listen: "127.0.0.1:8080"
  public_base_url: "http://127.0.0.1:8080"
  read_timeout: 10s
  write_timeout: 10m
  idle_timeout: 120s
  max_request_body_bytes: 4194304
  allow_local_upstreams: false

profile_sources:
  # Parsed by geppetto/pkg/engineprofiles source-chain code.
  # v1 can support only yaml first, but keep the field as a list.
  - kind: yaml
    path: "./profiles/default.yaml"
  - kind: sqlite
    path: "./var/profiles.sqlite"

routes:
  - public_model: "gpt-fast"
    display_name: "Fast OpenAI-compatible model"
    profile:
      registry: "default"
      slug: "openai-fast"
    backend_protocol: "openai_chat"
    provider_model: "gpt-4o-mini"
    upstream_base_url: "https://api.openai.com/v1"
    credential_ref: "provider/openai/default"
    client_model_echo: "public"       # public | provider
    enabled: true
    policy_tags: ["chat", "fast"]

  - public_model: "gpt-reasoning"
    profile:
      registry: "default"
      slug: "openai-reasoning"
    backend_protocol: "openai_responses"
    provider_model: "gpt-5"
    upstream_base_url: "https://api.openai.com/v1"
    credential_ref: "provider/openai/default"
    enabled: true
    policy_tags: ["chat", "reasoning"]

  - public_model: "sonnet"
    profile:
      registry: "default"
      slug: "claude-sonnet"
    backend_protocol: "anthropic_messages"
    provider_model: "claude-3-5-sonnet-20241022"
    upstream_base_url: "https://api.anthropic.com"
    credential_ref: "provider/anthropic/default"
    enabled: true
    policy_tags: ["chat", "anthropic"]

credentials:
  sources:
    - kind: env
      mappings:
        "provider/openai/default": "OPENAI_API_KEY"
        "provider/anthropic/default": "ANTHROPIC_API_KEY"

auth:
  static_bearer_keys:
    - key_env: "LLM_PROXY_ADMIN_KEY"
      principal_id: "operator"
      scopes: ["models:list", "chat:complete", "admin:routes:read"]
      allowed_models: ["*"]
    - key_env: "LLM_PROXY_FAST_ONLY_KEY"
      principal_id: "fast-client"
      scopes: ["models:list", "chat:complete"]
      allowed_models: ["gpt-fast"]
```

### Profile YAML sketch

Use Geppetto's existing profile fields. Route-specific data can live either in proxy route config or in `extensions.llm_proxy`, but v1 should prefer route config so profiles remain reusable.

```yaml
slug: default
display_name: Default proxy profiles
default_profile_slug: openai-fast
profiles:
  openai-fast:
    slug: openai-fast
    display_name: OpenAI fast defaults
    inference_settings:
      chat:
        api_type: openai
        engine: gpt-4o-mini
        max_response_tokens: 4096
        temperature: 0.2
      api:
        base_urls:
          openai-base-url: https://api.openai.com/v1
      model_info:
        context_window: 128000
        supports_tools: true

  claude-sonnet:
    slug: claude-sonnet
    display_name: Claude Sonnet defaults
    inference_settings:
      chat:
        api_type: claude
        engine: claude-3-5-sonnet-20241022
        max_response_tokens: 4096
        temperature: 0.2
      api:
        base_urls:
          claude-base-url: https://api.anthropic.com
      model_info:
        context_window: 200000
        supports_tools: true
```

### Config structs pseudocode

```go
type Config struct {
    Server         ServerConfig       `yaml:"server"`
    ProfileSources []ProfileSource    `yaml:"profile_sources"`
    Routes         []RouteConfig      `yaml:"routes"`
    Credentials    CredentialConfig   `yaml:"credentials"`
    Auth           AuthConfig         `yaml:"auth"`
}

type RouteConfig struct {
    PublicModel     string     `yaml:"public_model"`
    DisplayName     string     `yaml:"display_name"`
    Profile         ProfileRef `yaml:"profile"`
    BackendProtocol string     `yaml:"backend_protocol"` // openai_chat | openai_responses | anthropic_messages
    ProviderModel   string     `yaml:"provider_model"`
    UpstreamBaseURL string     `yaml:"upstream_base_url"`
    CredentialRef   string     `yaml:"credential_ref"`
    ClientModelEcho string     `yaml:"client_model_echo"` // public | provider
    Enabled         bool       `yaml:"enabled"`
    PolicyTags      []string   `yaml:"policy_tags"`
}

type ProfileRef struct {
    Registry string `yaml:"registry"`
    Slug     string `yaml:"slug"`
}

type EffectiveRoute struct {
    RouteConfig       RouteConfig
    ResolvedProfile   *engineprofiles.ResolvedEngineProfile
    InferenceSettings *settings.InferenceSettings
}
```

### Validation rules

Validate configuration at startup before listening:

- `server.listen` is non-empty.
- `server.max_request_body_bytes` has a sane upper bound.
- At least one profile source is configured.
- At least one enabled route exists.
- Every enabled route has a unique `public_model`.
- Every route's `backend_protocol` is known.
- Every route's profile resolves through `engineprofiles.Registry.ResolveEngineProfile`.
- The effective provider model is non-empty. Prefer `route.provider_model`; fall back to `InferenceSettings.Chat.Engine` only if present.
- The upstream base URL is non-empty. Prefer `route.upstream_base_url`; fall back to the provider base URL in `InferenceSettings.API.BaseUrls`.
- Outbound URL validation passes for the adapter endpoint.
- Every credential ref resolves to a non-empty secret for static v1 startup checks, unless a dynamic user credential store is enabled.
- Static proxy bearer keys are non-empty and do not duplicate.

## Public HTTP API

### `GET /healthz`

Use for process liveness. It should not check provider reachability.

Response:

```json
{"status":"ok"}
```

### `GET /readyz`

Use for readiness. It should verify that config loaded, route resolver initialized, profile registry initialized, and required static credential sources are usable. It should not call upstream LLM providers on every request.

Response:

```json
{
  "status": "ready",
  "routes": 3,
  "profile_sources": 2
}
```

### `GET /v1/models`

Return OpenAI-compatible model listing for models the authenticated principal may use.

```json
{
  "object": "list",
  "data": [
    {
      "id": "sonnet",
      "object": "model",
      "created": 0,
      "owned_by": "llm-proxy",
      "permission": []
    }
  ]
}
```

Authorization behavior:

- Missing/invalid bearer token returns OpenAI-compatible 401.
- Valid token with no visible routes returns an empty `data` array, not a 403.

### `POST /v1/chat/completions`

Primary endpoint. Request body follows OpenAI Chat Completions enough for typical clients:

```json
{
  "model": "sonnet",
  "messages": [
    {"role":"system","content":"You are concise."},
    {"role":"user","content":"Explain Go interfaces."}
  ],
  "stream": true,
  "temperature": 0.2,
  "max_tokens": 500,
  "tools": [
    {
      "type":"function",
      "function": {
        "name":"lookup",
        "description":"Lookup a fact",
        "parameters":{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}
      }
    }
  ]
}
```

Non-streaming response:

```json
{
  "id": "chatcmpl_proxy_...",
  "object": "chat.completion",
  "created": 1780616700,
  "model": "sonnet",
  "choices": [
    {
      "index": 0,
      "message": {"role":"assistant","content":"..."},
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
}
```

Streaming response:

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache

id: chatcmpl_proxy_...
data: {"id":"chatcmpl_proxy_...","object":"chat.completion.chunk","created":1780616700,"model":"sonnet","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"id":"chatcmpl_proxy_...","object":"chat.completion.chunk","created":1780616700,"model":"sonnet","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: [DONE]
```

### Error shape

Always return OpenAI-compatible errors to clients, even if the backend error came from Anthropic or another provider.

```json
{
  "error": {
    "message": "model not found: sonnet-private",
    "type": "invalid_request_error",
    "param": "model",
    "code": "model_not_found"
  }
}
```

Recommended mappings:

| Condition | Status | type | code |
|---|---:|---|---|
| Missing bearer token | 401 | authentication_error | missing_bearer_token |
| Invalid bearer token | 401 | authentication_error | invalid_bearer_token |
| Principal lacks `chat:complete` | 403 | permission_error | insufficient_scope |
| Model not configured | 404 | invalid_request_error | model_not_found |
| Model configured but not allowed | 403 | permission_error | model_not_allowed |
| Invalid JSON/body too large | 400 / 413 | invalid_request_error | invalid_request / request_too_large |
| Upstream timeout | 504 | api_error | upstream_timeout |
| Upstream 429 | 429 | rate_limit_error | upstream_rate_limited |
| Upstream 5xx | 502 | api_error | upstream_error |

## Core Runtime Components

### Server

Owns `http.Server`, config, route resolver, auth middleware, backend registry, and logger.

```go
type Server struct {
    cfg         Config
    routes      RouteResolver
    auth        Authenticator
    authorizer  Authorizer
    creds       CredentialResolver
    backends    map[BackendProtocol]adapters.Backend
    httpClient  *http.Client
    log         zerolog.Logger
}
```

### Route resolver

The route resolver is a pure component: given a public model name and principal, return an effective route or an error. It should not call upstream providers.

```go
type RouteResolver interface {
    Resolve(ctx context.Context, principal Principal, model string) (*EffectiveRoute, error)
    ListVisible(ctx context.Context, principal Principal) ([]ModelDescriptor, error)
}
```

Resolution order:

1. Normalize exact public model string by trimming whitespace only. Do not lowercase model IDs by default; model IDs are case-sensitive enough that exact matching is safest.
2. Find enabled route by `public_model`.
3. Ask `Authorizer` whether principal can use it.
4. Resolve profile through Geppetto registry.
5. Merge route overrides with profile settings into `EffectiveRoute`.
6. Return a clone/copy so request-specific changes do not mutate shared config.

### Backend interface

Keep adapters behind a small interface that preserves streaming vs non-streaming behavior.

```go
type Backend interface {
    Complete(ctx context.Context, req BackendRequest) (*openaiwire.ChatCompletionResponse, error)
    Stream(ctx context.Context, req BackendRequest) (<-chan openaiwire.StreamEvent, error)
}

type BackendRequest struct {
    Principal       Principal
    Route           *EffectiveRoute
    Credential      ResolvedCredential
    Ingress         *openaiwire.ChatCompletionRequest
    RequestID       string
    ReceivedAt      time.Time
}
```

Do not put `http.ResponseWriter` inside adapters. The handler should own HTTP response writing, and adapters should return domain events/chunks. This makes adapters testable without a live HTTP server.

### Auth interfaces

Design the v1 static auth in the same shape as future DB auth.

```go
type Principal struct {
    ID            string
    Subject       string
    Organization  string
    Scopes        []string
    AllowedModels []string
    Metadata      map[string]string
}

type Authenticator interface {
    Authenticate(ctx context.Context, bearerToken string) (Principal, error)
}

type Authorizer interface {
    CanListModels(ctx context.Context, p Principal) error
    CanUseRoute(ctx context.Context, p Principal, route RouteConfig) error
}
```

For v1, `StaticBearerAuthenticator` can hash configured keys at startup and compare with constant-time comparison. Never store plaintext bearer keys in logs, metrics, or errors.

### Credential resolver

The credential resolver is the seam for future per-user provider keys.

```go
type CredentialResolver interface {
    ResolveProviderCredential(ctx context.Context, p Principal, route *EffectiveRoute) (ResolvedCredential, error)
}

type ResolvedCredential struct {
    Provider string
    Token    string
    Headers  map[string]string // optional provider-specific auth headers
    Source   string            // env, db, vault; log source only, not secret
}
```

v1 can implement:

- `EnvCredentialResolver`: maps credential refs to environment variables.
- `StaticCredentialResolver`: loads refs from config for local development only; avoid this for production.

Future implementation can add:

- `UserCredentialResolver`: resolves provider keys owned by the authenticated user.
- `ServiceCredentialResolver`: resolves organization-level shared keys.
- `CompositeCredentialResolver`: tries user key, then org key, then operator key according to route policy.

## Request Flow Pseudocode

### Server startup

```go
func main() {
    cfg := LoadConfig(flags.ConfigPath)
    logger := NewLogger(flags.LogLevel)

    sourceSpecs := ConvertProxyProfileSources(cfg.ProfileSources)
    profileRegistry := engineprofiles.NewChainedRegistryFromSourceSpecs(ctx, sourceSpecs)
    defer profileRegistry.Close()

    credentialResolver := NewCredentialResolver(cfg.Credentials)
    authenticator := NewAuthenticator(cfg.Auth)
    authorizer := NewAuthorizer(cfg.Auth)

    routeResolver := NewRouteResolver(cfg.Routes, profileRegistry, authorizer)
    if err := routeResolver.Validate(ctx); err != nil { fatal(err) }

    srv := proxy.NewServer(proxy.ServerOptions{
        Config: cfg,
        RouteResolver: routeResolver,
        Authenticator: authenticator,
        Authorizer: authorizer,
        CredentialResolver: credentialResolver,
        HTTPClient: NewHTTPClient(cfg.Server),
        Logger: logger,
    })

    httpServer := &http.Server{
        Addr: cfg.Server.Listen,
        Handler: srv.Routes(),
        ReadTimeout: cfg.Server.ReadTimeout,
        WriteTimeout: cfg.Server.WriteTimeout,
        IdleTimeout: cfg.Server.IdleTimeout,
    }
    httpServer.ListenAndServe()
}
```

### Chat handler

```go
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    requestID := RequestIDFromContext(ctx)

    principal, err := PrincipalFromContext(ctx)
    if err != nil { writeOpenAIError(w, 401, err); return }

    limited := http.MaxBytesReader(w, r.Body, s.cfg.Server.MaxRequestBodyBytes)
    ingress, unknownFields, err := openaiwire.DecodeChatCompletionRequest(limited)
    if err != nil { writeOpenAIError(w, 400, err); return }
    if strings.TrimSpace(ingress.Model) == "" { writeOpenAIErrorParam(w, "model", ...); return }
    if len(ingress.Messages) == 0 { writeOpenAIErrorParam(w, "messages", ...); return }

    route, err := s.routes.Resolve(ctx, principal, ingress.Model)
    if err != nil { writeRouteError(w, err); return }

    credential, err := s.creds.ResolveProviderCredential(ctx, principal, route)
    if err != nil { writeOpenAIError(w, 403 or 500, err); return }

    backend := s.backends[route.RouteConfig.BackendProtocol]
    req := adapters.BackendRequest{
        Principal: principal,
        Route: route,
        Credential: credential,
        Ingress: ingress,
        UnknownFields: unknownFields,
        RequestID: requestID,
        ReceivedAt: time.Now(),
    }

    if ingress.Stream {
        events, err := backend.Stream(ctx, req)
        if err != nil { writeOpenAIError(w, mapAdapterError(err)); return }
        s.writeChatSSE(w, r, route, events)
        return
    }

    resp, err := backend.Complete(ctx, req)
    if err != nil { writeOpenAIError(w, mapAdapterError(err)); return }
    writeJSON(w, 200, resp)
}
```

### Streaming writer

```go
func (s *Server) writeChatSSE(w http.ResponseWriter, r *http.Request, route *EffectiveRoute, events <-chan openaiwire.StreamEvent) {
    flusher, ok := w.(http.Flusher)
    if !ok { writeOpenAIError(w, 500, "streaming unsupported"); return }

    h := w.Header()
    h.Set("Content-Type", "text/event-stream")
    h.Set("Cache-Control", "no-cache")
    h.Set("Connection", "keep-alive")
    w.WriteHeader(http.StatusOK)

    for {
        select {
        case <-r.Context().Done():
            return
        case ev, ok := <-events:
            if !ok {
                fmt.Fprintf(w, "data: [DONE]\n\n")
                flusher.Flush()
                return
            }
            if ev.Err != nil {
                // After headers are sent, encode as SSE error chunk if possible and close.
                fmt.Fprintf(w, "data: %s\n\n", MarshalStreamError(ev.Err))
                fmt.Fprintf(w, "data: [DONE]\n\n")
                flusher.Flush()
                return
            }
            fmt.Fprintf(w, "data: %s\n\n", ev.JSON)
            flusher.Flush()
        }
    }
}
```

## Backend Adapter Design

### Adapter 1: OpenAI Chat Completions

This adapter is mostly pass-through.

Request mapping:

- Endpoint: `{upstream_base_url}/chat/completions`, where base URL usually already includes `/v1`.
- Method: `POST`.
- Authorization: `Authorization: Bearer <resolved token>`.
- Body: OpenAI-compatible request with `model` rewritten from public model to provider model.
- Preserve common fields and unknown fields unless explicitly forbidden by route policy.
- Honor `stream` exactly as requested by the client.

Response mapping:

- Non-streaming: parse enough to rewrite `model` to public model if `client_model_echo: public`. Otherwise pass through provider model.
- Streaming: parse SSE frames and rewrite `model` in chunks; pass through `[DONE]`.
- Error: map upstream OpenAI error object to client response, but redact upstream details that could leak credentials or internal URLs.

Pseudocode:

```go
func (b *OpenAIChatBackend) Complete(ctx context.Context, req BackendRequest) (*ChatCompletionResponse, error) {
    body := req.Ingress.CloneRaw()
    body["model"] = req.Route.ProviderModel()
    body["stream"] = false

    upstreamResp := b.postJSON(ctx, req.Route.ChatCompletionsURL(), req.Credential, body)
    if upstreamResp.StatusCode >= 300 { return nil, mapOpenAIUpstreamError(upstreamResp) }

    chat := decodeChatCompletion(upstreamResp.Body)
    rewriteResponseModel(&chat, req.Route)
    ensureProxyIDIfEmpty(&chat)
    return &chat, nil
}
```

### Adapter 2: OpenAI Responses

This adapter translates Chat Completions ingress to Responses.

Request mapping:

| OpenAI Chat field | Responses field | Notes |
|---|---|---|
| `model` | `model` | Use provider model. |
| `messages` | `input` | Convert message sequence to Responses message items. |
| `max_completion_tokens` | `max_output_tokens` | Prefer over `max_tokens` if both present. |
| `max_tokens` | `max_output_tokens` | Fallback. |
| `temperature` | `temperature` | Omit for reasoning models if model metadata marks it unsupported. |
| `top_p` | `top_p` | Omit for reasoning models if needed. |
| `stop` | `stop_sequences` | Normalize string or array to array. |
| `response_format.json_schema` | `text.format` | Map schema/name/strict. |
| `tools` | `tools` | Convert function tools to Responses function tools. |
| `tool_choice` | `tool_choice` | Preserve when shape is supported. |
| `parallel_tool_calls` | `parallel_tool_calls` | Preserve. |
| `stream` | `stream` | Exact client value. |
| `reasoning_effort` | `reasoning.effort` | Also support profile defaults. |

Message mapping rules:

- `system` messages become input message items with role `system` if backend accepts it; otherwise merge leading system text into the first user input or route-level `instructions`. Make this an adapter option because Responses providers differ.
- `user` text becomes `input_text` content parts.
- `user` image parts become `input_image` content parts.
- `assistant` text becomes output-history message items if Responses accepts them as `input`; preserve assistant tool calls as `function_call` items.
- `tool` messages become `function_call_output` items with `call_id`/`tool_call_id` and `output`.

Non-streaming response mapping:

- `response.output` message text becomes one OpenAI choice message with role `assistant` and concatenated content.
- `function_call` outputs become OpenAI `tool_calls` on the assistant message.
- `usage` maps to OpenAI usage where possible; preserve raw usage in an extension field only if needed.
- `finish_reason` should map from provider status/reason. Default to `stop`; use `tool_calls` when tool calls are present.

Streaming mapping:

| Responses event | OpenAI chat chunk |
|---|---|
| `response.created` | optional initial role chunk |
| `response.output_text.delta` | `choices[0].delta.content` |
| `response.reasoning_text.delta` | extension field such as `choices[0].delta.reasoning_content` if enabled |
| `response.output_item.added` function_call | start `tool_calls[index]` with id/name/type |
| `response.function_call_arguments.delta` | append tool call arguments delta |
| `response.completed` | final chunk with finish reason, then `[DONE]` |
| `response.failed` | SSE error chunk then `[DONE]` |

Responses adapter pseudocode:

```go
func buildResponsesRequest(req BackendRequest) map[string]any {
    in := req.Ingress
    out := map[string]any{
        "model": req.Route.ProviderModel(),
        "input": mapMessagesToResponsesInput(in.Messages),
        "stream": in.Stream,
    }
    if max := in.MaxCompletionTokensOrMaxTokens(); max != nil {
        out["max_output_tokens"] = *max
    }
    if shouldForwardSampling(req.Route, "temperature") && in.Temperature != nil {
        out["temperature"] = *in.Temperature
    }
    if shouldForwardSampling(req.Route, "top_p") && in.TopP != nil {
        out["top_p"] = *in.TopP
    }
    if len(in.Stop) > 0 { out["stop_sequences"] = in.Stop }
    if in.ResponseFormat.JSONSchema != nil {
        out["text"] = mapJSONSchemaResponseFormat(in.ResponseFormat)
    }
    if len(in.Tools) > 0 { out["tools"] = mapToolsToResponses(in.Tools) }
    if in.ToolChoice != nil { out["tool_choice"] = in.ToolChoice }
    return out
}
```

### Adapter 3: Anthropic Messages

Request mapping:

| OpenAI Chat field | Anthropic field | Notes |
|---|---|---|
| `model` | `model` | Use provider model. |
| leading `system` messages | `system` | Concatenate with double newline. |
| non-system `messages` | `messages` | Convert to Anthropic role/content blocks. |
| `max_completion_tokens`/`max_tokens` | `max_tokens` | Anthropic requires a max token value; use profile default if missing. |
| `temperature` | `temperature` | Pointer field in Geppetto model. |
| `top_p` | `top_p` | Pointer field. |
| `stop` | `stop_sequences` | Normalize to list. |
| `tools` | `tools` | Function name/description/schema to Anthropic tool. |
| `tool_choice` | provider-specific `tool_choice` | Add to local wire model if Geppetto type lacks it. |
| `stream` | `stream` | Exact client value. |

Anthropic message mapping pseudocode:

```go
func mapChatMessagesToAnthropic(messages []ChatMessage) (system string, out []AnthropicMessage, err error) {
    for _, m := range messages {
        switch m.Role {
        case "system":
            system = appendSystem(system, m.Text())
        case "user":
            out = append(out, AnthropicMessage{Role: "user", Content: mapUserParts(m.Content)})
        case "assistant":
            blocks := []ContentBlock{}
            if txt := m.Text(); txt != "" { blocks = append(blocks, TextBlock(txt)) }
            for _, tc := range m.ToolCalls {
                blocks = append(blocks, ToolUseBlock{ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(tc.Function.Arguments)})
            }
            out = append(out, AnthropicMessage{Role: "assistant", Content: blocks})
        case "tool":
            out = append(out, AnthropicMessage{Role: "user", Content: []ContentBlock{
                ToolResultBlock{ToolUseID: m.ToolCallID, Content: m.Text()},
            }})
        default:
            return "", nil, fmt.Errorf("unsupported role %q", m.Role)
        }
    }
    return system, coalesceAdjacentSameRole(out), nil
}
```

Non-streaming response mapping:

- `text` content blocks concatenate into `message.content`.
- `tool_use` blocks become `message.tool_calls`.
- Anthropic `stop_reason` maps:
  - `end_turn` -> `stop`.
  - `max_tokens` -> `length`.
  - `tool_use` -> `tool_calls`.
  - `stop_sequence` -> `stop`.
- Usage maps `input_tokens` to `prompt_tokens`, `output_tokens` to `completion_tokens`, total as sum. Cache token fields can be omitted from OpenAI usage or added under an internal extension later.

Streaming mapping:

| Anthropic event | OpenAI chat chunk |
|---|---|
| `message_start` | role chunk and id/model metadata |
| `content_block_start` text | no-op or initial content chunk |
| `content_block_delta` text_delta | `delta.content` |
| `content_block_start` tool_use | start `delta.tool_calls[index]` with id/type/name |
| `content_block_delta` input_json_delta | append `delta.tool_calls[index].function.arguments` |
| `content_block_stop` | no-op |
| `message_delta` stop_reason | final finish-reason chunk |
| `message_stop` | `[DONE]` |

## Future Multi-user Auth and Credential Architecture

The long-term product will have many users, each with their own provider keys, and proxy bearer keys that can be scoped, refreshed, revoked, and permissioned. Do not build the whole product in v1, but put these seams in place now.

### Future data model sketch

```text
users
  id, email, display_name, created_at, disabled_at

provider_credentials
  id, owner_user_id, provider, encrypted_secret, label,
  created_at, updated_at, disabled_at, last_used_at

proxy_keys
  id, owner_user_id, key_hash, label,
  scopes, allowed_models, allowed_profile_registries,
  expires_at, revoked_at, refresh_family_id, created_at, last_used_at

profile_registries
  id, owner_user_id or org_id, slug, payload_json, updated_at

model_routes
  id, owner_user_id or org_id, public_model, profile_registry, profile_slug,
  backend_protocol, credential_policy, enabled, policy_tags

audit_events
  id, principal_id, route_id, provider, model, request_id,
  status, prompt_tokens, completion_tokens, latency_ms, created_at
```

### Permission model

Minimum scopes:

- `models:list` — can call `GET /v1/models`.
- `chat:complete` — can call `POST /v1/chat/completions`.
- `admin:routes:read` — can inspect route configuration.
- `admin:keys:manage` — can create/revoke proxy keys.
- `admin:profiles:manage` — can edit profiles.
- `credentials:manage` — can add/remove provider credentials.

Authorization decision input:

```go
type AuthorizationInput struct {
    Principal Principal
    Action    string
    Route     *EffectiveRoute
    Model     string
    Profile   ProfileRef
}
```

Important v1 design rule: even if static auth is simple, handlers should only know about `Principal` and `Authorizer`, not static config internals.

### Bearer-key refresh design

For future refreshable keys, do not make provider credentials double as proxy bearer keys. Proxy keys are separate objects with their own hashes and expiry. A refresh operation should rotate the bearer key, preserve permissions, and invalidate the old key atomically.

Recommended constraints:

- Store only key hashes, never plaintext keys.
- Show plaintext key only once at creation.
- Use constant-time comparison.
- Include key ID prefix in the token string for fast DB lookup, e.g. `llmp_ks_abc123.secretpart`.
- Keep revocation and expiry checks in `Authenticator`.
- Log `key_id`, not key value.

## Design Decisions

### Decision: Implement server in `llm-proxy`, reuse Geppetto as a library

- **Context:** The workspace now has a dedicated `llm-proxy` module, while Geppetto contains reusable inference/provider/profile code.
- **Options considered:** Put the proxy inside Geppetto; implement it in llm-proxy and import Geppetto; create a third module.
- **Decision:** Implement the HTTP server and proxy-specific code in `llm-proxy`, importing Geppetto settings/profile packages.
- **Rationale:** `llm-proxy` is currently empty and intended for this server. Geppetto should remain reusable provider infrastructure. This also keeps auth/HTTP product concerns out of the inference library.
- **Consequences:** `llm-proxy/go.mod` must add a dependency on `github.com/go-go-golems/geppetto` and probably Cobra/YAML packages. Release builds must work outside the local `go.work` by pinning module versions.
- **Status:** proposed

### Decision: Use profile-store interfaces, not YAML-only config

- **Context:** The user explicitly wants the inference profile layer configurable so profiles can later be loaded from a DB.
- **Options considered:** Embed all inference settings in one proxy YAML; use YAML profile files only; use Geppetto `EngineProfileStore`/registry interfaces.
- **Decision:** Use Geppetto profile registry interfaces and accept a list of profile source specs.
- **Rationale:** Geppetto already has `EngineProfileStore`, YAML store, SQLite store, and source-chain support. The proxy can start with YAML and SQLite without changing handler code later.
- **Consequences:** Startup config validation must resolve profile refs. Route config remains separate from profile storage. Future DB auth/profile stores can replace static source specs behind the same resolver boundary.
- **Status:** proposed

### Decision: Direct HTTP adapters instead of `engine.Engine` for v1 proxy traffic

- **Context:** `engine.Engine` consumes and produces Geppetto `Turn` objects, but a protocol proxy needs wire compatibility.
- **Options considered:** Use `engine.Engine`; fork existing provider clients; write direct HTTP adapters that reuse types/mapping knowledge.
- **Decision:** Write direct HTTP adapters for OpenAI Chat, OpenAI Responses, and Anthropic Messages.
- **Rationale:** Direct adapters preserve status codes, streaming chunk boundaries, tool-call deltas, unknown fields, and provider-specific errors. They are also easy to test with fake HTTP upstreams.
- **Consequences:** Some mapping logic may duplicate provider engine logic initially. Mitigate by sharing small pure mapping helpers later if stable.
- **Status:** proposed

### Decision: Keep route config separate from inference profiles

- **Context:** Profiles should be reusable, while routes are proxy exposure policy.
- **Options considered:** Put everything in `EngineProfile.Extensions`; keep all route metadata in proxy config; split fields between profile and route.
- **Decision:** Keep public model aliases, backend protocol, credential refs, auth policy tags, and model visibility in proxy route config. Use profiles for inference/provider defaults.
- **Rationale:** This avoids coupling normal Geppetto model profiles to one server's public API and permission model.
- **Consequences:** Effective route resolution must merge route overrides with profile settings. Documentation and tests must make precedence clear.
- **Status:** proposed

### Decision: Introduce auth/credential interfaces in v1 even with static implementations

- **Context:** Future users will manage their own provider keys and proxy bearer keys.
- **Options considered:** Hard-code one global proxy key and provider keys in env; implement full DB auth immediately; define interfaces with static implementations.
- **Decision:** Define `Authenticator`, `Authorizer`, and `CredentialResolver` interfaces now, with static/env implementations for v1.
- **Rationale:** This adds little complexity but prevents handler signatures and adapter requests from assuming one global operator identity.
- **Consequences:** v1 tests should cover principal-scoped model filtering and denied route access.
- **Status:** proposed

## File-Level Implementation Plan

### Phase 1: Module cleanup and server skeleton

Files:

- `llm-proxy/cmd/llm-proxy-server/main.go`
- `llm-proxy/pkg/proxy/config.go`
- `llm-proxy/pkg/proxy/server.go`
- `llm-proxy/pkg/proxy/middleware.go`
- `llm-proxy/pkg/proxy/errors.go`
- `llm-proxy/go.mod`
- `llm-proxy/Makefile`

Work:

1. Replace `cmd/XXX` with `cmd/llm-proxy-server`.
2. Add CLI flags:
   - `--config`.
   - `--listen` override.
   - `--log-level`.
   - `--allow-local-upstreams` for local fake-provider testing only.
3. Load YAML config and validate it.
4. Build `http.ServeMux` routes for `/healthz`, `/readyz`, `/v1/models`, and `/v1/chat/completions`.
5. Add request ID middleware and redacted zerolog logging.
6. Add body size limiting.
7. Add OpenAI-compatible error writer.

Validation:

```bash
cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
go test ./...
go run ./cmd/llm-proxy-server --config ./examples/proxy.yaml --log-level debug
curl -s http://127.0.0.1:8080/healthz
```

### Phase 2: Profile and route resolution

Files:

- `llm-proxy/pkg/proxy/profiles.go`
- `llm-proxy/pkg/proxy/route.go`
- `llm-proxy/pkg/proxy/policy.go`
- `llm-proxy/pkg/proxy/credentials.go`
- `llm-proxy/examples/proxy.yaml`
- `llm-proxy/examples/profiles.yaml`

Work:

1. Convert proxy `profile_sources` into Geppetto `RegistrySourceSpec` values.
2. Build `engineprofiles.ChainedRegistry` from source specs.
3. Implement `RouteResolver.Validate` to resolve every route at startup.
4. Implement static auth and static authorizer.
5. Implement env credential resolver.
6. Add `/v1/models` model filtering by principal.

Validation:

- Unit test duplicate public models.
- Unit test unresolved profile ref.
- Unit test allowed/denied model visibility.
- Unit test missing credential env var.

### Phase 3: OpenAI wire package

Files:

- `llm-proxy/pkg/openaiwire/chat.go`
- `llm-proxy/pkg/openaiwire/stream.go`
- `llm-proxy/pkg/openaiwire/errors.go`

Work:

1. Define OpenAI-compatible request, response, chunk, usage, message, tool-call, and error structs.
2. Preserve flexible fields (`content`, `tool_choice`, unknown extras) with `json.RawMessage` or `map[string]json.RawMessage`.
3. Add helpers:
   - `MaxCompletionTokensOrMaxTokens()`.
   - `StopAsStrings()`.
   - `CloneRawWithModel(providerModel)`.
   - `MessageTextParts()`.
4. Add golden decode/encode tests.

Validation:

- Ensure a representative OpenAI Chat request round-trips without losing unknown fields.
- Ensure message content can be string or array.
- Ensure tool call arguments remain raw JSON strings.

### Phase 4: OpenAI Chat backend

Files:

- `llm-proxy/pkg/adapters/backend.go`
- `llm-proxy/pkg/adapters/openai_chat.go`
- `llm-proxy/pkg/adapters/openai_chat_test.go`

Work:

1. Implement non-streaming passthrough with model rewrite.
2. Implement streaming passthrough with chunk model rewrite and `[DONE]` handling.
3. Map upstream errors to OpenAI-compatible proxy errors.
4. Reuse `security.ValidateOutboundURL` before upstream calls.

Validation:

- Fake upstream receives provider model and provider bearer token.
- Client receives public model.
- Streaming test verifies role chunk, content chunks, finish chunk, `[DONE]`.

### Phase 5: OpenAI Responses backend

Files:

- `llm-proxy/pkg/adapters/openai_responses.go`
- `llm-proxy/pkg/adapters/mapping_tools.go`
- `llm-proxy/pkg/adapters/openai_responses_test.go`
- `llm-proxy/pkg/adapters/testdata/responses_*.golden.json`

Work:

1. Build Responses request from OpenAI Chat request.
2. Support text, image, tools, tool results, structured output, token/sampling fields, reasoning effort.
3. Implement non-streaming response mapping.
4. Implement streaming event mapping.
5. Normalize known event aliases.

Validation:

- Golden tests for request mapping.
- Fake upstream non-streaming response maps to OpenAI chat response.
- Fake upstream streaming text maps to chat chunks.
- Fake upstream tool-call argument deltas map to OpenAI tool-call deltas.

### Phase 6: Anthropic Messages backend

Files:

- `llm-proxy/pkg/adapters/anthropic_messages.go`
- `llm-proxy/pkg/adapters/anthropic_messages_test.go`
- `llm-proxy/pkg/adapters/testdata/anthropic_*.golden.json`

Work:

1. Convert OpenAI messages to Anthropic `system` plus `messages`.
2. Convert OpenAI function tools to Anthropic tools.
3. Require/fill `max_tokens` from request or profile default.
4. Implement non-streaming content/tool mapping.
5. Implement Anthropic SSE event mapping.
6. Set Anthropic headers including API key and API version.

Validation:

- System prompt extraction test.
- Adjacent role coalescing test.
- Tool use/tool result round-trip test.
- Streaming text/tool-use event test.

### Phase 7: Integration smoke tests and examples

Files:

- `llm-proxy/examples/README.md`
- `llm-proxy/examples/proxy.yaml`
- `llm-proxy/examples/profiles.yaml`
- `llm-proxy/pkg/proxy/integration_test.go`

Work:

1. Start proxy with fake upstreams.
2. Call `/v1/models` and `/v1/chat/completions` with static bearer key.
3. Assert policy and credential behavior.
4. Document local smoke commands.

Validation:

```bash
cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
go test ./... -count=1
go run ./cmd/llm-proxy-server --config examples/proxy.yaml --allow-local-upstreams --log-level debug
```

## Testing Strategy

### Unit tests

- Config load/validation.
- Profile source conversion.
- Route resolver.
- Static bearer auth.
- Authorizer model allowlists.
- Env credential resolver.
- OpenAI error writer.
- SSE parser/writer.

### Adapter golden tests

Use input request JSON and expected backend request JSON:

```text
pkg/adapters/testdata/
  responses_basic_request.input.json
  responses_basic_request.golden.json
  anthropic_system_tools.input.json
  anthropic_system_tools.golden.json
```

Golden tests should compare normalized JSON, not byte-for-byte formatting.

### Fake upstream tests

Each backend adapter should have tests that start an `httptest.Server`, capture request headers/body, and return deterministic provider responses.

Key assertions:

- Upstream receives provider model, not public model.
- Upstream receives provider credential, not proxy bearer token.
- Client response echoes public model if configured.
- Error status and body are mapped correctly.
- Context cancellation closes upstream streams.

### Streaming tests

Streaming is the highest-risk part. Test these cases explicitly:

1. OpenAI Chat upstream sends normal chunks and `[DONE]`.
2. Responses upstream sends text deltas, usage, completion.
3. Responses upstream sends tool-call start and argument deltas.
4. Anthropic upstream sends message start, content deltas, message delta, message stop.
5. Upstream returns error before headers are sent.
6. Upstream emits malformed SSE after headers are sent.
7. Client disconnects mid-stream.

### Manual smoke test

```bash
export LLM_PROXY_ADMIN_KEY=test-proxy-key
export OPENAI_API_KEY=sk-...
export ANTHROPIC_API_KEY=sk-ant-...

cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
go run ./cmd/llm-proxy-server --config examples/proxy.yaml --log-level debug

curl -sS http://127.0.0.1:8080/v1/models \
  -H 'Authorization: Bearer test-proxy-key' | jq .

curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Authorization: Bearer test-proxy-key' \
  -H 'Content-Type: application/json' \
  -d '{"model":"sonnet","messages":[{"role":"user","content":"Say hello"}],"stream":false}' | jq .
```

## Risks and Mitigations

### Risk: streaming semantics differ across providers

Providers do not emit the same event boundaries. OpenAI Chat streams chat chunks, Responses streams typed response events, and Anthropic streams message/content-block events. Tool calls are especially different.

Mitigation:

- Keep streaming mapping in adapter-specific code.
- Use fake upstream tests with exact event sequences.
- Emit OpenAI-compatible chunks conservatively.
- Always terminate successful streams with `[DONE]`.

### Risk: profile and route precedence becomes unclear

Both profiles and routes can specify model/base URL/defaults. Confusing precedence can cause wrong model routing or wrong credentials.

Mitigation:

- Define precedence explicitly: request fields > route overrides > resolved profile settings > adapter defaults.
- Log effective route metadata with secrets redacted.
- Add validation errors for ambiguous missing provider model or base URL.

### Risk: provider secrets leak

The proxy handles both proxy bearer keys and upstream provider keys.

Mitigation:

- Never forward client `Authorization` upstream.
- Never return upstream credential errors with secret values.
- Redact headers and body fields in logs.
- Store only hashes for proxy keys.
- Keep provider credential resolution isolated in one component.

### Risk: using local/private upstream URLs becomes SSRF

Future user-managed routes and profiles could point upstream URLs at local services.

Mitigation:

- Reuse `security.ValidateOutboundURL`.
- Default `allow_local_upstreams` to false.
- Allow local upstreams only under explicit development flag.
- Revalidate final adapter endpoint, not only base URL.

### Risk: direct adapters duplicate Geppetto engine mapping

Direct adapters may duplicate some mapping logic from existing Geppetto engines.

Mitigation:

- Start with direct code for wire fidelity.
- Extract pure mapping helpers only after behavior is tested and stable.
- Keep references to Geppetto provider code in comments/tests.

## Alternatives Considered

### Alternative 1: Use Geppetto `engine.Engine` for every request

This would convert OpenAI Chat requests into `Turn` objects, call a provider engine, then convert the resulting turn back to OpenAI responses.

Rejected for v1 because it loses too much wire-level control. The proxy must preserve streaming chunks, tool-call deltas, HTTP errors, usage details, and unknown request fields. `engine.Engine` remains valuable for future non-wire-compatible endpoints or internal Geppetto execution.

### Alternative 2: Put all configuration in one proxy YAML file

This would be quick but conflicts with the user's requirement that inference profiles later load from a DB. It also duplicates Geppetto's existing profile model.

Rejected. Use proxy YAML only for server/route/auth concerns and use Geppetto profile sources for inference settings.

### Alternative 3: Implement full multi-user DB auth now

This would match the long-term vision but delay the actual proxy.

Rejected for v1. Instead, define interfaces and static implementations. The data model and method signatures should anticipate DB auth, but v1 should ship a working operator-managed proxy first.

### Alternative 4: Use a third-party LLM proxy

Third-party proxies may already provide OpenAI-compatible routing.

Rejected for this project because the user explicitly wants to use the Geppetto implementation and to integrate with Geppetto inference profiles, future profile stores, and user-managed credentials.

## API References

### OpenAI Chat Completions

- Endpoint: `POST /v1/chat/completions`.
- Request concepts: model, messages, tools, tool_choice, response_format, stream.
- Response concepts: `chat.completion`, `chat.completion.chunk`, choices, delta, usage, finish_reason.
- Reference: <https://platform.openai.com/docs/api-reference/chat/create>

### OpenAI Responses

- Endpoint: `POST /v1/responses`.
- Request concepts: model, input, tools, text.format, reasoning, stream, max_output_tokens.
- Streaming concepts: response events such as output text deltas, function call argument deltas, completion/failure.
- Reference: <https://platform.openai.com/docs/api-reference/responses/create>

### Anthropic Messages

- Endpoint: `POST /v1/messages`.
- Request concepts: model, system, messages, content blocks, tools, max_tokens, stream.
- Streaming concepts: message start, content block start/delta/stop, message delta, message stop.
- Reference: <https://docs.anthropic.com/en/api/messages>

## Review Checklist for the Intern

Start review in this order:

1. `llm-proxy/pkg/openaiwire`: confirm request/response structs preserve OpenAI compatibility.
2. `llm-proxy/pkg/proxy/config.go`: confirm startup validation prevents ambiguous routes.
3. `llm-proxy/pkg/proxy/route.go`: confirm route/profile merge precedence.
4. `llm-proxy/pkg/proxy/policy.go`: confirm unauthorized models are not visible or callable.
5. `llm-proxy/pkg/proxy/credentials.go`: confirm client bearer token never goes upstream.
6. `llm-proxy/pkg/adapters/openai_chat.go`: confirm pass-through semantics and model rewrite.
7. `llm-proxy/pkg/adapters/openai_responses.go`: confirm request/stream mapping.
8. `llm-proxy/pkg/adapters/anthropic_messages.go`: confirm system/tool/stream mapping.
9. `llm-proxy/pkg/proxy/handler_chat.go`: confirm non-streaming vs streaming control flow and error behavior.

Validation commands:

```bash
cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
go test ./... -count=1
go run ./cmd/llm-proxy-server --config examples/proxy.yaml --allow-local-upstreams --log-level debug
curl -sS -H 'Authorization: Bearer test-proxy-key' http://127.0.0.1:8080/v1/models | jq .
```

## Open Questions

1. Should v1 support a legacy `/v1/completions` text endpoint, or should we wait for a concrete client that requires it?
2. Should public model names be globally unique, user-scoped, or organization-scoped in the first DB-backed version?
3. Should route config allow provider-specific extra headers, and if yes, which headers are safe to expose to operators?
4. Should Responses reasoning deltas be exposed through `delta.reasoning_content`, `delta.content` with markers, or a proxy extension flag?
5. Which Anthropic API version header should be the default for v1 deployments?

## References

### Key source files

- `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/go.mod`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/cmd/XXX/main.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/types.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/store.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/sqlite_store.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/source_chain.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/settings/settings-inference.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/settings/settings-chat.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai/chat_types.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai/chat_stream.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai_responses/helpers.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai_responses/stream_events.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/claude/api/messages.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/security/outbound_url.go`

### Historical context

- `/home/manuel/workspaces/2026-05-04/llm-proxy-server/geppetto/ttmp/2026/05/04/2026-05-04-llm-proxy-server--generic-llm-proxy-server-design/design-doc/01-generic-llm-proxy-server-design-and-implementation-guide.md`
