# llm-proxy prototype examples

This prototype exposes OpenAI-compatible text endpoints backed by Geppetto engine profiles:

- `GET /healthz`
- `GET /v1/models`
- `POST /v1/completions`
- `POST /v1/chat/completions`

The `model` field is interpreted as a Geppetto engine profile slug from the loaded profile YAML.

## Run

```bash
go run ./cmd/llm-proxy-server --profiles ./examples/profiles.yaml --listen 127.0.0.1:8080
```

## Health

```bash
curl -sS http://127.0.0.1:8080/healthz
```

## List profiles as models

```bash
curl -sS http://127.0.0.1:8080/v1/models | jq .
```

## Non-streaming completion

```bash
curl -sS http://127.0.0.1:8080/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"sonnet","prompt":"Write one sentence about event sinks.","stream":false}' | jq .
```

## Streaming completion

```bash
curl -N http://127.0.0.1:8080/v1/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"sonnet","prompt":"Write one sentence about event sinks.","stream":true}'
```

## Non-streaming chat completion

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

## Streaming chat completion

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

## Chat completion with tool advertisement

The proxy accepts OpenAI-style function tools, maps them to Geppetto per-turn tool definitions, and maps generated Geppetto tool-call blocks back to OpenAI `tool_calls`. The proxy does not execute client tools itself; a client should send the tool result back in a later `role: "tool"` message.

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "sonnet",
    "messages": [
      {"role": "user", "content": "Look up order 123 and summarize it."}
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "lookup_order",
          "description": "Look up an order by id.",
          "parameters": {
            "type": "object",
            "properties": {
              "order_id": {"type": "string"}
            },
            "required": ["order_id"]
          }
        }
      }
    ],
    "tool_choice": "auto"
  }' | jq .
```

If the model returns a tool call, send the result back like this:

```bash
curl -sS http://127.0.0.1:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "sonnet",
    "messages": [
      {"role": "user", "content": "Look up order 123 and summarize it."},
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
    ],
    "tools": [
      {
        "type": "function",
        "function": {
          "name": "lookup_order",
          "parameters": {"type":"object","properties":{"order_id":{"type":"string"}}}
        }
      }
    ]
  }' | jq .
```

## Notes

- Provider setup is intentionally not proxy config; it lives in Geppetto profile YAML.
- Chat tool support currently means tool schema advertisement, assistant tool-call mapping, tool-result message mapping, and tool-call streaming chunks. The proxy does not execute arbitrary client tools.
- If the current Geppetto YAML loader does not expand `${ENV}` in API key fields, use the existing Geppetto-supported credential/config mechanism or replace the example values with local test values outside version control.
- OpenAI Responses support is deferred; see the ticket design docs for the later Responses bridge plan.

## Smoke test through Pinocchio

Pinocchio's OpenAI-compatible provider path uses Chat Completions. To smoke test this proxy with Pinocchio itself, add a Pinocchio profile whose OpenAI-compatible base URL points at the running proxy and whose engine is the upstream proxy model/profile slug.

For local development, Geppetto/Pinocchio currently rejects plain HTTP provider URLs and local-network provider targets. One working smoke-test shape is:

1. Run `llm-proxy-server` on localhost.
2. Expose it through a temporary HTTPS tunnel such as ngrok.
3. Add a Pinocchio profile with `openai-base-url: https://<ngrok-host>/v1` and `chat.engine: <proxy model slug>`.
4. Run:

```bash
pinocchio code unix --profile llm-proxy-groq-oss-20b --non-interactive \
  "Reply with exactly: llm-proxy chat smoke ok"
```

The smoke profile used during implementation pointed Pinocchio at `/v1/chat/completions` on this proxy and used `groq-oss-20b` as the model slug resolved by the proxy.
