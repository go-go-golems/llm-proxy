---
Title: Simple Geppetto Engine OpenAI Responses Proxy Prototype
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
        Profile resolver interface used to treat model as profile slug
    - Path: geppetto/pkg/engineprofiles/source_chain.go
      Note: |-
        Existing YAML/SQLite profile source chain; prototype should load Geppetto profile YAML through this seam.
        Profile YAML/source loading seam
    - Path: geppetto/pkg/events/canonical_events.go
      Note: |-
        Canonical streaming event types to map to OpenAI Responses SSE events.
        Canonical events to translate into OpenAI Responses SSE
    - Path: geppetto/pkg/events/context.go
      Note: |-
        Context event-sink attachment used to stream Geppetto events during RunInference.
        Context-attached event sink mechanism for streaming
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
        Helper that runs an engine and returns canonical inference metadata for final OpenAI Responses output.
        RunInferenceWithResult helper for canonical final metadata
    - Path: geppetto/pkg/turns/helpers_blocks.go
      Note: |-
        Convenience constructors for user/system/assistant/tool blocks.
        Convenience constructors for mapped input/output blocks
    - Path: geppetto/pkg/turns/types.go
      Note: |-
        Turn and block data model used as the bridge between OpenAI Responses input and Geppetto engines.
        Turn/block bridge data model
    - Path: llm-proxy/cmd/XXX/main.go
      Note: Template command entrypoint to replace with cmd/llm-proxy-server.
    - Path: llm-proxy/go.mod
      Note: |-
        Current proxy module that should host the simple prototype server.
        Proxy module that should host the simple prototype
ExternalSources:
    - https://platform.openai.com/docs/api-reference/responses/create
Summary: Simplified prototype design for an OpenAI Responses-compatible endpoint that selects Geppetto profiles by model/profile slug, runs Geppetto inference engines directly, and maps Geppetto turns/events back to OpenAI Responses JSON/SSE.
LastUpdated: 2026-06-05T00:20:00-04:00
WhatFor: Use this as the revised prototype implementation guide; it supersedes the earlier direct-provider-adapter design for the first prototype.
WhenToUse: Read before implementing the first llm-proxy prototype that focuses on Geppetto engine execution and OpenAI Responses compatibility.
---


# Simple Geppetto Engine OpenAI Responses Proxy Prototype

## Executive Summary

The first prototype should be much simpler than the earlier route-and-adapter design. It should expose a small OpenAI Responses-compatible HTTP surface, use the incoming `model` string as a **Geppetto engine profile slug**, resolve that profile from a Geppetto profile YAML registry, construct a Geppetto inference engine with the existing factory, run `RunInference`, and translate the final `Turn` plus streamed Geppetto events back into OpenAI Responses JSON/SSE.

There is no model route table, no provider-specific proxy config, and no hand-written provider adapters in the prototype. Provider setup belongs entirely in Geppetto profile YAML. The proxy only needs a tiny layer that understands:

- how to load a profile registry,
- how to resolve `request.model` as a profile slug,
- how to build an engine from resolved Geppetto settings,
- how to map OpenAI Responses input into a Geppetto `Turn`, and
- how to map Geppetto output/events into OpenAI Responses format.

This document supersedes the earlier direct-provider-adapter design for the first prototype. The earlier design can remain as long-term reference, but the implementation should start here.

## Problem Statement

We need a fast prototype that proves the core bridge:

```text
OpenAI Responses HTTP request
        ↓
model == Geppetto profile slug
        ↓
Geppetto profile YAML resolves provider/model/keys/base URL/settings
        ↓
Geppetto engine factory creates the provider engine
        ↓
engine.RunInference(ctx, turn)
        ↓
OpenAI Responses JSON or SSE output
```

The proxy is not trying to become a full provider router yet. It is trying to make existing OpenAI Responses clients usable against Geppetto-managed models. Geppetto remains responsible for provider-specific inference behavior for Claude, OpenAI Responses, OpenAI Chat, and later providers. The proxy is responsible only for protocol shape at the HTTP boundary.

## Strict Prototype Scope

### In scope

- `POST /v1/responses`.
- Optional `GET /healthz`.
- Optional `GET /v1/models` listing profile slugs from the loaded registry.
- Load Geppetto profile registry source(s), initially from YAML.
- Treat `request.model` as `engineprofiles.EngineProfileSlug`.
- Resolve the profile using Geppetto's profile registry interface.
- Construct a Geppetto engine from resolved inference settings using the standard engine factory.
- Convert a simple subset of OpenAI Responses input into Geppetto `turns.Turn` blocks.
- Run inference through Geppetto's engine interface.
- Convert final `turns.Turn` output into an OpenAI Responses `response` object.
- For `stream: true`, attach an `events.EventSink` to the context and convert Geppetto events into OpenAI Responses SSE events.
- Keep a few small interfaces so later profile/provider/model loading can evolve.

### Out of scope

- No route config.
- No public-model-to-provider-model mapping table.
- No direct OpenAI/Claude provider HTTP adapters in llm-proxy.
- No separate backend protocol enum.
- No per-route credential config.
- No user accounts, refreshable proxy keys, or permissions in this prototype.
- No full OpenAI Responses schema parity.
- No Assistants API.
- No Chat Completions endpoint unless a concrete client requires it after the Responses prototype works.

## Current Code Evidence

### Geppetto profiles already provide the model/provider setup seam

Geppetto exposes a profile registry abstraction in `geppetto/pkg/engineprofiles/registry.go`. `ResolveInput` accepts a `RegistrySlug` and `EngineProfileSlug`, and `ResolvedEngineProfile` returns `InferenceSettings`, lineage, and metadata. The `Registry` interface exposes `ResolveEngineProfile(ctx, in)` as the stable seam.

This is the right interface for the proxy. The prototype can load a registry and resolve `request.model` as `EngineProfileSlug`. Later, another implementation can resolve profiles from a DB, user-specific registry, or dynamic provider catalog without changing the HTTP handler.

### Geppetto already knows how to build engines from resolved settings

`geppetto/pkg/cli/bootstrap/engine_settings.go` shows the existing pattern:

1. Resolve profile runtime.
2. Resolve engine profile.
3. Merge base inference settings with profile inference settings.
4. Create an engine through `factory.NewStandardEngineFactory().CreateEngine(settings)`.

The prototype can use the same conceptual flow, but with less CLI machinery. For each request, resolve the profile slug and create an engine from the resolved settings. If construction becomes expensive, add a small cache later.

### Geppetto engine is the execution boundary

`geppetto/pkg/inference/engine/engine.go` defines:

```go
type Engine interface {
    RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error)
}
```

The prototype should use this directly. The earlier design rejected `engine.Engine` because it wanted exact provider wire fidelity. The revised prototype explicitly does **not** need provider wire fidelity. It needs Geppetto inference fidelity and OpenAI Responses boundary compatibility.

### Geppetto can return canonical inference metadata

`geppetto/pkg/inference/engine/run_with_result.go` provides `RunInferenceWithResult(ctx, eng, turn)`, which prefers engines implementing `RunInferenceWithResult`, otherwise calls `RunInference` and extracts or synthesizes canonical metadata. This gives the proxy a consistent way to fill response status, finish reason, and usage when available.

The proxy should call `engine.RunInferenceWithResult` instead of calling `eng.RunInference` directly.

### Geppetto streams through event sinks attached to context

`geppetto/pkg/events/context.go` provides `events.WithEventSinks(ctx, sinks...)`, `events.GetEventSinks`, and `events.PublishEventToContext`. Provider engines publish streaming deltas and lifecycle events to sinks in the context.

`geppetto/pkg/events/canonical_events.go` defines canonical events such as:

- `EventProviderCallStarted`
- `EventProviderCallFinished`
- `EventTextSegmentStarted`
- `EventTextDelta`
- `EventTextSegmentFinished`
- `EventReasoningSegmentStarted`
- `EventReasoningDelta`
- `EventReasoningSegmentFinished`
- tool-call events

The streaming prototype should implement an `events.EventSink` that receives these events, converts them into OpenAI Responses events, and sends serialized SSE frames to the HTTP writer through a channel.

### Geppetto turns are the bridge data model

`geppetto/pkg/turns/types.go` defines `Turn` as an ordered list of `Block` values plus metadata and data. `geppetto/pkg/turns/helpers_blocks.go` includes constructors for user text, user multimodal, assistant text, system text, tool call, and tool result blocks.

The proxy's request mapper should be intentionally small: map only the OpenAI Responses input forms needed for the first prototype into these block types.

## Proposed Prototype Architecture

```text
┌────────────────────────────┐
│ OpenAI Responses client    │
│ POST /v1/responses         │
│ { model: "sonnet", ... }  │
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
│ OpenAI Responses → Turn    │
│ input/message items        │
│ become turns.Blocks        │
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
│ Turn/events → Responses    │
│ response JSON or SSE       │
└────────────────────────────┘
```

## Minimal Configuration Story

The prototype has no route config. It needs only enough startup input to find Geppetto profile YAML.

Recommended command shape:

```bash
llm-proxy-server \
  --listen 127.0.0.1:8080 \
  --profiles ./profiles.yaml
```

Alternative if we want to reuse Geppetto source-chain syntax immediately:

```bash
llm-proxy-server \
  --listen 127.0.0.1:8080 \
  --profile-sources yaml:./profiles.yaml
```

The important rule is:

```text
OpenAI Responses request field `model` == Geppetto engine profile slug.
```

Example request:

```json
{
  "model": "sonnet",
  "input": "Write a short haiku about logs.",
  "stream": false
}
```

The server resolves profile slug `sonnet` from the loaded Geppetto profile registry. The profile determines provider type, provider model name, base URL, API key names/env behavior, sampling defaults, response token limits, and provider-specific settings.

## Profile YAML Example

This is illustrative. Use the exact Geppetto profile YAML schema supported by the current `engineprofiles` package.

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
    display_name: OpenAI Responses through Geppetto
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

If environment interpolation is not supported by the current profile loader, the first prototype should use the existing Geppetto-supported credential mechanism rather than inventing proxy-specific credential config.

## Tiny Interfaces to Keep

These interfaces are intentionally small. They are seams, not a framework.

### Profile resolver

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

Implementation v1:

- Wrap `engineprofiles.Registry`.
- Parse slug with `engineprofiles.ParseEngineProfileSlug`.
- Call `Registry.ResolveEngineProfile(ctx, engineprofiles.ResolveInput{EngineProfileSlug: slug})`.
- Return `resolved.InferenceSettings`.

Future replacement:

- DB-backed per-user profile resolver.
- Resolver that overlays request/user settings.
- Resolver that restricts visible profiles by caller.

### Engine provider

```go
type EngineProvider interface {
    EngineForProfile(ctx context.Context, profile *ResolvedProfileRuntime) (engine.Engine, error)
}
```

Implementation v1:

- Use `factory.NewStandardEngineFactory().CreateEngine(profile.Settings)`.
- Do not cache initially unless engine construction is measurably expensive.

Future replacement:

- Cache engines by profile fingerprint.
- Inject custom factories for tests.
- Add middleware/tool-loop runner if needed.

### Responses mapper

```go
type ResponsesMapper interface {
    RequestToTurn(req *ResponsesRequest) (*turns.Turn, error)
    TurnToResponse(req *ResponsesRequest, out *turns.Turn, result *engine.InferenceResult) (*ResponsesResponse, error)
}
```

Implementation v1:

- Keep a small local `openairesponses` wire package.
- Support only common text/message input first.
- Add tool/image mappings after the basic bridge works.

### Event translator

```go
type EventTranslator interface {
    TranslateEvent(ev events.Event) ([]ResponsesSSEEvent, error)
    FinalEvents(result *engine.InferenceResult) []ResponsesSSEEvent
}
```

Implementation v1:

- Convert canonical text/reasoning/tool events to Responses-like SSE events.
- Unknown events can be ignored or emitted as `response.proxy.event` only behind a debug flag. For strict prototype simplicity, ignore unknown non-error events.

## HTTP API

### `POST /v1/responses`

Supported request subset:

```go
type ResponsesRequest struct {
    Model             string          `json:"model"`
    Input             json.RawMessage `json:"input"`
    Instructions      string          `json:"instructions,omitempty"`
    Stream            bool            `json:"stream,omitempty"`
    MaxOutputTokens   *int            `json:"max_output_tokens,omitempty"`
    Temperature       *float64        `json:"temperature,omitempty"`
    TopP              *float64        `json:"top_p,omitempty"`
    Stop              []string        `json:"stop,omitempty"`
    PreviousResponseID string         `json:"previous_response_id,omitempty"`
}
```

The prototype does not need to forward every request field to Geppetto immediately. It should preserve unknown fields in the decoded struct for future work, but v1 mapping can be explicit and small.

### Basic request examples

String input:

```json
{
  "model": "sonnet",
  "input": "Explain event sinks in one paragraph.",
  "stream": false
}
```

Message-item input:

```json
{
  "model": "gpt5",
  "input": [
    {
      "type": "message",
      "role": "user",
      "content": [
        {"type": "input_text", "text": "Give me three test names."}
      ]
    }
  ],
  "stream": true
}
```

### `GET /v1/models`

Optional but useful. Return profile slugs as model IDs:

```json
{
  "object": "list",
  "data": [
    {"id": "sonnet", "object": "model", "owned_by": "geppetto-profile"},
    {"id": "gpt5", "object": "model", "owned_by": "geppetto-profile"}
  ]
}
```

## Request Mapping: OpenAI Responses → Geppetto Turn

### v1 input subset

Support these first:

1. `input` as a string.
2. `input` as an array of message items:
   - `type: "message"`
   - `role: "system" | "developer" | "user" | "assistant"`
   - `content` array with `input_text` / `output_text` text parts.
3. Optional `instructions` as a system block prepended before input items.

Defer these until the text bridge works:

- image input,
- function calls,
- function call outputs,
- previous response continuation,
- file search / web search tools,
- structured output schema.

### Mapping rules

| Responses input | Geppetto turn block |
|---|---|
| `instructions` | `turns.NewSystemTextBlock(instructions)` |
| string `input` | `turns.NewUserTextBlock(input)` |
| message role `system` | `turns.NewSystemTextBlock(text)` |
| message role `developer` | `turns.NewSystemTextBlock(text)` for prototype simplicity |
| message role `user` | `turns.NewUserTextBlock(text)` |
| message role `assistant` | `turns.NewAssistantTextBlock(text)` |

Pseudocode:

```go
func (m *Mapper) RequestToTurn(req *ResponsesRequest) (*turns.Turn, error) {
    t := &turns.Turn{ID: newID("turn")}

    if strings.TrimSpace(req.Instructions) != "" {
        turns.AppendBlock(t, turns.NewSystemTextBlock(req.Instructions))
    }

    switch input := decodeInput(req.Input).(type) {
    case string:
        turns.AppendBlock(t, turns.NewUserTextBlock(input))
    case []ResponsesInputItem:
        for _, item := range input {
            blocks, err := mapInputItem(item)
            if err != nil { return nil, err }
            for _, b := range blocks { turns.AppendBlock(t, b) }
        }
    default:
        return nil, badRequest("unsupported input shape")
    }

    applyRequestOverridesToTurnData(t, req)
    return t, nil
}
```

### Request-level inference overrides

The first prototype can ignore request-level `temperature`, `top_p`, `max_output_tokens`, and `stop` if profile settings are enough. A slightly better prototype maps them into Geppetto per-turn inference config if the existing `engine` keys make that straightforward.

Recommendation:

- Phase 1: profile settings only.
- Phase 2: map common request overrides to `turn.Data` using Geppetto's existing inference config keys.

Do not mutate profile settings per request.

## Final Response Mapping: Geppetto Turn → OpenAI Responses

After `RunInferenceWithResult` returns, inspect blocks appended by the engine.

Output selection rule:

- The input turn has `preBlockCount` blocks.
- Generated blocks are `out.Blocks[preBlockCount:]`.
- Convert generated assistant text, reasoning, and tool-call blocks into Responses `output` items.

Minimal response shape:

```json
{
  "id": "resp_proxy_...",
  "object": "response",
  "created_at": 1780620000,
  "status": "completed",
  "model": "sonnet",
  "output": [
    {
      "id": "msg_proxy_...",
      "type": "message",
      "status": "completed",
      "role": "assistant",
      "content": [
        {"type": "output_text", "text": "...", "annotations": []}
      ]
    }
  ],
  "usage": {
    "input_tokens": 10,
    "output_tokens": 20,
    "total_tokens": 30
  }
}
```

Mapping rules:

| Geppetto output | Responses output |
|---|---|
| `BlockKindLLMText` | `type: message`, role assistant, `content[].type = output_text` |
| `BlockKindReasoning` | `type: reasoning` item, or omit in phase 1 if no stable representation is needed |
| `BlockKindToolCall` | `type: function_call` output item |
| canonical result usage | `usage.input_tokens`, `usage.output_tokens`, `usage.total_tokens` |
| finish class completed | `status: completed` |
| finish class max tokens | `status: incomplete`, `incomplete_details.reason = max_output_tokens` |
| error | HTTP error before response, or `status: failed` if already streaming |

Pseudocode:

```go
func (m *Mapper) TurnToResponse(req *ResponsesRequest, out *turns.Turn, result *engine.InferenceResult) (*ResponsesResponse, error) {
    generated := out.Blocks[m.preBlockCount:]
    response := &ResponsesResponse{
        ID:        newID("resp"),
        Object:    "response",
        CreatedAt: time.Now().Unix(),
        Model:     req.Model, // profile slug, matching client-visible model
        Status:    statusFromResult(result),
        Output:    mapGeneratedBlocks(generated),
        Usage:     usageFromResult(result),
    }
    return response, nil
}
```

## Streaming Mapping: Geppetto Events → OpenAI Responses SSE

For `stream: true`, the handler should not wait for the engine to finish before writing. Instead:

1. Create a channel of `ResponsesSSEEvent`.
2. Create an `events.EventSink` that translates Geppetto events into channel messages.
3. Attach the sink with `events.WithEventSinks(ctx, sink)`.
4. Run `engine.RunInferenceWithResult` in a goroutine.
5. The HTTP handler reads channel messages and writes SSE frames.
6. When inference returns, emit final completion events and close the stream.

### Event sink shape

```go
type ResponsesEventSink struct {
    translator EventTranslator
    out        chan<- ResponsesSSEEvent
}

func (s *ResponsesEventSink) PublishEvent(ev events.Event) error {
    frames, err := s.translator.TranslateEvent(ev)
    if err != nil {
        s.out <- ResponsesSSEEvent{Event: "error", Data: errorPayload(err)}
        return nil // do not break Geppetto inference from sink translation failure
    }
    for _, f := range frames {
        s.out <- f
    }
    return nil
}
```

### Recommended event mapping

| Geppetto event | OpenAI Responses SSE event |
|---|---|
| provider call started | `response.created` or no-op if already sent |
| text segment started | `response.output_item.added` with message item |
| text delta | `response.output_text.delta` |
| text segment finished | `response.output_text.done` and maybe `response.output_item.done` |
| reasoning segment started | `response.output_item.added` with reasoning item |
| reasoning delta | `response.reasoning_text.delta` |
| reasoning segment finished | `response.reasoning_text.done` |
| tool call started | `response.output_item.added` with function_call item |
| tool call arguments delta | `response.function_call_arguments.delta` |
| tool call requested | `response.output_item.done` for function_call |
| provider call finished | store usage/finish metadata for final event |
| error | `response.failed` |

Initial and final events for every successful stream:

```text
event: response.created
data: {"type":"response.created","response":{"id":"resp_proxy_...","status":"in_progress","model":"sonnet"}}

event: response.completed
data: {"type":"response.completed","response":{"id":"resp_proxy_...","status":"completed", ...}}
```

For prototype simplicity, emit conservative events:

- Always emit `response.created` before running the engine.
- Emit text deltas as they arrive.
- Emit `response.completed` after `RunInferenceWithResult` returns successfully.
- Emit `response.failed` if the engine returns an error.

This is enough to prove Geppetto event streaming without implementing every Responses event immediately.

## Handler Flow

### Non-streaming

```go
func handleResponses(w http.ResponseWriter, r *http.Request) {
    req := decodeResponsesRequest(r.Body)
    if req.Model == "" { writeOpenAIError(w, 400, "model is required"); return }

    profile, err := profiles.ResolveProfile(r.Context(), req.Model)
    if err != nil { writeOpenAIError(w, 404, "unknown profile"); return }

    eng, err := engines.EngineForProfile(r.Context(), profile)
    if err != nil { writeOpenAIError(w, 500, "create engine"); return }

    turn, err := mapper.RequestToTurn(req)
    if err != nil { writeOpenAIError(w, 400, err.Error()); return }
    preBlockCount := len(turn.Blocks)

    out, result, err := engine.RunInferenceWithResult(r.Context(), eng, turn)
    if err != nil { writeOpenAIError(w, 502, err.Error()); return }

    resp, err := mapper.TurnToResponse(req, out, result, preBlockCount)
    if err != nil { writeOpenAIError(w, 500, err.Error()); return }

    writeJSON(w, 200, resp)
}
```

### Streaming

```go
func handleResponsesStream(w http.ResponseWriter, r *http.Request, req *ResponsesRequest) {
    frames := make(chan ResponsesSSEEvent, 64)
    sink := &ResponsesEventSink{translator: translator, out: frames}
    runCtx := events.WithEventSinks(r.Context(), sink)

    go func() {
        defer close(frames)
        frames <- ResponseCreated(req.Model)
        out, result, err := engine.RunInferenceWithResult(runCtx, eng, turn)
        if err != nil {
            frames <- ResponseFailed(err)
            return
        }
        frames <- ResponseCompleted(req, out, result)
    }()

    writeSSE(w, r, frames)
}
```

The SSE writer is the only goroutine that touches `http.ResponseWriter`.

## Package Layout

Keep the package layout smaller than the earlier design.

```text
llm-proxy/
  cmd/
    llm-proxy-server/
      main.go
  pkg/
    server/
      server.go              # mux, health, responses handler
      sse.go                 # simple SSE writer
      errors.go              # OpenAI-style error response
    profiles/
      resolver.go            # ProfileResolver over geppetto engineprofiles.Registry
    runtime/
      engine_provider.go     # EngineProvider over geppetto factory
    openairesponses/
      types.go               # minimal Responses request/response/event structs
      mapper.go              # Responses request <-> turns.Turn and final response
      event_translator.go    # events.Event -> ResponsesSSEEvent
```

Avoid adding `adapters/openai`, `adapters/anthropic`, or route packages in the prototype.

## Implementation Plan

### Phase 1: Skeleton endpoint

Files:

- `llm-proxy/cmd/llm-proxy-server/main.go`
- `llm-proxy/pkg/server/server.go`
- `llm-proxy/pkg/server/errors.go`
- `llm-proxy/pkg/openairesponses/types.go`

Work:

1. Replace template command with `llm-proxy-server`.
2. Add flags: `--listen`, `--profiles`.
3. Implement `GET /healthz`.
4. Implement `POST /v1/responses` with JSON decode and placeholder response.
5. Add request body limit.

Validation:

```bash
cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
go test ./...
go run ./cmd/llm-proxy-server --profiles ./examples/profiles.yaml
```

### Phase 2: Profile resolver and engine construction

Files:

- `llm-proxy/pkg/profiles/resolver.go`
- `llm-proxy/pkg/runtime/engine_provider.go`
- `llm-proxy/examples/profiles.yaml`

Work:

1. Load Geppetto profile YAML as an `engineprofiles.Registry`.
2. Resolve request `model` as `EngineProfileSlug`.
3. Create engine via `factory.NewStandardEngineFactory().CreateEngine(settings)`.
4. Add `GET /v1/models` to list profile slugs.

Validation:

- Unknown `model` returns 404-style OpenAI error.
- Known profile creates an engine.
- `GET /v1/models` lists profile slugs.

### Phase 3: Non-streaming Responses bridge

Files:

- `llm-proxy/pkg/openairesponses/mapper.go`
- `llm-proxy/pkg/server/responses_handler.go`

Work:

1. Map string input to `turns.NewUserTextBlock`.
2. Map message input with text parts to system/user/assistant blocks.
3. Run `engine.RunInferenceWithResult`.
4. Map generated assistant text blocks to Responses output message.
5. Map canonical usage and finish status.

Validation:

- Use a fake Geppetto engine in unit tests.
- Verify output JSON shape is OpenAI Responses-like.
- Verify model in output remains the profile slug requested by the client.

### Phase 4: Streaming Responses bridge

Files:

- `llm-proxy/pkg/openairesponses/event_translator.go`
- `llm-proxy/pkg/server/sse.go`

Work:

1. Implement channel-backed `events.EventSink`.
2. Attach sink through `events.WithEventSinks`.
3. Run inference in a goroutine.
4. Emit `response.created`, text deltas, `response.completed`, and `response.failed`.
5. Add tests with a fake engine that publishes `EventTextDelta` events.

Validation:

- Streaming request receives `response.created`.
- Text deltas become `response.output_text.delta` SSE frames.
- Successful completion emits `response.completed`.
- Engine error emits `response.failed`.

### Phase 5: Small compatibility increments

Only after the basic bridge works:

- Map `max_output_tokens`, `temperature`, `top_p`, and `stop` into Geppetto per-turn inference overrides if the existing keys make this clean.
- Add reasoning event mapping.
- Add tool-call event mapping.
- Add image input mapping.
- Add a small static bearer-key middleware if needed for deployment.

## Testing Strategy

### Unit tests

- Decode minimal Responses requests.
- Map string input to `Turn`.
- Map message-array input to `Turn`.
- Map generated `Turn` blocks to Responses output.
- Resolve known/missing profile slug.
- Fake engine creation.
- Event translator text delta mapping.

### Integration-style tests with fake engine

Do not call live providers in prototype tests. Build a fake `engine.Engine` that appends assistant blocks and optionally publishes events through `events.PublishEventToContext`.

```go
type fakeEngine struct{}

func (e fakeEngine) RunInference(ctx context.Context, t *turns.Turn) (*turns.Turn, error) {
    events.PublishEventToContext(ctx, events.NewTextSegmentStartedEvent(meta, corr, "assistant"))
    events.PublishEventToContext(ctx, events.NewTextDeltaEvent(meta, corr, "hello", "hello", 1))
    turns.AppendBlock(t, turns.NewAssistantTextBlock("hello"))
    return t, nil
}
```

This validates the proxy bridge without provider credentials.

## Design Decisions

### Decision: Use Geppetto engine execution, not direct provider adapters

- **Context:** The first prototype should prove Geppetto inference behind an OpenAI-compatible Responses boundary.
- **Options considered:** Direct provider adapters; Geppetto `engine.Engine`; tool-loop runner.
- **Decision:** Use Geppetto `engine.Engine` through `engine.RunInferenceWithResult`.
- **Rationale:** Provider setup and provider-specific behavior already belong to Geppetto. This minimizes proxy code and focuses the prototype on request/response/event translation.
- **Consequences:** The proxy will not preserve provider wire-level details. That is acceptable for the prototype because the client-facing contract is OpenAI Responses shape, not upstream-provider fidelity.
- **Status:** accepted for prototype

### Decision: Request `model` is the profile slug

- **Context:** The user requested no config and target profiles chosen by profile slug as engine name.
- **Options considered:** Route table; model alias map; profile slug directly.
- **Decision:** Interpret `request.model` as `engineprofiles.EngineProfileSlug`.
- **Rationale:** This eliminates route config and makes the prototype easy to understand.
- **Consequences:** Public model names equal profile slugs. If aliases are needed later, add them behind `ProfileResolver`, not in the handler.
- **Status:** accepted for prototype

### Decision: No proxy provider config

- **Context:** Provider setup should happen through Geppetto profile YAML.
- **Options considered:** Proxy YAML with providers; profile YAML only; environment variables only.
- **Decision:** Use Geppetto profile YAML as the only provider setup mechanism.
- **Rationale:** This keeps the proxy thin and tests the intended Geppetto profile workflow.
- **Consequences:** Any missing key/base URL/model setting is a profile problem, not a proxy route problem.
- **Status:** accepted for prototype

### Decision: Tiny interfaces only

- **Context:** We still want future expansion points without building the full future system now.
- **Options considered:** No interfaces; large plugin framework; small interfaces.
- **Decision:** Add only `ProfileResolver`, `EngineProvider`, `ResponsesMapper`, and `EventTranslator` seams.
- **Rationale:** These cover the likely future changes: DB-backed profile resolution, engine caching/factory changes, richer protocol mapping, and event compatibility.
- **Consequences:** The prototype stays simple but testable.
- **Status:** accepted for prototype

## Open Questions

1. Which exact Geppetto profile loader should the prototype call: low-level `engineprofiles.NewYAMLFileEngineProfileStore` or the existing source-chain helper?
2. Does the current Geppetto profile YAML loader support environment expansion in API keys? If not, should the prototype rely on Geppetto's existing env/default settings merge instead?
3. Should `GET /v1/models` list only the default registry or all loaded registries?
4. Should streaming emit strict OpenAI Responses event names only, or is a small `response.proxy.event` debug event acceptable during development?
5. Should Phase 1 ignore request-level sampling overrides completely, or map them immediately to per-turn inference config?

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
- OpenAI Responses API: <https://platform.openai.com/docs/api-reference/responses/create>
