---
Title: Simple Geppetto Engine OpenAI Completions Proxy Prototype
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
    - Path: geppetto/pkg/cli/bootstrap/engine_settings.go
      Note: |-
        Existing example of resolving profiles, merging settings, and constructing Geppetto engines from settings.
        Existing profile-to-engine construction pattern
    - Path: geppetto/pkg/engineprofiles/registry.go
      Note: |-
        Existing profile resolver interface; request model strings should resolve as profile slugs.
        Profile resolver interface used to treat completions model as profile slug
    - Path: geppetto/pkg/engineprofiles/source_chain.go
      Note: |-
        Existing YAML/SQLite profile source chain; prototype should load Geppetto profile YAML through this seam.
        Profile YAML/source loading seam
    - Path: geppetto/pkg/events/canonical_events.go
      Note: |-
        Canonical text streaming event types to map to OpenAI Completions stream chunks.
        Canonical text events to translate into OpenAI Completions SSE
    - Path: geppetto/pkg/events/context.go
      Note: |-
        Context event-sink attachment used to stream Geppetto events during RunInference.
        Context-attached event sink mechanism for streaming completions
    - Path: geppetto/pkg/inference/engine/engine.go
      Note: |-
        Core Geppetto inference engine interface that the prototype should use directly.
        Geppetto engine execution boundary for the prototype
    - Path: geppetto/pkg/inference/engine/factory/factory.go
      Note: |-
        Existing provider factory that creates engines from resolved Geppetto inference settings.
        Standard factory for engines from profile inference settings
    - Path: geppetto/pkg/inference/engine/run_with_result.go
      Note: |-
        Helper that runs an engine and returns canonical inference metadata for final OpenAI Completions output.
        RunInferenceWithResult helper for canonical final metadata
    - Path: geppetto/pkg/turns/helpers_blocks.go
      Note: |-
        Convenience constructors for prompt and completion blocks.
        Convenience constructors for prompt and generated text blocks
    - Path: geppetto/pkg/turns/types.go
      Note: |-
        Turn and block data model used as the bridge between prompt input and Geppetto engines.
        Turn/block bridge data model
    - Path: llm-proxy/cmd/XXX/main.go
      Note: Template command entrypoint to replace with cmd/llm-proxy-server.
    - Path: llm-proxy/go.mod
      Note: |-
        Current proxy module that should host the simple prototype server.
        Proxy module that should host the Completions-first prototype
    - Path: llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md
      Note: |-
        Previous Responses-focused design preserved as the later-phase plan; this document is the Completions-first copy/adaptation.
        Preserved Responses-later design copied/adapted into this Completions-first design
ExternalSources:
    - https://platform.openai.com/docs/api-reference/completions/create
Summary: Simplified first prototype design for an OpenAI Completions-compatible endpoint that selects Geppetto profiles by model/profile slug, runs Geppetto inference engines directly, and maps Geppetto turns/events back to legacy OpenAI Completions JSON/SSE.
LastUpdated: 2026-06-05T00:40:00-04:00
WhatFor: Use this as the current first-prototype implementation guide; OpenAI Responses support is deferred and preserved in design doc 02.
WhenToUse: Read before implementing the first llm-proxy prototype that focuses on Geppetto engine execution and OpenAI Completions compatibility.
---


# Simple Geppetto Engine OpenAI Completions Proxy Prototype

## Executive Summary

The first prototype should expose the **legacy OpenAI Completions API** (`POST /v1/completions`), not OpenAI Responses. OpenAI Responses remains the next phase and is preserved in `02-simple-geppetto-engine-openai-responses-proxy-prototype.md`.

This prototype is intentionally small:

1. Accept an OpenAI Completions request.
2. Treat `request.model` as a Geppetto profile slug.
3. Resolve that profile from Geppetto profile YAML through a tiny `ProfileResolver` interface.
4. Construct a Geppetto `engine.Engine` from the profile's `InferenceSettings`.
5. Convert the `prompt` string into a Geppetto `turns.Turn`.
6. Run `engine.RunInferenceWithResult`.
7. Convert generated assistant text blocks into an OpenAI Completions response.
8. If `stream: true`, translate Geppetto text events into OpenAI Completions SSE chunks.

There is no route config, no provider adapter layer, no OpenAI Responses mapper in the first implementation, and no proxy-specific provider setup. Provider model names, API types, base URLs, keys, sampling defaults, and token limits are all configured in Geppetto profile YAML.

## Why Completions First

OpenAI Completions is simpler than OpenAI Responses because the request and response shape is mostly plain text:

```text
prompt string -> generated text
```

The proxy does not need to model Responses `input` item arrays, output item types, reasoning items, function-call output items, annotations, or response lifecycle objects. The first implementation can prove the core bridge with a minimal contract:

```text
OpenAI Completions HTTP request
        ↓
model == Geppetto profile slug
        ↓
profile YAML resolves provider/model/settings
        ↓
Geppetto engine runs inference
        ↓
OpenAI Completions JSON or SSE response
```

Once this works, `/v1/responses` can reuse the same profile resolver, engine provider, inference runner, event sink pattern, and much of the turn-output mapping.

## Strict Prototype Scope

### In scope

- `POST /v1/completions`.
- Optional `GET /healthz`.
- Optional `GET /v1/models` listing profile slugs from the loaded registry.
- Load Geppetto profile YAML/source chain.
- Treat `request.model` as `engineprofiles.EngineProfileSlug`.
- Resolve the profile using Geppetto's profile registry interface.
- Construct a Geppetto engine with the standard engine factory.
- Convert a text prompt into a Geppetto `Turn`.
- Run inference through Geppetto's engine interface.
- Convert final generated assistant text into OpenAI Completions JSON.
- For `stream: true`, attach a Geppetto event sink and convert text deltas into OpenAI Completions SSE chunks.
- Keep only tiny interfaces for future profile/provider/model expansion.

### Out of scope

- No `/v1/responses` in the first prototype.
- No `/v1/chat/completions` in the first prototype unless a concrete client forces it.
- No direct provider HTTP adapters in llm-proxy.
- No route alias table.
- No proxy-specific provider config.
- No user accounts, refreshable bearer keys, or permissions.
- No tool-call mapping in the Completions response.
- No `logprobs`, `best_of`, `echo`, `suffix`, or multi-prompt batching in Phase 1.
- No exact OpenAI billing/usage parity.

## Current Code Evidence

### Profiles are the model/provider setup seam

`geppetto/pkg/engineprofiles/registry.go` defines `ResolveInput`, `ResolvedEngineProfile`, and the `Registry` interface. The proxy should resolve `request.model` as an `EngineProfileSlug` and receive `InferenceSettings` from the resolved profile.

This keeps model/provider setup in Geppetto profile YAML and gives us a future seam for DB-backed or user-specific profile stores.

### Geppetto already constructs engines from settings

`geppetto/pkg/cli/bootstrap/engine_settings.go` shows the existing pattern for resolving a profile, merging inference settings, and constructing an engine through the standard factory. The prototype should reuse that idea, but avoid unnecessary CLI complexity.

The core runtime step is:

```go
eng, err := factory.NewStandardEngineFactory().CreateEngine(resolved.InferenceSettings)
```

### Geppetto engine is the inference boundary

`geppetto/pkg/inference/engine/engine.go` defines:

```go
type Engine interface {
    RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error)
}
```

The prototype should run the engine through `engine.RunInferenceWithResult` from `geppetto/pkg/inference/engine/run_with_result.go`, so it gets normalized inference metadata when available.

### Geppetto streams through event sinks

`geppetto/pkg/events/context.go` provides `events.WithEventSinks(ctx, sinks...)`. Provider engines publish canonical streaming events to sinks attached to the context.

For OpenAI Completions streaming, the proxy only needs the simplest event subset:

- `EventTextSegmentStarted` — optional; may initialize stream state.
- `EventTextDelta` — maps to a Completions stream chunk with `choices[0].text`.
- `EventTextSegmentFinished` — may produce the final finish-reason chunk.
- `EventProviderCallFinished` — source for usage/finish metadata if available.
- `EventError` — maps to stream failure handling.

### Geppetto turns are enough for prompt/completion mapping

`geppetto/pkg/turns/types.go` defines `Turn` and `Block`. `geppetto/pkg/turns/helpers_blocks.go` provides constructors such as `turns.NewUserTextBlock` and `turns.NewAssistantTextBlock`.

For Phase 1, a Completions prompt maps to one user text block. Generated assistant text blocks map back to the `choices[].text` field.

## Proposed Prototype Architecture

```text
┌────────────────────────────┐
│ OpenAI Completions client  │
│ POST /v1/completions       │
│ { model: "sonnet",        │
│   prompt: "..." }          │
└──────────────┬─────────────┘
               │
               ▼
┌────────────────────────────┐
│ llm-proxy handler          │
│ - parse request            │
│ - model is profile slug    │
│ - stream true/false        │
└──────────────┬─────────────┘
               │
               ▼
┌────────────────────────────┐
│ ProfileResolver            │
│ Resolve(ctx, "sonnet")     │
│ -> InferenceSettings       │
└──────────────┬─────────────┘
               │
               ▼
┌────────────────────────────┐
│ EngineProvider             │
│ factory.CreateEngine(...)  │
└──────────────┬─────────────┘
               │
               ▼
┌────────────────────────────┐
│ Completions prompt → Turn  │
│ turns.NewUserTextBlock     │
└──────────────┬─────────────┘
               │
               ▼
┌────────────────────────────┐
│ Geppetto Engine            │
│ RunInferenceWithResult     │
│ + optional EventSink       │
└──────────────┬─────────────┘
               │
               ▼
┌────────────────────────────┐
│ Turn/events → Completions  │
│ JSON or text_completion SSE│
└────────────────────────────┘
```

## Minimal Startup Inputs

No route config. No provider config. Only tell the server where profiles live and what address to listen on.

Recommended command:

```bash
llm-proxy-server \
  --listen 127.0.0.1:8080 \
  --profiles ./profiles.yaml
```

If we want source-chain syntax immediately:

```bash
llm-proxy-server \
  --listen 127.0.0.1:8080 \
  --profile-sources yaml:./profiles.yaml
```

The important runtime rule is:

```text
OpenAI Completions request field `model` == Geppetto engine profile slug.
```

## Profile YAML Example

This remains pure Geppetto profile configuration. The proxy does not add routes or provider config around it.

```yaml
slug: default
display_name: Prototype profiles
default_profile_slug: sonnet
profiles:
  sonnet:
    slug: sonnet
    display_name: Claude Sonnet through Geppetto
    inference_settings:
      chat:
        api_type: claude
        engine: claude-3-5-sonnet-20241022
        max_response_tokens: 4096
        temperature: 0.2
      api:
        api_keys:
          claude-api-key: ${ANTHROPIC_API_KEY}
        base_urls:
          claude-base-url: https://api.anthropic.com

  gpt5:
    slug: gpt5
    display_name: OpenAI Responses engine through Geppetto
    inference_settings:
      chat:
        api_type: open-responses
        engine: gpt-5
        max_response_tokens: 4096
      api:
        api_keys:
          openai-api-key: ${OPENAI_API_KEY}
        base_urls:
          openai-base-url: https://api.openai.com/v1
```

If `${ENV}` expansion is not supported by the current Geppetto profile loader, use the existing Geppetto-supported credential/config resolution path instead of adding proxy-specific provider config.

## Tiny Interfaces to Keep

The goal is not to build a framework. These interfaces exist only to avoid baking profile loading and engine construction directly into the HTTP handler.

### ProfileResolver

```go
type ProfileResolver interface {
    ResolveProfile(ctx context.Context, slug string) (*ResolvedProfileRuntime, error)
    ListProfiles(ctx context.Context) ([]ProfileDescriptor, error)
}

type ResolvedProfileRuntime struct {
    RegistrySlug string
    ProfileSlug  string
    Settings     *settings.InferenceSettings
    Metadata     map[string]any
}
```

V1 implementation wraps `engineprofiles.Registry`:

```go
func (r *GeppettoProfileResolver) ResolveProfile(ctx context.Context, slug string) (*ResolvedProfileRuntime, error) {
    profileSlug, err := engineprofiles.ParseEngineProfileSlug(slug)
    if err != nil { return nil, err }

    resolved, err := r.registry.ResolveEngineProfile(ctx, engineprofiles.ResolveInput{
        EngineProfileSlug: profileSlug,
    })
    if err != nil { return nil, err }

    return &ResolvedProfileRuntime{
        RegistrySlug: resolved.RegistrySlug.String(),
        ProfileSlug:  resolved.EngineProfileSlug.String(),
        Settings:     resolved.InferenceSettings,
        Metadata:     resolved.Metadata,
    }, nil
}
```

Future replacements can load profiles from DB, apply user overlays, or add aliasing without changing the handler.

### EngineProvider

```go
type EngineProvider interface {
    EngineForProfile(ctx context.Context, profile *ResolvedProfileRuntime) (engine.Engine, error)
}
```

V1 implementation:

```go
type FactoryEngineProvider struct {
    factory factory.EngineFactory
}

func (p *FactoryEngineProvider) EngineForProfile(ctx context.Context, profile *ResolvedProfileRuntime) (engine.Engine, error) {
    return p.factory.CreateEngine(profile.Settings)
}
```

Start without caching. Add caching later only if engine creation shows up in profiling or provider clients are expensive to create.

### CompletionMapper

```go
type CompletionMapper interface {
    RequestToTurn(req *CompletionRequest) (*turns.Turn, error)
    TurnToCompletion(req *CompletionRequest, out *turns.Turn, result *engine.InferenceResult, preBlockCount int) (*CompletionResponse, error)
}
```

V1 implementation handles:

- prompt as string,
- generated assistant text blocks,
- usage metadata if available.

### CompletionEventTranslator

```go
type CompletionEventTranslator interface {
    TranslateEvent(ev events.Event) ([]CompletionSSEFrame, error)
    FinalFrame(result *engine.InferenceResult) *CompletionSSEFrame
}
```

V1 implementation handles `EventTextDelta` and final finish reason. Unknown non-error events are ignored.

## HTTP API

### `POST /v1/completions`

Minimal supported request:

```go
type CompletionRequest struct {
    Model       string          `json:"model"`
    Prompt      json.RawMessage `json:"prompt"`
    MaxTokens   *int            `json:"max_tokens,omitempty"`
    Temperature *float64        `json:"temperature,omitempty"`
    TopP        *float64        `json:"top_p,omitempty"`
    Stop        any             `json:"stop,omitempty"`
    Stream      bool            `json:"stream,omitempty"`
}
```

Phase 1 support:

- `model`: required; parsed as profile slug.
- `prompt`: required; must be a string.
- `stream`: optional.
- `max_tokens`, `temperature`, `top_p`, `stop`: accepted but may initially be ignored in favor of profile settings. Phase 2 can map them to Geppetto per-turn inference overrides.

Phase 1 rejects or ignores:

- prompt arrays,
- `n > 1`,
- `best_of`,
- `logprobs`,
- `echo`,
- `suffix`,
- `presence_penalty`, `frequency_penalty`,
- `user`.

Prefer rejecting unsupported fields only when they would create misleading behavior. It is acceptable to ignore unknown fields for prototype clients that send harmless defaults.

### Non-streaming request example

```json
{
  "model": "sonnet",
  "prompt": "Write one sentence about event sinks.",
  "max_tokens": 100,
  "stream": false
}
```

### Non-streaming response shape

```json
{
  "id": "cmpl_proxy_01J...",
  "object": "text_completion",
  "created": 1780620000,
  "model": "sonnet",
  "choices": [
    {
      "text": "Event sinks let Geppetto stream inference progress without coupling provider engines to a specific UI.",
      "index": 0,
      "logprobs": null,
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 16,
    "total_tokens": 26
  }
}
```

### Streaming response shape

OpenAI Completions streaming uses `text_completion` chunks. Each chunk carries incremental text in `choices[0].text`.

```text
HTTP/1.1 200 OK
Content-Type: text/event-stream
Cache-Control: no-cache

data: {"id":"cmpl_proxy_01J...","object":"text_completion","created":1780620000,"model":"sonnet","choices":[{"text":"Event","index":0,"logprobs":null,"finish_reason":null}]}

data: {"id":"cmpl_proxy_01J...","object":"text_completion","created":1780620000,"model":"sonnet","choices":[{"text":" sinks","index":0,"logprobs":null,"finish_reason":null}]}

data: {"id":"cmpl_proxy_01J...","object":"text_completion","created":1780620000,"model":"sonnet","choices":[{"text":"","index":0,"logprobs":null,"finish_reason":"stop"}]}

data: [DONE]
```

### `GET /v1/models`

Optional but useful for smoke tests. Return profile slugs as model IDs:

```json
{
  "object": "list",
  "data": [
    {"id": "sonnet", "object": "model", "owned_by": "geppetto-profile"},
    {"id": "gpt5", "object": "model", "owned_by": "geppetto-profile"}
  ]
}
```

## Request Mapping: Completions Prompt → Geppetto Turn

### Phase 1 mapping

| OpenAI Completions request | Geppetto turn/block |
|---|---|
| `prompt: "text"` | `turns.NewUserTextBlock(text)` |
| `model: "slug"` | profile slug; not stored as provider model |
| `max_tokens` | defer or map to per-turn inference config in Phase 2 |
| `temperature` | defer or map to per-turn inference config in Phase 2 |
| `top_p` | defer or map to per-turn inference config in Phase 2 |
| `stop` | defer or map to per-turn inference config in Phase 2 |

Pseudocode:

```go
func (m *Mapper) RequestToTurn(req *CompletionRequest) (*turns.Turn, error) {
    prompt, err := req.PromptString()
    if err != nil { return nil, err }
    if strings.TrimSpace(prompt) == "" { return nil, badRequest("prompt is required") }

    t := &turns.Turn{ID: newID("turn")}
    turns.AppendBlock(t, turns.NewUserTextBlock(prompt))

    // Phase 2 only: apply max_tokens/temperature/top_p/stop to t.Data
    // using Geppetto's existing per-turn inference config keys.

    return t, nil
}
```

### Prompt arrays

The OpenAI Completions API supports prompt arrays, but Phase 1 should not. Supporting arrays correctly means either running multiple independent Geppetto inferences or deciding how to batch multiple prompts in a single turn. That is not needed for the first bridge.

Recommended Phase 1 behavior:

```json
{
  "error": {
    "message": "prompt arrays are not supported in this prototype",
    "type": "invalid_request_error",
    "param": "prompt",
    "code": "unsupported_prompt_shape"
  }
}
```

## Final Mapping: Geppetto Turn → OpenAI Completion

The handler should record `preBlockCount := len(turn.Blocks)` before inference. After `RunInferenceWithResult`, generated blocks are `out.Blocks[preBlockCount:]`.

Text extraction rule:

1. Iterate generated blocks.
2. Include `BlockKindLLMText` blocks with assistant role.
3. Read `PayloadKeyText` from each payload.
4. Concatenate text in order.
5. Ignore reasoning/tool/internal blocks in Phase 1.

Finish reason mapping:

| Geppetto inference result | OpenAI Completions `finish_reason` |
|---|---|
| completed / unknown | `stop` |
| max tokens / truncated | `length` |
| interrupted | `stop` |
| tool calls pending | `stop` for Phase 1; do not expose tools through Completions |
| error | return HTTP error before response, or stream failure if already streaming |

Usage mapping:

| Geppetto result usage | OpenAI usage |
|---|---|
| `InputTokens` | `prompt_tokens` |
| `OutputTokens` | `completion_tokens` |
| sum | `total_tokens` |

Pseudocode:

```go
func (m *Mapper) TurnToCompletion(req *CompletionRequest, out *turns.Turn, result *engine.InferenceResult, preBlockCount int) (*CompletionResponse, error) {
    text := generatedAssistantText(out, preBlockCount)

    return &CompletionResponse{
        ID:      newID("cmpl"),
        Object:  "text_completion",
        Created: time.Now().Unix(),
        Model:   req.Model, // client-visible profile slug
        Choices: []CompletionChoice{{
            Text:         text,
            Index:        0,
            Logprobs:     nil,
            FinishReason: finishReason(result),
        }},
        Usage: usageFromResult(result),
    }, nil
}
```

## Streaming Mapping: Geppetto Events → OpenAI Completions SSE

For `stream: true`, attach an event sink and run inference in a goroutine. The SSE writer remains in the handler goroutine.

### Streaming control flow

```go
func handleCompletionStream(w http.ResponseWriter, r *http.Request, req *CompletionRequest) {
    frames := make(chan CompletionSSEFrame, 64)
    sink := &CompletionEventSink{translator: translator, out: frames}
    runCtx := events.WithEventSinks(r.Context(), sink)

    go func() {
        defer close(frames)
        out, result, err := engine.RunInferenceWithResult(runCtx, eng, turn)
        if err != nil {
            frames <- ErrorFrame(err)
            return
        }
        frames <- FinalCompletionFrame(req.Model, result)
        _ = out // final response body is not sent in Completions streaming, but can be logged/debugged
    }()

    writeCompletionSSE(w, r, frames)
}
```

### Event mapping

| Geppetto event | OpenAI Completions SSE chunk |
|---|---|
| `EventTextDelta{Delta:"x"}` | `choices[0].text = "x"`, `finish_reason = null` |
| `EventTextSegmentFinished` | optional final empty chunk with finish reason if known |
| `EventProviderCallFinished` | record finish/usage for final chunk |
| `EventReasoningDelta` | ignore in Phase 1 |
| tool events | ignore in Phase 1 |
| error event | error frame if possible, then `[DONE]` |

Frame payload:

```go
type CompletionStreamChunk struct {
    ID      string                 `json:"id"`
    Object  string                 `json:"object"` // "text_completion"
    Created int64                  `json:"created"`
    Model   string                 `json:"model"`
    Choices []CompletionChunkChoice `json:"choices"`
}

type CompletionChunkChoice struct {
    Text         string `json:"text"`
    Index        int    `json:"index"`
    Logprobs     any    `json:"logprobs"`
    FinishReason *string `json:"finish_reason"`
}
```

Always finish a successful stream with:

```text
data: [DONE]
```

## Handler Flow

### Non-streaming handler

```go
func handleCompletions(w http.ResponseWriter, r *http.Request) {
    req, err := decodeCompletionRequest(r.Body)
    if err != nil { writeOpenAIError(w, 400, err); return }
    if req.Model == "" { writeOpenAIErrorParam(w, "model", "model is required"); return }

    profile, err := profiles.ResolveProfile(r.Context(), req.Model)
    if err != nil { writeOpenAIError(w, 404, "unknown model/profile"); return }

    eng, err := engines.EngineForProfile(r.Context(), profile)
    if err != nil { writeOpenAIError(w, 500, "create engine"); return }

    turn, err := mapper.RequestToTurn(req)
    if err != nil { writeOpenAIError(w, 400, err); return }
    preBlockCount := len(turn.Blocks)

    if req.Stream {
        handleCompletionStream(w, r, req, eng, turn, preBlockCount)
        return
    }

    out, result, err := engine.RunInferenceWithResult(r.Context(), eng, turn)
    if err != nil { writeOpenAIError(w, 502, err); return }

    resp, err := mapper.TurnToCompletion(req, out, result, preBlockCount)
    if err != nil { writeOpenAIError(w, 500, err); return }

    writeJSON(w, 200, resp)
}
```

## Package Layout

Keep the package layout smaller than both previous designs.

```text
llm-proxy/
  cmd/
    llm-proxy-server/
      main.go
  pkg/
    server/
      server.go              # mux, health, completions handler
      completions_handler.go # POST /v1/completions
      sse.go                 # simple SSE writer
      errors.go              # OpenAI-style error response
    profiles/
      resolver.go            # ProfileResolver over geppetto engineprofiles.Registry
    runtime/
      engine_provider.go     # EngineProvider over geppetto factory
    openaicompletions/
      types.go               # minimal Completions request/response/chunk structs
      mapper.go              # Completion request <-> turns.Turn and final response
      event_translator.go    # events.Event -> CompletionSSEFrame
```

Do not add `adapters/openai`, `adapters/anthropic`, or route packages in this prototype.

## Implementation Plan

### Phase 1: Skeleton Completions endpoint

Files:

- `llm-proxy/cmd/llm-proxy-server/main.go`
- `llm-proxy/pkg/server/server.go`
- `llm-proxy/pkg/server/completions_handler.go`
- `llm-proxy/pkg/server/errors.go`
- `llm-proxy/pkg/openaicompletions/types.go`

Work:

1. Replace template command with `llm-proxy-server`.
2. Add flags: `--listen`, `--profiles`.
3. Implement `GET /healthz`.
4. Implement `POST /v1/completions` with JSON decode and validation.
5. Return a placeholder `text_completion` response before wiring Geppetto.
6. Add request body limit.

Validation:

```bash
cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
go test ./...
go run ./cmd/llm-proxy-server --profiles ./examples/profiles.yaml
curl -sS http://127.0.0.1:8080/healthz
```

### Phase 2: Profile resolver and engine construction

Files:

- `llm-proxy/pkg/profiles/resolver.go`
- `llm-proxy/pkg/runtime/engine_provider.go`
- `llm-proxy/examples/profiles.yaml`

Work:

1. Load Geppetto profile YAML/source chain.
2. Resolve request `model` as `EngineProfileSlug`.
3. Create engine via `factory.NewStandardEngineFactory().CreateEngine(settings)`.
4. Add optional `GET /v1/models` to list profile slugs.

Validation:

- Unknown `model` returns OpenAI-style 404.
- Known profile creates an engine.
- `GET /v1/models` lists profile slugs.

### Phase 3: Non-streaming Geppetto bridge

Files:

- `llm-proxy/pkg/openaicompletions/mapper.go`
- `llm-proxy/pkg/server/completions_handler.go`

Work:

1. Map prompt string to `turns.NewUserTextBlock`.
2. Run `engine.RunInferenceWithResult`.
3. Map generated assistant text blocks to `choices[0].text`.
4. Map usage and finish reason where available.
5. Add fake-engine tests.

Validation:

```bash
curl -sS http://127.0.0.1:8080/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"test-profile","prompt":"Say hello","stream":false}' | jq .
```

Expected shape:

- `object == "text_completion"`
- `model == "test-profile"`
- `choices[0].text` contains generated text.

### Phase 4: Streaming Completions bridge

Files:

- `llm-proxy/pkg/openaicompletions/event_translator.go`
- `llm-proxy/pkg/server/sse.go`

Work:

1. Implement channel-backed `events.EventSink`.
2. Attach sink through `events.WithEventSinks`.
3. Run inference in a goroutine.
4. Translate `EventTextDelta` into `text_completion` stream chunks.
5. Emit final empty finish-reason chunk and `[DONE]`.
6. Add tests with a fake engine publishing text deltas.

Validation:

```bash
curl -N http://127.0.0.1:8080/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"test-profile","prompt":"Say hello","stream":true}'
```

### Phase 5: Small increments only after the bridge works

- Map `max_tokens`, `temperature`, `top_p`, and `stop` to Geppetto per-turn inference config.
- Add prompt array support by running one engine call per prompt, if needed.
- Add `echo` only if a real client requires it.
- Add static bearer auth only if deployment requires it.
- Start `/v1/responses` from design doc 02 after Completions is stable.

## Testing Strategy

### Unit tests

- Decode minimal Completions requests.
- Reject missing model.
- Reject missing prompt.
- Reject prompt arrays in Phase 1.
- Map prompt string to `Turn`.
- Map generated `Turn` text to `CompletionResponse`.
- Resolve known/missing profile slug.
- Translate `EventTextDelta` to stream chunk.

### Fake engine tests

Build a fake engine that appends assistant text:

```go
type fakeEngine struct{}

func (e fakeEngine) RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error) {
    turns.AppendBlock(t, turns.NewAssistantTextBlock("hello"))
    return t, nil
}
```

For streaming, publish text events:

```go
type streamingFakeEngine struct{}

func (e streamingFakeEngine) RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error) {
    meta := events.EventMetadata{TurnID: t.ID}
    corr := events.Correlation{}
    events.PublishEventToContext(ctx, events.NewTextSegmentStartedEvent(meta, corr, "assistant"))
    events.PublishEventToContext(ctx, events.NewTextDeltaEvent(meta, corr, "hel", "hel", 1))
    events.PublishEventToContext(ctx, events.NewTextDeltaEvent(meta, corr, "lo", "hello", 2))
    events.PublishEventToContext(ctx, events.NewTextSegmentFinishedEvent(meta, corr, "hello", "stop"))
    turns.AppendBlock(t, turns.NewAssistantTextBlock("hello"))
    return t, nil
}
```

These tests validate the bridge without provider credentials.

## Design Decisions

### Decision: Expose OpenAI Completions first, defer Responses

- **Context:** The user clarified that the exposed API should be OpenAI Completions first and OpenAI Responses later.
- **Options considered:** `/v1/responses`; `/v1/chat/completions`; `/v1/completions`.
- **Decision:** Implement `POST /v1/completions` for the first prototype.
- **Rationale:** Completions has the simplest prompt-to-text shape and is enough to prove Geppetto inference behind an OpenAI-compatible endpoint.
- **Consequences:** The Responses-focused design remains preserved as a later-phase document, but implementation starts here.
- **Status:** accepted for prototype

### Decision: Use Geppetto engine execution, not direct provider adapters

- **Context:** Provider setup and provider-specific execution already live in Geppetto.
- **Options considered:** Direct provider adapters; Geppetto `engine.Engine`; full tool-loop runner.
- **Decision:** Use Geppetto `engine.Engine` through `engine.RunInferenceWithResult`.
- **Rationale:** This minimizes proxy code and keeps provider behavior in Geppetto.
- **Consequences:** The proxy maps Geppetto turn/events, not upstream provider wire details.
- **Status:** accepted for prototype

### Decision: Request `model` is the profile slug

- **Context:** The user requested no config and target profiles chosen by profile slug as engine name.
- **Options considered:** Route table; alias map; direct profile slug.
- **Decision:** Interpret `request.model` as `engineprofiles.EngineProfileSlug`.
- **Rationale:** This eliminates route config and makes the prototype obvious.
- **Consequences:** Public model names equal profile slugs until we add aliasing behind `ProfileResolver` later.
- **Status:** accepted for prototype

### Decision: Prompt string only in Phase 1

- **Context:** OpenAI Completions supports prompt arrays, but batching complicates the prototype.
- **Options considered:** Support all prompt shapes; support string only; flatten arrays into one prompt.
- **Decision:** Support prompt string only in Phase 1.
- **Rationale:** The core bridge is prompt text to Geppetto turn to generated text. Arrays can be added later by running multiple independent inferences.
- **Consequences:** Some OpenAI clients that send prompt arrays need a later increment.
- **Status:** accepted for prototype

## Open Questions

1. Which exact Geppetto profile loader should implementation call first: low-level YAML store or source-chain helper?
2. Does current profile YAML support environment expansion in API key fields, or should the prototype rely on Geppetto base settings/env merging?
3. Should `GET /v1/models` list only the default registry or all loaded registries?
4. Should unsupported Completions fields be rejected strictly or ignored leniently for client compatibility?
5. Should request-level `max_tokens` map to Geppetto per-turn inference config in Phase 1 or Phase 2?

## References

- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/registry.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/source_chain.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/cli/bootstrap/engine_settings.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/engine/engine.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/engine/run_with_result.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/engine/factory/factory.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/context.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/canonical_events.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/turns/types.go`
- `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/turns/helpers_blocks.go`
- Responses-later design: `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md`
- OpenAI Completions API: <https://platform.openai.com/docs/api-reference/completions/create>
