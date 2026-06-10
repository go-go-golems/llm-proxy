---
Title: LLM Proxy Overview
Slug: llm-proxy-overview
Short: OpenAI-compatible proxy backed by Geppetto profile runtime configuration.
Topics:
- llm-proxy
- openai
- geppetto
Commands:
- llm-proxy-server
Flags:
- listen
- profiles
IsTopLevel: true
IsTemplate: false
ShowPerDefault: true
SectionType: GeneralTopic
---

`llm-proxy-server` exposes an OpenAI-compatible HTTP API and translates requests into Geppetto inference calls. Provider credentials and model routing live in Geppetto profile YAML; the proxy itself does not store API keys or provider routing tables.

The main runtime flow is:

1. Load optional profile YAML from `--profiles`.
2. Build OpenAI-compatible model, completion, and chat-completion services from those profiles.
3. Start an HTTP server on `--listen`.
4. Serve `/healthz`, `/v1/models`, `/v1/completions`, and `/v1/chat/completions`.

Example:

```bash
llm-proxy-server --profiles examples/profiles.yaml --listen 127.0.0.1:8080
```
