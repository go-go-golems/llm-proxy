---
Title: Simple Geppetto Engine OpenAI Chat Completions Proxy Prototype
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
    - Path: geppetto/pkg/events/canonical_events.go
      Note: |-
        Text delta events used for chat streaming chunks.
        Text delta events used by chat streaming
    - Path: geppetto/pkg/turns/helpers_blocks.go
      Note: |-
        Geppetto constructors for system, user, and assistant text blocks.
        System/user/assistant block constructors used by chat mapper
    - Path: llm-proxy/examples/README.md
      Note: Chat Completions and tool usage examples
    - Path: llm-proxy/pkg/openaichat/mapper.go
      Note: Chat message/tool mapping to Geppetto turns and responses
    - Path: llm-proxy/pkg/openaichat/stream.go
      Note: Chat SSE frame constructors and Geppetto event sink
    - Path: llm-proxy/pkg/openaichat/types.go
      Note: Chat Completions wire types including tools and tool calls
    - Path: llm-proxy/pkg/openaicompletions/mapper.go
      Note: |-
        Existing prompt-to-turn and turn-to-completion mapping to mirror for chat.
        Existing mapper pattern mirrored for chat
    - Path: llm-proxy/pkg/openaicompletions/stream.go
      Note: |-
        Existing text-delta SSE translation pattern to mirror for chat chunks.
        Existing stream frame pattern mirrored for chat
    - Path: llm-proxy/pkg/runtime/chat_service.go
      Note: Geppetto-backed Chat Completions runtime service
    - Path: llm-proxy/pkg/runtime/completion_service.go
      Note: |-
        Existing Geppetto profile/engine/inference service flow to reuse for chat.
        Existing Geppetto runtime service pattern reused for chat
    - Path: llm-proxy/pkg/server/server.go
      Note: |-
        Existing HTTP server and /v1/completions handler to extend with /v1/chat/completions.
        HTTP routing and service interface extension point
        HTTP route and service interface for /v1/chat/completions
    - Path: llm-proxy/pkg/server/sse.go
      Note: Shared SSE writer for completions and chat streams
ExternalSources:
    - https://platform.openai.com/docs/api-reference/chat/create
Summary: Design for adding a simple OpenAI Chat Completions endpoint backed by Geppetto profile slug resolution and engine inference, reusing the existing /v1/completions architecture.
LastUpdated: 2026-06-05T01:25:00-04:00
WhatFor: 'Use this as the implementation guide for the next proxy endpoint: /v1/chat/completions.'
WhenToUse: Read before implementing or reviewing the Chat Completions bridge.
---



# Simple Geppetto Engine OpenAI Chat Completions Proxy Prototype

## Executive summary

The next endpoint should be `POST /v1/chat/completions`. It should follow the same architecture as the existing `/v1/completions` prototype: the request `model` is a Geppetto profile slug, provider setup remains in Geppetto profile YAML, the proxy creates a Geppetto engine from the resolved profile, and inference runs through `engine.RunInferenceWithResult`.

The difference is the boundary mapper. Completions maps one `prompt` string to one user block. Chat Completions maps an ordered `messages` array to an ordered Geppetto `Turn`. The response maps generated assistant text and tool-call blocks to an OpenAI `chat.completion` response. Streaming maps Geppetto `EventTextDelta` and tool-call events to `chat.completion.chunk` SSE frames.

The prototype should support the common text-chat and function-tool subset first:

- `role: system`, `developer`, `user`, `assistant`, and `tool`
- string `content` for text messages
- assistant `tool_calls` and tool-result messages
- OpenAI function `tools` mapped to Geppetto per-turn tool definitions
- optional `stream`
- optional token/sampling fields parsed but not applied yet

Image parts, structured output, full OpenAI field compatibility, and proxy-side tool execution are deferred. The goal is to complete a working chat bridge quickly without changing the profile/engine architecture.

## Problem statement

The existing prototype exposes legacy `/v1/completions`, but many OpenAI-compatible clients use `/v1/chat/completions`. Chat Completions is still much simpler than OpenAI Responses because its basic text path is an ordered list of role-tagged messages and one assistant message result. It can be added by reusing the current profile resolver, engine provider, event-sink streaming path, and server structure.

The implementation should not introduce direct provider adapters. Geppetto still owns provider setup and provider execution.

## Scope

### In scope

- `POST /v1/chat/completions`.
- Non-streaming `chat.completion` responses.
- Streaming `chat.completion.chunk` SSE responses.
- Text messages with string `content`.
- Roles: `system`, `developer`, `user`, `assistant`, and `tool`.
- Request `model` interpreted as Geppetto profile slug.
- OpenAI function `tools` mapped to Geppetto turn tool definitions and tool config.
- Assistant `tool_calls` and `role: tool` results mapped to Geppetto tool-call/tool-use blocks.
- Streaming text deltas and tool-call chunks.
- Reuse `GET /v1/models` profile slug listing.
- Fake-engine tests for mapping, non-streaming, streaming, and tool-call conversion.

### Out of scope for this increment

- Proxy-side execution of arbitrary client tools.
- Multimodal content arrays.
- `response_format`, JSON schema, structured output.
- Applying request-level inference overrides.
- Exact OpenAI compatibility for every optional field.
- `/v1/responses`.

## Architecture

```mermaid
flowchart TD
  Client[OpenAI chat client]
  Handler[pkg/server chat handler]
  ChatWire[pkg/openaichat]
  Profiles[pkg/profiles resolver]
  Runtime[pkg/runtime ChatCompletionService]
  Engine[Geppetto engine]
  Turn[Geppetto Turn]
  Response[chat.completion JSON]
  Stream[chat.completion.chunk SSE]

  Client -->|POST /v1/chat/completions| Handler
  Handler --> ChatWire
  Handler --> Runtime
  Runtime --> Profiles
  Profiles -->|model as profile slug| Runtime
  Runtime --> Engine
  ChatWire -->|messages to Turn| Turn
  Turn --> Engine
  Engine -->|Turn + InferenceResult| ChatWire
  ChatWire --> Response
  Engine -->|EventTextDelta| Stream
  Response --> Handler
  Stream --> Handler
  Handler --> Client
```

## Wire types

Add a new `pkg/openaichat` package rather than expanding `pkg/openaicompletions`. The two OpenAI endpoints share some concepts but their response shapes differ enough to keep the code separate.

Minimum request type:

```go
type ChatCompletionRequest struct {
    Model      string          `json:"model"`
    Messages   []ChatMessage   `json:"messages"`
    Tools      []ChatTool      `json:"tools,omitempty"`
    ToolChoice json.RawMessage `json:"tool_choice,omitempty"`
    MaxTokens  *int            `json:"max_tokens,omitempty"`
    Stream     bool            `json:"stream,omitempty"`
}

type ChatMessage struct {
    Role       string          `json:"role"`
    Content    json.RawMessage `json:"content,omitempty"`
    ToolCallID string          `json:"tool_call_id,omitempty"`
    ToolCalls  []ChatToolCall  `json:"tool_calls,omitempty"`
}
```

Phase 1 should accept only string content. If content is an array, return an OpenAI-style invalid request error saying multimodal content is not supported in this prototype.

Minimum non-streaming response:

```go
type ChatCompletionResponse struct {
    ID      string       `json:"id"`
    Object  string       `json:"object"` // "chat.completion"
    Created int64        `json:"created"`
    Model   string       `json:"model"`
    Choices []ChatChoice `json:"choices"`
    Usage   *Usage       `json:"usage,omitempty"`
}

type ChatChoice struct {
    Index        int         `json:"index"`
    Message      ChatMessageOut `json:"message"`
    FinishReason string      `json:"finish_reason,omitempty"`
}

type ChatMessageOut struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
```

Minimum stream chunk:

```go
type ChatCompletionChunk struct {
    ID      string              `json:"id"`
    Object  string              `json:"object"` // "chat.completion.chunk"
    Created int64               `json:"created"`
    Model   string              `json:"model"`
    Choices []ChatStreamChoice  `json:"choices"`
}

type ChatStreamChoice struct {
    Index        int        `json:"index"`
    Delta        ChatDelta  `json:"delta"`
    FinishReason *string    `json:"finish_reason"`
}

type ChatDelta struct {
    Role      string              `json:"role,omitempty"`
    Content   string              `json:"content,omitempty"`
    ToolCalls []ChatToolCallDelta `json:"tool_calls,omitempty"`
}
```

## Message-to-turn mapping

Mapping is direct for text and function-tool chat:

| Chat message role | Geppetto block/data |
|---|---|
| `system` | `turns.NewSystemTextBlock(content)` |
| `developer` | `turns.NewSystemTextBlock(content)` for prototype simplicity |
| `user` | `turns.NewUserTextBlock(content)` |
| `assistant` content | `turns.NewAssistantTextBlock(content)` |
| `assistant` tool calls | `turns.NewToolCallBlock(id, name, arguments)` |
| `tool` | `turns.NewToolUseBlock(tool_call_id, content)` |
| request `tools` | `engine.KeyToolDefinitions` and `engine.KeyToolConfig` on the turn |

The mapper should preserve ordering. It should reject empty `messages` and unsupported roles. It should reject non-string content until multimodal support is explicitly added.

## Turn-to-chat mapping

The service records `preBlockCount` before inference. Generated blocks are `out.Blocks[preBlockCount:]`. The mapper concatenates generated assistant text and maps generated tool-call blocks to assistant `tool_calls`.

```go
ChatCompletionResponse{
    Object: "chat.completion",
    Model: req.Model,
    Choices: []ChatChoice{{
        Index: 0,
        Message: ChatMessageOut{Role: "assistant", Content: generatedText},
        FinishReason: finishReason(result),
    }},
    Usage: usageFromResult(result),
}
```

This keeps the response model equal to the client-visible profile slug.

## Streaming mapping

Streaming should emit an initial role chunk, content delta chunks, a final finish chunk, and `[DONE]`.

```text
data: {"object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"},"finish_reason":null}]}

data: {"object":"chat.completion.chunk","choices":[{"delta":{"content":"hel"},"finish_reason":null}]}

data: {"object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]
```

Geppetto event mapping:

| Geppetto event | Chat chunk |
|---|---|
| stream start | initial assistant role chunk |
| `EventTextDelta` | `delta.content` chunk |
| `EventToolCallStarted` | `delta.tool_calls[].id`, `type`, and function name |
| `EventToolCallArgumentsDelta` | `delta.tool_calls[].function.arguments` |
| `EventToolCallRequested` | fallback full argument chunk when a provider emits only the requested event |
| inference success | final chunk with finish reason (`stop`, `length`, or `tool_calls`) |
| inference error | SSE error object, then `[DONE]` |

The handler goroutine remains the only goroutine writing to `http.ResponseWriter`.

## Package layout

```text
pkg/
  openaichat/
    types.go
    mapper.go
    stream.go
    *_test.go
  runtime/
    chat_service.go
    chat_service_test.go
  server/
    server.go        # add POST /v1/chat/completions
    sse.go           # add chat SSE writer or generic SSE helper
```

## Implementation phases

### Phase 1: Chat wire package and mapping

- Add `pkg/openaichat/types.go`.
- Add request decoding and validation.
- Add `RequestToTurn` and `TurnToChatCompletion` mapping.
- Add tool schema, assistant tool-call, and tool-result mapping.
- Add tests for roles, missing messages, unsupported roles, unsupported content, generated assistant text, and generated tool calls.

### Phase 2: Runtime service

- Add `pkg/runtime/chat_service.go`.
- Reuse `ProfileResolver` and `EngineProvider`.
- Run `engine.RunInferenceWithResult`.
- Add fake-engine tests for non-streaming chat.

### Phase 3: HTTP endpoint

- Add `POST /v1/chat/completions` to `pkg/server`.
- Add `ChatCompletionService` and `StreamingChatCompletionService` interfaces.
- Wire `cmd/llm-proxy-server` to create chat service when profiles are provided.
- Add handler tests.

### Phase 4: Streaming

- Add chat stream frames and `ChatEventSink`.
- Translate Geppetto text deltas to chat chunks.
- Add chat SSE writer or make existing SSE writer generic.
- Add fake streaming tests.

### Phase 5: Examples and diary

- Update `examples/README.md` with `/v1/chat/completions` examples.
- Run `go test ./... -count=1` and `GOWORK=off go test ./... -count=1`.
- Update diary and changelog.
- Commit phase-sized changes.

## Decisions

### Decision: Add Chat Completions as a sibling mapper, not a replacement

- **Context:** `/v1/completions` already works and should remain available.
- **Decision:** Add `pkg/openaichat` and keep `pkg/openaicompletions` intact.
- **Rationale:** The endpoints have different wire shapes and should be testable independently.
- **Status:** accepted.

### Decision: Support function tool shapes but not proxy-side execution

- **Context:** Chat clients commonly use `tools`, assistant `tool_calls`, and `role: tool` result messages.
- **Decision:** Map OpenAI function tool schemas to Geppetto turn tool definitions, map generated Geppetto tool-call blocks back to OpenAI `tool_calls`, and map client tool results to Geppetto tool-use blocks. Do not execute arbitrary client tools in the proxy.
- **Rationale:** This supports the standard client-driven tool loop while preserving the proxy boundary: Geppetto/provider inference can request tools, and the client remains responsible for executing them.
- **Status:** accepted.

### Decision: Stream only assistant text deltas

- **Context:** Chat Completions chunks can express visible assistant content, but not a clean separate thinking channel in the standard shape.
- **Decision:** Map `EventTextDelta` to `delta.content` and ignore reasoning/tool events for now.
- **Rationale:** This keeps standard OpenAI Chat chunk compatibility for visible text.
- **Status:** accepted.

## Open questions

1. Should the decoder allow unknown fields for client compatibility, as with Completions?
2. Should `developer` messages remain mapped to system blocks, or should Geppetto get a separate block kind later?
3. Should proxy-side tool execution ever be supported, or should the proxy always keep client-driven tool loops?
4. Should request overrides be implemented once and shared by Completions and Chat?

## References

- Current Completions implementation: `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaicompletions`
- Runtime service pattern: `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/runtime/completion_service.go`
- Server pattern: `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/server/server.go`
- Geppetto turns: `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/turns/helpers_blocks.go`
- Geppetto events: `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/canonical_events.go`
