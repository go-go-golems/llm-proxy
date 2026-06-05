---
Title: Research logbook
Ticket: 2026-06-04-llm-proxy-openai-compatible-geppetto-proxy
Status: active
Topics:
    - llm-proxy
    - inference
    - geppetto
    - openai
    - anthropic
DocType: reference
Intent: long-term
Owners: []
RelatedFiles:
    - Path: geppetto/pkg/engineprofiles/codec_yaml_runtime.go
      Note: Profile YAML validation behavior evaluated
    - Path: geppetto/pkg/engineprofiles/registry.go
      Note: Profile slug resolution resource evaluated
    - Path: geppetto/pkg/events/context.go
      Note: Streaming event sink resource evaluated
    - Path: geppetto/pkg/inference/engine/run_with_result.go
      Note: Geppetto inference result bridge resource evaluated
    - Path: geppetto/pkg/turns/types.go
      Note: Turn/block bridge model evaluated
    - Path: llm-proxy/examples/README.md
      Note: Current prototype usage docs evaluated as needing README integration later
    - Path: llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/01-openai-compatible-llm-proxy-design-and-implementation-guide.md
      Note: Superseded broad design evaluated for long-term context
    - Path: llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md
      Note: Deferred Responses design evaluated as useful background
    - Path: llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/03-simple-geppetto-engine-openai-completions-proxy-prototype.md
      Note: |-
        Current implementation design whose supporting resources are tracked in this logbook.
        Current implementation design whose sources are evaluated
    - Path: llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md
      Note: Chronological implementation record that references many of the resources summarized here.
ExternalSources:
    - https://platform.openai.com/docs/api-reference/completions/create
    - https://platform.openai.com/docs/api-reference/responses/create
    - https://platform.openai.com/docs/api-reference/chat/create
    - https://docs.anthropic.com/en/api/messages
Summary: Logbook of documents, code resources, and external API references consulted or cited while designing and implementing the Geppetto-backed /v1/completions prototype.
LastUpdated: 2026-06-05T00:55:00-04:00
WhatFor: Use this to decide which resources remain useful, which ones are superseded, and what needs refreshing before continuing the proxy.
WhenToUse: Read before updating the proxy design, implementing deferred Responses support, or reviewing whether documentation/source references are current.
---


# Research logbook

## Goal

Track the documents, code resources, and external references used while designing and implementing the Geppetto-backed OpenAI-compatible proxy prototype. The logbook records why each resource was consulted, how it was found, what was useful, what was not useful, what is out of date or wrong, and what should be updated.

## Context

The ticket started with a broad OpenAI-compatible proxy design, then narrowed twice:

1. Use Geppetto engines directly instead of direct provider adapters.
2. Expose legacy OpenAI Completions (`POST /v1/completions`) first, with OpenAI Responses deferred.

This means some early resources remain useful as background but are no longer authoritative for the current implementation. The current implementation authority is:

- `design-doc/03-simple-geppetto-engine-openai-completions-proxy-prototype.md`
- `reference/01-investigation-diary.md`
- `tasks.md`
- source code under `cmd/llm-proxy-server`, `pkg/openaicompletions`, `pkg/profiles`, `pkg/runtime`, and `pkg/server`

## Status legend

- **Current:** Still directly relevant to the `/v1/completions` prototype.
- **Useful background:** Useful for context, but not the current implementation target.
- **Superseded:** Kept for history; do not implement from it without re-checking scope.
- **Needs refresh:** Should be re-read or verified before future implementation.
- **Not directly read:** Cited or known, but not fetched/read in this session; verify before relying on details.

## Logbook entries

### 1. Current design: Simple Geppetto Engine OpenAI Completions Proxy Prototype

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/03-simple-geppetto-engine-openai-completions-proxy-prototype.md`
- **Status:** Current.
- **What I was researching:** The current implementation plan for the first prototype.
- **What I was looking for in this document:** The target endpoint, scope, package layout, phase plan, and mapping rules for `/v1/completions`.
- **Why I chose it:** It was created after the user clarified that the first exposed API should be OpenAI Completions rather than OpenAI Responses.
- **How I found it:** Created with `docmgr doc add` as the copy/adaptation of the Responses design.
- **What I found useful:** The document clearly states `model == Geppetto profile slug`, `prompt -> turns.NewUserTextBlock`, `RunInferenceWithResult`, and text-delta SSE mapping.
- **What I didn't find useful:** It intentionally omits exact implementation details for request overrides, provider credential expansion, and live provider smoke testing.
- **What is out of date / wrong:** Nothing known for the current prototype, but it assumes “OpenAI completions” means legacy `/v1/completions`.
- **What would need updating:** Update after adding request override mapping, better error classification, or if the target endpoint changes to `/v1/chat/completions`.

### 2. Preserved future design: Simple Geppetto Engine OpenAI Responses Proxy Prototype

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md`
- **Status:** Useful background / deferred.
- **What I was researching:** How to expose an OpenAI Responses-compatible API using Geppetto engines.
- **What I was looking for in this document:** Responses input/output/event mapping concepts and reusable seams such as `ProfileResolver`, `EngineProvider`, `ResponsesMapper`, and `EventTranslator`.
- **Why I chose it:** It was the first simplified design after the user rejected direct provider adapters.
- **How I found it:** Created in the same ticket, then intentionally preserved when the user asked to switch to Completions first.
- **What I found useful:** The event-sink pattern and profile-slug routing rule carried forward into the Completions implementation.
- **What I didn't find useful:** The Responses object model is too large for the first prototype and should not drive current code.
- **What is out of date / wrong:** It says the first prototype target is `/v1/responses`; that is no longer true.
- **What would need updating:** Before implementing Responses later, update it with lessons from the Completions implementation, especially the actual package names and stream writer design.

### 3. Original broad proxy design

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/design-doc/01-openai-compatible-llm-proxy-design-and-implementation-guide.md`
- **Status:** Superseded for implementation; useful long-term background.
- **What I was researching:** A comprehensive OpenAI-compatible proxy that routes directly to OpenAI Chat, OpenAI Responses, and Anthropic backends.
- **What I was looking for in this document:** Original thinking on route config, auth, credential seams, provider adapters, and long-term user-key management.
- **Why I chose it:** It was the first design produced for the ticket.
- **How I found it:** Created in this ticket before the user narrowed the scope.
- **What I found useful:** Long-term concerns around auth, credential isolation, route aliases, and future user-managed keys.
- **What I didn't find useful:** Direct provider adapters and route config are intentionally not part of the current prototype.
- **What is out of date / wrong:** It recommends not using `engine.Engine`; that is explicitly wrong for the revised prototype.
- **What would need updating:** Mark more prominently as superseded for v1. If revisited, separate long-term architecture from prototype implementation.

### 4. Earlier May 2026 preliminary design

- **Resource:** `/home/manuel/workspaces/2026-05-04/llm-proxy-server/geppetto/ttmp/2026/05/04/2026-05-04-llm-proxy-server--generic-llm-proxy-server-design/design-doc/01-generic-llm-proxy-server-design-and-implementation-guide.md`
- **Status:** Superseded / historical background.
- **What I was researching:** Prior thinking on a generic LLM proxy server.
- **What I was looking for in this document:** Existing architecture arguments, provider mapping details, and protocol notes that might still be useful.
- **Why I chose it:** The user explicitly pointed to it in the first ticket request.
- **How I found it:** User-provided absolute path.
- **What I found useful:** It contained a clear description of protocol families and direct-adapter tradeoffs.
- **What I didn't find useful:** It targeted a broader direct-wire proxy rather than the current Geppetto-engine prototype.
- **What is out of date / wrong:** Its implementation direction is no longer current for this repo/ticket.
- **What would need updating:** Add a note pointing readers to design doc 03 for the current `/v1/completions` prototype.

### 5. Ticket investigation diary

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/reference/01-investigation-diary.md`
- **Status:** Current.
- **What I was researching:** Chronological decisions, commands, failures, commits, and validation results.
- **What I was looking for in this document:** What changed, why scope changed, what commands passed/failed, and which commits correspond to which implementation phases.
- **Why I chose it:** The user asked to keep a detailed diary.
- **How I found it:** Created as part of the ticket workflow and appended after each major phase.
- **What I found useful:** Exact errors and commands, especially the profile YAML `default_profile_slug` failure and final commit ledger.
- **What I didn't find useful:** It is chronological and verbose; it is not the quickest place to find current API contracts.
- **What is out of date / wrong:** Earlier steps describe superseded designs; later steps correct them.
- **What would need updating:** Continue appending after each implementation change and add links to new commits.

### 6. Ticket task list

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/tasks.md`
- **Status:** Current.
- **What I was researching:** The implementation phase checklist.
- **What I was looking for in this document:** Which `/v1/completions` phases were complete and which tasks remain deferred.
- **Why I chose it:** The user asked to add detailed phases and tasks to the ticket.
- **How I found it:** Ticket-generated task document.
- **What I found useful:** It now shows completed phases 1–5 and deferred items.
- **What I didn't find useful:** It does not contain implementation rationale; use the diary/design for that.
- **What is out of date / wrong:** Nothing known after the final update; deferred items remain intentionally open.
- **What would need updating:** Add new tasks for request overrides, error classification, live smoke tests, and eventual Responses support.

### 7. Geppetto engine profile registry interface

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/registry.go`
- **Status:** Current.
- **What I was researching:** How to resolve profile slugs into inference settings.
- **What I was looking for in this document:** `ResolveInput`, `ResolvedEngineProfile`, and the `Registry` interface.
- **Why I chose it:** The implementation rule is `request.model == EngineProfileSlug`.
- **How I found it:** Repository search for `ResolveInput`, `ResolvedEngineProfile`, and profile registry code.
- **What I found useful:** It provided the exact interface for the proxy's `ProfileResolver` wrapper.
- **What I didn't find useful:** It does not describe YAML source loading; that lives elsewhere.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** If Geppetto changes profile resolution semantics, update `pkg/profiles/resolver.go` and design doc 03.

### 8. Geppetto profile YAML/source-chain code

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/source_chain.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/file_store_yaml.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/engineprofiles/codec_yaml_runtime.go`
- **Status:** Current / needs ongoing verification.
- **What I was researching:** How to load profile YAML and how strict the YAML format is.
- **What I was looking for in this document:** Source kinds, YAML store behavior, and codec validation rules.
- **Why I chose it:** The proxy loads profile YAML with `--profiles`.
- **How I found it:** Repository search under `geppetto/pkg/engineprofiles`.
- **What I found useful:** The codec revealed that `default_profile_slug` is rejected in engine profile YAML and defaults are inferred.
- **What I didn't find useful:** Source-chain code was more general than the current implementation; the prototype uses a low-level YAML resolver for simplicity.
- **What is out of date / wrong:** The design-doc example initially used `default_profile_slug`; smoke testing proved that wrong for current Geppetto YAML.
- **What would need updating:** Update design examples to remove `default_profile_slug` or use a `default` profile. Consider switching from low-level YAML store to source-chain if multiple profile sources are needed.

### 9. Geppetto CLI bootstrap engine settings

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/cli/bootstrap/engine_settings.go`
- **Status:** Useful background.
- **What I was researching:** Existing Geppetto pattern for profile-to-engine construction.
- **What I was looking for in this document:** How Geppetto resolves profile settings and creates engines with the standard factory.
- **Why I chose it:** The proxy should follow existing Geppetto patterns rather than inventing a new engine construction path.
- **How I found it:** Repository search for `NewEngineFromResolvedCLIEngineSettings` and `ResolveEngineProfile`.
- **What I found useful:** It confirmed `factory.NewStandardEngineFactory().CreateEngine(settings)` as the normal engine creation path.
- **What I didn't find useful:** The CLI bootstrap includes config/env/default merging machinery that the prototype does not use yet.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** If the proxy later needs Geppetto's full config/env merge behavior, revisit this file and possibly reuse more bootstrap code.

### 10. Geppetto engine interface and result helper

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/engine/engine.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/engine/run_with_result.go`
- **Status:** Current.
- **What I was researching:** The execution boundary and how to get canonical inference metadata.
- **What I was looking for in this document:** `Engine.RunInference` and `RunInferenceWithResult`.
- **Why I chose it:** The prototype's central requirement is to use Geppetto's inference engine.
- **How I found it:** Repository search for `RunInference` and `RunInferenceWithResult`.
- **What I found useful:** `RunInferenceWithResult` handles both engines that return canonical results and engines that only append blocks.
- **What I didn't find useful:** It does not define HTTP or OpenAI response mapping; that belongs in llm-proxy.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** If Geppetto's engine interface gains streaming methods or result semantics change, update `pkg/runtime/completion_service.go`.

### 11. Geppetto standard engine factory

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/engine/factory/factory.go`
- **Status:** Current.
- **What I was researching:** How to instantiate provider engines from Geppetto `InferenceSettings`.
- **What I was looking for in this document:** Supported providers and factory validation behavior.
- **Why I chose it:** The proxy's `FactoryEngineProvider` wraps this factory.
- **How I found it:** Earlier broad design research and repository search for provider creation.
- **What I found useful:** It confirmed OpenAI, OpenAI Responses, Claude/Anthropic, and Gemini provider support through `Chat.ApiType`.
- **What I didn't find useful:** Factory validation can require API keys/base URLs, which means tests need fake engine providers instead of real factory calls.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** If provider aliases or required settings change, update example profiles and error guidance.

### 12. Geppetto inference settings

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/settings/settings-inference.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/settings/settings-chat.go`
- **Status:** Current / needs follow-up for request overrides.
- **What I was researching:** How profiles represent provider API keys, base URLs, model names, token limits, and sampling defaults.
- **What I was looking for in this document:** `InferenceSettings`, `APISettings`, and `ChatSettings` fields.
- **Why I chose it:** The proxy intentionally relies on profile YAML for provider setup.
- **How I found it:** Repository search and design evidence gathering.
- **What I found useful:** `APISettings` has API key/base URL maps, and `ChatSettings` has engine, api type, max response tokens, temperature, top-p, stop, and stream fields.
- **What I didn't find useful:** It did not answer whether `${ENV}` placeholders in YAML are expanded in the low-level YAML store path.
- **What is out of date / wrong:** The example profiles may be wrong if `${ENV}` expansion is not supported in this path.
- **What would need updating:** Verify credential expansion and decide whether to reuse Geppetto bootstrap config merging for production-like runs.

### 13. Geppetto events and event sinks

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/context.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/sink.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/canonical_events.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/events/chat-events.go`
- **Status:** Current.
- **What I was researching:** How to capture streaming output from Geppetto engines.
- **What I was looking for in this document:** `EventSink`, `WithEventSinks`, `PublishEventToContext`, `EventTextDelta`, and event metadata.
- **Why I chose it:** Streaming `/v1/completions` maps Geppetto text events to OpenAI SSE chunks.
- **How I found it:** Repository search for `EventSink`, `WithEventSinks`, and canonical event types.
- **What I found useful:** The context-attached event-sink pattern made streaming possible without direct provider adapters.
- **What I didn't find useful:** There is no OpenAI-specific stream mapper; llm-proxy implements that.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** Add mappings for reasoning/tool events only when the exposed API supports them; legacy Completions currently ignores them.

### 14. Geppetto turns and block helpers

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/turns/types.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/turns/helpers_blocks.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/turns/inference_result.go`
- **Status:** Current.
- **What I was researching:** The internal data model used between the proxy request and Geppetto engine.
- **What I was looking for in this document:** `Turn`, `Block`, block kinds, payload keys, helper constructors, and inference result fields.
- **Why I chose it:** Completions prompt mapping and generated-text extraction depend on these types.
- **How I found it:** Repository search for `type Turn`, `type Block`, `PayloadKeyText`, and block helper constructors.
- **What I found useful:** `NewUserTextBlock`, `NewAssistantTextBlock`, `PayloadKeyText`, and canonical usage fields.
- **What I didn't find useful:** There is no direct OpenAI Completion representation; mapping remains in llm-proxy.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** If Geppetto changes block kind semantics, update `generatedAssistantText` in `pkg/openaicompletions/mapper.go`.

### 15. Geppetto provider protocol references

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai/chat_types.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai/chat_stream.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai_responses/helpers.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/openai_responses/stream_events.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/steps/ai/claude/api/messages.go`
- **Status:** Useful background / mostly superseded for the current implementation.
- **What I was researching:** Provider-specific request/response shapes and streaming behavior.
- **What I was looking for in this document:** OpenAI Chat, OpenAI Responses, and Anthropic Messages fields that a broad proxy might translate directly.
- **Why I chose it:** Early designs considered direct provider adapters.
- **How I found it:** Repository `find`/`rg` under `geppetto/pkg/steps/ai`.
- **What I found useful:** It confirmed Geppetto already owns provider protocol complexity.
- **What I didn't find useful:** Direct provider wire mapping is no longer part of the current prototype.
- **What is out of date / wrong:** Not wrong, but no longer the right implementation layer for `/v1/completions` prototype.
- **What would need updating:** Revisit only if adding direct provider adapter support later.

### 16. Geppetto outbound URL safety

- **Resource:** `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/security/outbound_url.go`
- **Status:** Useful background / deferred.
- **What I was researching:** How Geppetto validates provider URLs.
- **What I was looking for in this document:** Whether provider clients already protect against unsafe upstream URLs.
- **Why I chose it:** The original broad proxy design had SSRF concerns around user-controlled upstream URLs.
- **How I found it:** Repository search for `ValidateOutboundURL`.
- **What I found useful:** Geppetto already rejects unsafe schemes and local networks unless explicitly allowed.
- **What I didn't find useful:** The current prototype has no proxy-level upstream URL config.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** Revisit when adding user-editable profiles or DB-backed provider settings.

### 17. Geppetto runner/session/tool-loop resources

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/runner/types.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/runner/run.go`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/geppetto/pkg/inference/toolloop/enginebuilder/builder.go`
- **Status:** Useful background / deferred.
- **What I was researching:** Whether to use the higher-level runner/tool-loop instead of direct engine calls.
- **What I was looking for in this document:** Existing abstractions for sessions, tools, middleware, event sinks, and prepared runs.
- **Why I chose it:** The implementation might later need tools or middleware.
- **How I found it:** Repository search for event sinks and `RunInference` wrappers.
- **What I found useful:** It showed a richer future path if the proxy needs tools/session orchestration.
- **What I didn't find useful:** It is too much machinery for the first `/v1/completions` prototype.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** Revisit when adding tools, middleware, or durable sessions.

### 18. llm-proxy module and project docs

- **Resources:**
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/AGENT.md`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/README.md`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy/go.mod`
  - `/home/manuel/workspaces/2026-06-04/llm-proxy/go.work`
- **Status:** Current.
- **What I was researching:** Project conventions, module state, and workspace layout.
- **What I was looking for in this document:** Build/test commands, module name, existing dependencies, and whether llm-proxy was ready to host code.
- **Why I chose it:** Needed to know where to implement and how to validate.
- **How I found it:** Workspace discovery with `ls`, `find`, and direct reads.
- **What I found useful:** `AGENT.md` gave build/test conventions; `go.mod` showed the module; `go.work` showed local Geppetto/glazed/pinocchio modules.
- **What I didn't find useful:** `README.md` was still a template and did not describe the proxy.
- **What is out of date / wrong:** `README.md` is out of date for the new prototype.
- **What would need updating:** Update `README.md` to point to `examples/README.md` and describe `/v1/completions`.

### 19. Process/reference docs from skills

- **Resources:**
  - `/home/manuel/.pi/agent/skills/ticket-research-docmgr-remarkable/references/writing-style.md`
  - `/home/manuel/.pi/agent/skills/ticket-research-docmgr-remarkable/references/deliverable-checklist.md`
  - `/home/manuel/.pi/agent/skills/diary/references/diary.md`
  - `/home/manuel/.pi/agent/skills/git-commit-instructions/SKILL.md`
- **Status:** Current process guidance.
- **What I was researching:** How to structure ticket docs, diary entries, reMarkable upload, and commits.
- **What I was looking for in this document:** Required sections, validation checklist, diary format, and commit hygiene.
- **Why I chose it:** The user asked for docmgr ticket docs, reMarkable upload, detailed diary, and commits.
- **How I found it:** Loaded through available/pinned skills and the git-commit skill when committing was requested.
- **What I found useful:** The checklist kept the ticket validated and diary/commit boundaries explicit.
- **What I didn't find useful:** These docs do not inform proxy runtime architecture.
- **What is out of date / wrong:** Nothing known.
- **What would need updating:** No proxy-specific updates needed.

### 20. OpenAI Completions API reference

- **Resource:** `https://platform.openai.com/docs/api-reference/completions/create`
- **Status:** Needs refresh / not directly fetched in this session.
- **What I was researching:** Legacy OpenAI Completions request/response/stream shape.
- **What I was looking for in this document:** `POST /v1/completions`, `prompt`, `text_completion`, streaming chunk shape, and finish reasons.
- **Why I chose it:** It is the authoritative external API reference for the endpoint the prototype exposes.
- **How I found it:** Known OpenAI API reference URL; cited in the design doc.
- **What I found useful:** The endpoint name and legacy response concept guided the design.
- **What I didn't find useful:** It was not directly fetched/read in this session, so exact edge cases are not verified.
- **What is out of date / wrong:** The current implementation may be too strict (`DisallowUnknownFields`) and may not cover real OpenAI optional fields.
- **What would need updating:** Fetch/read the current OpenAI Completions docs before hardening compatibility. Verify prompt arrays, unknown fields, `n`, `echo`, `logprobs`, and streaming format.

### 21. OpenAI Responses API reference

- **Resource:** `https://platform.openai.com/docs/api-reference/responses/create`
- **Status:** Deferred / not directly fetched in this session.
- **What I was researching:** Future Responses support.
- **What I was looking for in this document:** Responses input/output/event structure.
- **Why I chose it:** The previous design targeted `/v1/responses` before the user changed the prototype target.
- **How I found it:** Known OpenAI API reference URL; cited in design docs.
- **What I found useful:** It framed future work but did not drive current code.
- **What I didn't find useful:** Not needed for the current `/v1/completions` implementation.
- **What is out of date / wrong:** Not directly verified in this session.
- **What would need updating:** Re-read before implementing design doc 02.

### 22. OpenAI Chat Completions API reference

- **Resource:** `https://platform.openai.com/docs/api-reference/chat/create`
- **Status:** Deferred / not directly fetched in this session.
- **What I was researching:** Broader OpenAI-compatible proxy possibilities.
- **What I was looking for in this document:** Chat request/response compatibility for future clients.
- **Why I chose it:** The original broad proxy design included chat compatibility.
- **How I found it:** Known OpenAI API reference URL; cited in the broad design.
- **What I found useful:** Useful context for future, not current implementation.
- **What I didn't find useful:** Not relevant to the Completions-first prototype.
- **What is out of date / wrong:** Not directly verified in this session.
- **What would need updating:** Re-read only if adding `/v1/chat/completions`.

### 23. Anthropic Messages API reference

- **Resource:** `https://docs.anthropic.com/en/api/messages`
- **Status:** Deferred / not directly fetched in this session.
- **What I was researching:** Claude provider protocol details for the broad design.
- **What I was looking for in this document:** Anthropic message shape and streaming behavior.
- **Why I chose it:** The broad design considered Claude mapping.
- **How I found it:** Known Anthropic API docs URL; cited in design docs.
- **What I found useful:** It helped frame provider complexity as something Geppetto should own.
- **What I didn't find useful:** The current proxy does not talk to Anthropic directly.
- **What is out of date / wrong:** Not directly verified in this session.
- **What would need updating:** Re-read only if bypassing Geppetto and adding direct Anthropic adapters, which is not the current plan.

## Update priorities

### Update now / before next implementation pass

1. **OpenAI Completions compatibility details:** Fetch the current OpenAI Completions API docs and decide whether to relax `DisallowUnknownFields`.
2. **Profile credential expansion:** Verify whether Geppetto YAML profile loading expands `${ENV}` placeholders in API key fields for the path used by `NewYAMLResolver`.
3. **Error classification:** Decide how unknown profile errors should map to OpenAI-style HTTP status codes.

### Update before Responses work

1. Re-read OpenAI Responses API docs directly.
2. Update design doc 02 using lessons from the implemented Completions bridge.
3. Add a new task phase for Responses input/output/event mapping.

### Update before multi-user/key work

1. Revisit broad design doc 01 for auth/key ideas, but do not treat its direct-adapter plan as current.
2. Revisit Geppetto profile store/source-chain code for DB-backed/user-specific profile resolution.
3. Revisit outbound URL validation if user-editable profiles can contain base URLs.

## Related

- Current implementation design: `../design-doc/03-simple-geppetto-engine-openai-completions-proxy-prototype.md`
- Deferred Responses design: `../design-doc/02-simple-geppetto-engine-openai-responses-proxy-prototype.md`
- Superseded broad design: `../design-doc/01-openai-compatible-llm-proxy-design-and-implementation-guide.md`
- Implementation diary: `01-investigation-diary.md`
- Task checklist: `../tasks.md`
