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
- llm-proxy-server serve
Flags:
- listen
- profiles
- byok-db
- byok-meter-transient-failure-threshold
- byok-meter-recovery-cooldown
IsTopLevel: true
IsTemplate: false
ShowPerDefault: true
SectionType: GeneralTopic
---

`llm-proxy-server` exposes an OpenAI-compatible HTTP API and translates requests into Geppetto inference calls. Model routing lives in Geppetto profile YAML. Without BYOK, provider credentials also come from profiles. With `--byok-db`, credentials come from the encrypted per-user vault and scoped capability tokens gate `/v1/*`.

The main runtime flow is:

1. Run the Glazed-backed `serve` command.
2. Load optional profile YAML from `--profiles`.
3. Build OpenAI-compatible model, completion, and chat-completion services from those profiles.
4. Start an HTTP server on `--listen`.
5. Serve liveness on `/healthz`, dependency readiness on `/readyz`, and the OpenAI-compatible `/v1/models`, `/v1/completions`, and `/v1/chat/completions` routes.

In BYOK mode, usage writes drive a fail-closed circuit. Persistent metering
failures open immediately; transient SQLite busy/locked failures open after
`--byok-meter-transient-failure-threshold`. While open, `/v1/*` returns 503 and
`/readyz` reports unavailable. After `--byok-meter-recovery-cooldown`, a
committed database write probe must succeed before inference resumes.

Example:

```bash
llm-proxy-server serve --profiles examples/profiles.yaml --listen 127.0.0.1:8080
```
