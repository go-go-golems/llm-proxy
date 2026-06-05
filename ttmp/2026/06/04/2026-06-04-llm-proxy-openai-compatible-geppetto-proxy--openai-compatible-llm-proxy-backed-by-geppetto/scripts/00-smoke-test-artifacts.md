---
Title: Smoke-test scripts and artifacts
Ticket: 2026-06-04-llm-proxy-openai-compatible-geppetto-proxy
Status: active
Topics:
  - llm-proxy
  - geppetto
  - openai
  - anthropic
  - smoke-testing
DocType: reference
Intent: short-term
Owners: []
Summary: Index and reproduction notes for live provider smoke-test scripts and artifacts.
LastUpdated: 2026-06-05T07:30:00-04:00
WhatFor: Use this to rerun or inspect the live Chat Completions tool-call smoke tests.
WhenToUse: Read when debugging provider-specific tool-call behavior or reproducing backend smoke tests.
---

# Smoke-test scripts and artifacts

This directory stores the ad-hoc scripts and request/response artifacts used while testing the OpenAI-compatible `llm-proxy` prototype. It is intentionally part of the ticket so future work can retrace the commands, payloads, and observed failures.

## Scripts

- `01-generate-backend-smoke-profiles.py` reads local Pinocchio credentials from `~/.config/pinocchio/profiles.yaml` and writes `/tmp/llm-proxy-backend-smoke-profiles.yaml`. The generated file contains secrets and is **not** committed.
- `02-backend-tool-smoke.py` calls `/v1/chat/completions` against three temporary backend profiles: OpenAI Chat-compatible, Anthropic/Claude, and OpenAI Responses.
- `03-provider-tool-smoke.py` is the earlier provider smoke runner used with profiles from the local Pinocchio profile file.
- `04-inspect-claude-request.go` inspects the resolved Claude profile and generated Claude request shape for debugging the Anthropic `no response` failure.
- `05-claude-stream-flag-smoke.py` compares Claude proxy behavior with profile `chat.stream: false` versus `chat.stream: true` and preserves the request/response evidence.

## Artifacts

The `artifacts/` directory contains non-secret request JSON, response bodies, SSE transcripts, and redacted profile YAML. The important files are:

- `llm-proxy-tool-call-request.json` / `llm-proxy-tool-call-response.raw`: successful non-streaming Groq/OpenAI-compatible tool call after the context-registry fix.
- `llm-proxy-tool-result-request.json` / `llm-proxy-tool-result-response.raw`: follow-up client-driven tool-result request.
- `llm-proxy-tool-call-stream.sse`: streaming tool-call transcript before duplicate argument suppression.
- `llm-proxy-tool-call-stream-after-fix.sse`: streaming tool-call transcript after duplicate argument suppression.
- `backend-tool-smoke-summary.json`: cross-backend smoke summary before the Geppetto Claude streaming fix; OpenAI Chat and OpenAI Responses passed, Anthropic failed with `no response`.
- `before-geppetto-claude-stream-force-*`: focused reproduction showing `chat.stream: false` failed with `no response` while `chat.stream: true` succeeded before the fix.
- `after-geppetto-claude-stream-force-*`: focused reproduction showing both Claude stream flag variants succeeded after forcing Claude `RunInference` requests into streaming mode.
- `backend-tool-smoke-summary-after-geppetto-claude-stream-force.json`: final all-provider tool-call smoke summary; OpenAI Chat, Anthropic, and OpenAI Responses all returned OpenAI-compatible `tool_calls`.
- `anthropic-*.raw` and `haiku-*.raw`: Anthropic failure artifacts used to debug the backend-specific issue.
- `llm-proxy-backend-smoke-profiles.redacted.yaml`: redacted shape of the temporary backend smoke profiles.
- `llm-proxy-backend-smoke-profiles-after-geppetto-claude-stream-force.redacted.yaml`: redacted shape of the final all-provider smoke profile file.

## Reproduction outline

```bash
cd /home/manuel/workspaces/2026-06-04/llm-proxy/llm-proxy
python3 ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/01-generate-backend-smoke-profiles.py

go run ./cmd/llm-proxy-server \
  --profiles /tmp/llm-proxy-backend-smoke-profiles.yaml \
  --listen 127.0.0.1:18083

python3 ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/02-backend-tool-smoke.py
```

Never commit `/tmp/llm-proxy-backend-smoke-profiles.yaml`; it contains local API keys.
