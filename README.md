# llm-proxy

An **OpenAI-compatible proxy** that translates `v1/completions` and `v1/chat/completions` requests into
[Geppetto](https://github.com/go-go-golems/geppetto) engine-inference calls.
Provider setup lives entirely in Geppetto profile YAML — no proxy-side API keys, auth, or route configuration.

## Quick start

```bash
go run ./cmd/llm-proxy-server \
  --profiles examples/profiles.yaml \
  --listen 127.0.0.1:8080
```

With profiles loaded, the proxy turns any standard OpenAI SDK call into a Geppetto inference run.
Without profiles it still serves `GET /healthz` and returns static stub responses.

---

## Endpoints

| Method | Path              | Description                             |
|--------|-------------------|-----------------------------------------|
| `GET`  | `/healthz`        | Liveness / health check                 |
| `GET`  | `/v1/models`      | List profiles as OpenAI-compatible models |
| `POST` | `/v1/completions` | Non-streaming & streaming text completion |
| `POST` | `/v1/chat/completions` | Non-streaming & streaming chat completion |

---

## How it works

```
OpenAI client  ──▶  llm-proxy  ──▶  Geppetto  ──▶  Provider API
                         │              │
                    profile YAML     engine
                               (Claude, Gemini, OpenAI,…)
```

1. The `model` field is treated as a **Geppetto engine profile slug** (e.g. `sonnet`, `gemini-3-flash-preview`).
2. The proxy resolves the slug against the profile YAML, merges base inference settings, and creates the appropriate
   Geppetto engine.
3. Requests and responses are mapped through the proxy's OpenAI-compatible types into Geppetto's internal
   `turns.Turn` representation and back again.

No routing table or provider configuration lives in the proxy — everything is driven by the profile file.

---

## Supported features

| Feature                  | `/v1/completions` | `/v1/chat/completions` |
|--------------------------|:-----------------:|:----------------------:|
| Non-streaming responses  |        ✅         |           ✅           |
| Streaming (SSE)          |        ✅         |           ✅           |
| Tool advertisement       |         –         |           ✅           |
| Client-driven tool loops |         –         |           ✅           |
| System / user / assistant / tool roles |    –         |           ✅           |
| Image input (multimodal) |         –         |           ✅           |
| Extended thinking / reasoning |      –         |           ✅           |
| OpenAI Responses API     |         –         |           🚧 deferred  |

> Chat tool support means the proxy advertises tools, maps assistant `tool_calls`, and maps client `role: "tool"`
> messages back into Geppetto tool-use blocks. The proxy itself does **not** execute arbitrary client tools.

---

## Profile YAML

The `--profiles` flag points to a Geppetto engine-profile YAML file.
Each entry defines one proxy-visible "model" slug.

```yaml
slug: default
display_name: Prototype profiles
profiles:
  sonnet:
    slug: sonnet
    display_name: Claude Sonnet through Geppetto
    inference_settings:
      chat:
        api_type: claude
        engine: claude-3-5-sonnet-20241022
        max_response_tokens: 1024
        temperature: 0.2
      api:
        api_keys:
          claude-api-key: ${ANTHROPIC_API_KEY}
        base_urls:
          claude-base-url: https://api.anthropic.com
  gemini:
    slug: gemini
    display_name: Gemini 3 Flash through Geppetto
    inference_settings:
      chat:
        api_type: gemini
        engine: gemini-3-flash-preview
        max_response_tokens: 2048
      api:
        api_keys:
          gemini-api-key: ${GEMINI_API_KEY}
```

Environment variables are expanded via Go template syntax (`${VAR}`).
API keys **must not** be committed; use the `${…}` form or populate the file locally.

---

## Examples

### Health check

```bash
curl -sS http://127.0.0.1:8080/healthz
# {"status":"ok"}
```

### List models

```bash
curl -sS http://127.0.0.1:8080/v1/models | jq .
# {
#   "object": "list",
#   "data": [
#     {"id": "sonnet", "object": "model", "owned_by": "geppetto"},
#     {"id": "gemini", "object": "model", "owned_by": "geppetto"}
#   ]
# }
```

### Non-streaming chat completion

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "sonnet",
    "messages": [
      {"role": "system", "content": "Answer in one sentence."},
      {"role": "user", "content": "What does an event sink do?"}
    ],
    "stream": false
  }' | jq .
```

### Streaming chat completion

```bash
curl -N http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "sonnet",
    "messages": [
      {"role": "user", "content": "Write a short greeting."}
    ],
    "stream": true
  }'
```

### Chat with tool calls

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "sonnet",
    "messages": [
      {"role": "user", "content": "Look up order 123."}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "lookup_order",
          "description": "Look up an order by id.",
          "parameters": {
            "type": "object",
            "properties": { "order_id": {"type": "string"} },
            "required": ["order_id"]
          }
        }
      }
    ],
    "tool_choice": "auto"
  }' | jq .
```

If the model returns a `tool_calls` array, send the result back in a follow-up request with a `role: "tool"` message:

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "sonnet",
    "messages": [
      {"role": "user", "content": "Look up order 123."},
      {
        "role": "assistant",
        "content": null,
        "tool_calls": [
          {
            "id": "call_1",
            "type": "function",
            "function": {"name": "lookup_order", "arguments": "{\"order_id\":\"123\"}"}
          }
        ]
      },
      {"role": "tool", "tool_call_id": "call_1", "content": "{\"status\":\"shipped\"}"}
    ]
  }' | jq .
```

### Image input (multimodal)

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gemini",
    "messages": [
      {
        "role": "user",
        "content": [
          {"type": "text", "text": "Describe this image."},
          {
            "type": "image_url",
            "image_url": {"url": "data:image/png;base64,iVBOR…"}
          }
        ]
      }
    ],
    "stream": false
  }' | jq .
```

---

## Project structure

```
cmd/llm-proxy-server/      — CLI entry point
pkg/profiles/              — Profile resolution from YAML
pkg/openaichat/            — Chat completion request/response types + mapper
pkg/openaicompletions/     — Legacy completion types + mapper
pkg/runtime/               — Geppetto-backed completion/chat services
pkg/server/                — HTTP server + SSE helpers
examples/                  — Example profiles and usage docs
```

---

## Notes

- **No user auth, no key management** — this is a prototype proxy. API keys are stored in the profile YAML.
- **OpenAI Responses API** (`/v1/responses`) support is deferred.
- **Smoke testing through Pinocchio** — Pinocchio can be pointed at the proxy as an OpenAI-compatible provider by
  configuring a Pinocchio profile with the proxy's base URL and model slug. (See `examples/README.md` for details.)

---

## License

MIT — see `LICENSE`.
