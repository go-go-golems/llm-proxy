# Tasks

## TODO

- [x] Create docmgr ticket workspace for OpenAI-compatible llm-proxy design.
- [x] Inspect preliminary May 2026 proxy design.
- [x] Inspect current `llm-proxy` module state.
- [x] Gather Geppetto evidence for profile stores, inference settings, provider protocols, and outbound URL safety.
- [x] Write intern-facing design and implementation guide.
- [x] Write investigation diary entries for setup, evidence gathering, and design authoring.
- [x] Relate key source files and docs through docmgr.
- [x] Validate ticket with `docmgr doctor`.
- [x] Upload design bundle to reMarkable and verify remote listing.
- [x] Add revised simple prototype design after scope correction.
- [x] Copy/adapt the simple design into a Completions-first prototype document while preserving the Responses design.

## Future implementation tasks (current Completions-first prototype)

### Phase 1: Server skeleton and OpenAI Completions wire types

- [x] Add `cmd/llm-proxy-server` with `--listen` and `--profiles` flags.
- [x] Add `pkg/server` with `GET /healthz`, request body limiting, JSON helpers, and OpenAI-style error responses.
- [x] Add `pkg/openaicompletions` request/response/chunk structs for the supported `/v1/completions` subset.
- [x] Implement request decoding for required `model` and string `prompt`, with explicit rejection of prompt arrays for the prototype.
- [x] Add unit tests for completion request decoding and validation.

### Phase 2: Geppetto profile resolution and engine construction

- [x] Add `pkg/profiles.ProfileResolver` that loads Geppetto profile YAML and resolves request `model` as `EngineProfileSlug`.
- [x] Add optional `GET /v1/models` that lists profile slugs from the loaded registry.
- [x] Add `pkg/runtime.EngineProvider` over Geppetto's standard engine factory.
- [x] Wire the server handler to resolve the profile and create an engine before inference.
- [x] Add tests for known/missing profile behavior using an in-memory or fake resolver.

### Phase 3: Non-streaming Geppetto inference bridge

- [x] Map OpenAI Completions `prompt` string to a Geppetto `turns.Turn` with one user text block.
- [x] Run `engine.RunInferenceWithResult` for non-streaming completion requests.
- [x] Extract generated assistant text from blocks appended after the input block count.
- [x] Map Geppetto inference metadata to OpenAI `usage` and `finish_reason` where available.
- [x] Add fake-engine handler tests for successful non-streaming completions and engine errors.

### Phase 4: Streaming Completions bridge

- [x] Add a channel-backed `events.EventSink` for a single request.
- [x] Translate Geppetto `EventTextDelta` into OpenAI `text_completion` SSE chunks.
- [x] Emit a final empty chunk with finish reason and `data: [DONE]` on success.
- [x] Ensure the HTTP handler goroutine is the only goroutine writing to `http.ResponseWriter`.
- [x] Add fake-engine streaming tests for text deltas, final chunks, and engine errors.

### Phase 5: Prototype polish and documentation

- [x] Add `examples/profiles.yaml` and curl smoke examples for `/healthz`, `/v1/models`, and `/v1/completions`.
- [x] Run `go test ./... -count=1` and document any failures.
- [x] Update the diary after each implementation phase with commands, failures, and review instructions.
- [x] Commit phase-sized changes with focused messages.

## Future implementation tasks (Chat Completions endpoint)

### Phase 6: Chat wire package and text-only turn mapping

- [x] Add `pkg/openaichat` request/response/chunk structs for `/v1/chat/completions`.
- [x] Decode and validate required `model` and non-empty `messages`.
- [x] Support text-only string content for `system`, `developer`, `user`, and `assistant` roles.
- [x] Map chat messages to Geppetto `turns.Turn` blocks in order.
- [x] Map generated assistant blocks to a `chat.completion` response.
- [x] Add mapper/unit tests for valid messages, missing messages, unsupported roles, unsupported content, and generated assistant text.
- [x] Map OpenAI function `tools` to Geppetto turn tool definitions and tool config.
- [x] Map assistant `tool_calls` and `role: tool` results to Geppetto tool-call/tool-use blocks.
- [x] Map generated Geppetto tool-call blocks back to OpenAI assistant `tool_calls`.

### Phase 7: Chat runtime service

- [x] Add `pkg/runtime.ChatCompletionService` using the existing profile resolver and engine provider seams.
- [x] Run `engine.RunInferenceWithResult` for non-streaming chat completions.
- [x] Map Geppetto usage and finish metadata to Chat Completions usage and finish reason.
- [x] Add fake-engine tests for successful chat completion and engine error propagation.

### Phase 8: Chat HTTP endpoint

- [x] Add `POST /v1/chat/completions` to `pkg/server`.
- [x] Add `ChatCompletionService` and optional streaming chat service interfaces in `pkg/server`.
- [x] Wire `cmd/llm-proxy-server` to create a chat service when `--profiles` is provided.
- [x] Add handler tests for non-streaming chat response and validation errors.

### Phase 9: Chat streaming bridge

- [x] Add chat stream frame constructors and a Geppetto `EventTextDelta` sink.
- [x] Emit initial assistant-role chunk, content delta chunks, final finish chunk, and `[DONE]`.
- [x] Add a chat SSE writer or genericize the existing SSE helper.
- [x] Add fake-engine and handler tests for streaming chat chunks.
- [x] Stream Geppetto tool-call events as OpenAI Chat Completions `delta.tool_calls` chunks.

### Phase 10: Chat examples and validation

- [x] Update `examples/README.md` with `/v1/chat/completions` non-streaming and streaming examples.
- [x] Run `go test ./... -count=1` and `GOWORK=off go test ./... -count=1`.
- [x] Update diary, changelog, and research logbook if relevant.
- [x] Commit phase-sized changes with focused messages.

## Deferred tasks

- [ ] Add OpenAI Responses support from design doc 02 after Completions and Chat Completions prototypes work.
- [ ] Add route aliases, auth, per-user keys, and direct provider adapters only after the simple Geppetto-engine prototype works and a concrete need appears.
