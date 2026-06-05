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

- [ ] Add `cmd/llm-proxy-server` with `--listen` and `--profiles` flags.
- [ ] Add `pkg/server` with `GET /healthz`, request body limiting, JSON helpers, and OpenAI-style error responses.
- [ ] Add `pkg/openaicompletions` request/response/chunk structs for the supported `/v1/completions` subset.
- [ ] Implement request decoding for required `model` and string `prompt`, with explicit rejection of prompt arrays for the prototype.
- [ ] Add unit tests for completion request decoding and validation.

### Phase 2: Geppetto profile resolution and engine construction

- [ ] Add `pkg/profiles.ProfileResolver` that loads Geppetto profile YAML and resolves request `model` as `EngineProfileSlug`.
- [ ] Add optional `GET /v1/models` that lists profile slugs from the loaded registry.
- [ ] Add `pkg/runtime.EngineProvider` over Geppetto's standard engine factory.
- [ ] Wire the server handler to resolve the profile and create an engine before inference.
- [ ] Add tests for known/missing profile behavior using an in-memory or fake resolver.

### Phase 3: Non-streaming Geppetto inference bridge

- [ ] Map OpenAI Completions `prompt` string to a Geppetto `turns.Turn` with one user text block.
- [ ] Run `engine.RunInferenceWithResult` for non-streaming completion requests.
- [ ] Extract generated assistant text from blocks appended after the input block count.
- [ ] Map Geppetto inference metadata to OpenAI `usage` and `finish_reason` where available.
- [ ] Add fake-engine handler tests for successful non-streaming completions and engine errors.

### Phase 4: Streaming Completions bridge

- [ ] Add a channel-backed `events.EventSink` for a single request.
- [ ] Translate Geppetto `EventTextDelta` into OpenAI `text_completion` SSE chunks.
- [ ] Emit a final empty chunk with finish reason and `data: [DONE]` on success.
- [ ] Ensure the HTTP handler goroutine is the only goroutine writing to `http.ResponseWriter`.
- [ ] Add fake-engine streaming tests for text deltas, final chunks, and engine errors.

### Phase 5: Prototype polish and documentation

- [ ] Add `examples/profiles.yaml` and curl smoke examples for `/healthz`, `/v1/models`, and `/v1/completions`.
- [ ] Run `go test ./... -count=1` and document any failures.
- [ ] Update the diary after each implementation phase with commands, failures, and review instructions.
- [ ] Commit phase-sized changes with focused messages.

## Deferred tasks

- [ ] Add OpenAI Responses support from design doc 02 after the Completions prototype works.
- [ ] Add route aliases, auth, per-user keys, and direct provider adapters only after the simple Geppetto-engine prototype works and a concrete need appears.
