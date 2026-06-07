# Changelog

## 2026-06-04

- Initial workspace created


## 2026-06-04

Created evidence-backed OpenAI-compatible llm-proxy design guide and investigation diary

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/01-openai-compatible-llm-proxy-design-and-implementation-guide.md — Primary design deliverable
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Chronological investigation record


## 2026-06-04

Validated ticket with docmgr doctor and uploaded design bundle to reMarkable

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/01-openai-compatible-llm-proxy-design-and-implementation-guide.md — Uploaded design deliverable
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Updated with validation and upload details


## 2026-06-04

Added revised simple prototype design using Geppetto engines directly and model-as-profile-slug routing

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md — New prototype design superseding earlier direct-adapter design for v1


## 2026-06-04

Updated diary and tasks for revised simple Geppetto-engine prototype scope; doctor passes

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded scope correction and new design step
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/tasks.md — Replaced broad implementation tasks with simple prototype tasks


## 2026-06-04

Uploaded revised simple Geppetto-engine prototype design as a standalone reMarkable PDF

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md — Standalone reMarkable upload source
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded standalone upload details


## 2026-06-04

Copied/adapted the simple Geppetto-engine design for a Completions-first API; Responses design remains preserved for later

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md — Original Responses-focused design intentionally preserved
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/03-simple-geppetto-engine-openai-completions-proxy-prototype.md — Current first-prototype design


## 2026-06-04

Updated diary and tasks to make OpenAI Completions the current first prototype target

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded Completions-first correction
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/tasks.md — Current tasks now target /v1/completions first


## 2026-06-04

Expanded /v1/completions prototype into detailed implementation phases

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded phased implementation plan
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/tasks.md — Detailed phase checklist for implementation


## 2026-06-04

Phase 1: added /v1/completions server skeleton, wire types, validation, and tests

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/cmd/llm-proxy-server/main.go — New prototype server entrypoint
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaicompletions/types.go — OpenAI Completions request/response/chunk wire types
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/server/server.go — Health and /v1/completions handlers
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded Phase 1 implementation


## 2026-06-04

Phase 2: added Geppetto profile resolver, model listing, engine-provider seam, and tests

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/profiles/resolver.go — Geppetto profile YAML resolver and profile slug lookup
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/runtime/engine_provider.go — Geppetto engine factory wrapper
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/server/server.go — Added /v1/models handler seam
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/reference/01-investigation-diary.md — Phase 2 diary note


## 2026-06-04

Phase 3: wired non-streaming /v1/completions to Geppetto RunInferenceWithResult

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/cmd/llm-proxy-server/main.go — Wires profile-backed completion service when --profiles is provided
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaicompletions/mapper.go — Prompt-to-turn and turn-to-completion mapping
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/runtime/completion_service.go — Geppetto-backed non-streaming completion flow
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded Phase 3 implementation


## 2026-06-04

Phase 4: added streaming /v1/completions bridge from Geppetto text events to SSE chunks

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaicompletions/stream.go — EventTextDelta to Completions stream frame translation
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/runtime/completion_service.go — Streaming inference goroutine and event-sink attachment
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/server/sse.go — SSE writer for Completions chunks
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded Phase 4 implementation


## 2026-06-04

Phase 5: added examples and smoke-tested profile loading plus /healthz and /v1/models

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/examples/README.md — Prototype run and curl examples
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/examples/profiles.yaml — Example Geppetto profile YAML for model listing and provider-backed completions
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded Phase 5 validation and YAML correction


## 2026-06-04

Recorded final validation and commit ledger for /v1/completions prototype

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Final validation and commit ledger


## 2026-06-04

Created research logbook tracking useful, superseded, and needs-refresh resources

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/02-research-logbook.md — Research logbook deliverable


## 2026-06-04

Recorded research logbook creation in diary

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Diary entry for research logbook creation
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/02-research-logbook.md — Research logbook document


## 2026-06-04

Uploaded research logbook to reMarkable

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Recorded reMarkable upload details
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/02-research-logbook.md — Uploaded standalone research logbook


## 2026-06-04

Added /v1/chat/completions design and detailed implementation phases

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/04-simple-geppetto-engine-openai-chat-completions-proxy-prototype.md — Chat Completions design
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/tasks.md — Chat Completions phase checklist


## 2026-06-04

Recorded Chat Completions design step in diary

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/04-simple-geppetto-engine-openai-chat-completions-proxy-prototype.md — Chat Completions design
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Diary step for Chat Completions design


## 2026-06-04

Phase 6: added Chat Completions wire types and Geppetto turn mapping

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaichat/mapper.go — Chat message to Geppetto turn mapping and response mapping
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaichat/types.go — Chat Completions request/response/chunk structs and validation
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Phase 6 diary entry


## 2026-06-04

Implemented /v1/chat/completions with text messages, function tool mapping, non-streaming responses, SSE streaming, examples, and validation

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/examples/README.md — Chat examples
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaichat — Chat wire
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/runtime/chat_service.go — Chat runtime service
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/server/server.go — Chat HTTP endpoint


## 2026-06-04

Recorded Chat Completions implementation and tool support in diary

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaichat/mapper.go — Tool mapping implementation
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Diary step for Chat Completions implementation and tool support


## 2026-06-04

Annotated Chat Completions diary with implementation commit 8583723

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Commit hash for Chat Completions implementation diary


## 2026-06-04

Smoke tested /v1/chat/completions through Pinocchio and relaxed chat decoding for compatibility fields

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/examples/README.md — Pinocchio smoke-test instructions
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaichat/types.go — Unknown Chat Completions fields are tolerated
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Pinocchio smoke-test diary


## 2026-06-04

Annotated Pinocchio smoke-test diary with commit c9284d1

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Commit hash for Pinocchio smoke-test diary


## 2026-06-05

Live tool-call smoke tested Chat Completions and fixed request tool advertisement plus duplicate streamed arguments

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/openaichat/stream.go — Streaming tool argument duplicate suppression
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/runtime/chat_service.go — Request tools now become a Geppetto context registry
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md — Live tool-call smoke diary


## 2026-06-05

Archived live smoke-test scripts and non-secret JSON/SSE artifacts under ticket scripts/ for retraceability

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts — Smoke scripts and artifacts directory
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/00-smoke-test-artifacts.md — Artifact index and reproduction notes


## 2026-06-05

Renamed smoke artifact index to numbered docmgr-compatible markdown

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/00-smoke-test-artifacts.md — Artifact index and reproduction notes


## 2026-06-05

Fixed Anthropic Claude proxy no-response failure by forcing Geppetto Claude RunInference requests into streaming mode; all three backend tool-call smokes now pass (Geppetto commit fb2b9ed)

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/claude/engine_claude.go — Claude streaming-mode fix
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/artifacts/backend-tool-smoke-summary-after-geppetto-claude-stream-force.json — All-provider smoke summary


## 2026-06-05

Marked live three-backend tool-call smoke task complete after final all-pass summary

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/tasks.md — Task 64 complete


## 2026-06-05

Captured official Anthropic and OpenAI reasoning/streaming docs with Defuddle under ticket sources/ before continuing thinking-stream implementation

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/sources — Official docs source archive


## 2026-06-05

Implemented Claude extended thinking stream parsing and validated Claude/OpenAI Responses thinking smokes through engine and proxy (Geppetto commit 6928c321)

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/claude/content-block-merger.go — Claude thinking event mapping
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/artifacts/llm-proxy-thinking-smoke-summary-after-claude-thinking-fix.json — Direct engine thinking smoke evidence


## 2026-06-05

Fix profile resolver to merge sparse Geppetto profile overlays onto base settings; Gemini-backed proxy smokes pass

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/ttmp/2026/06/05/2026-06-05-geppetto-gemini-api-polish--geppetto-gemini-api-polish-for-gemini-3-flash/scripts/artifacts/llm-proxy-gemini-smoke-summary.json — Gemini proxy smoke evidence
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/profiles/resolver.go — Profile resolver base merge fix


## 2026-06-05

Add regression test for sparse Geppetto profile overlays in llm-proxy resolver

### Related Files

- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/profiles/resolver.go — Resolver behavior under test
- /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/pkg/profiles/resolver_test.go — Resolver regression test

