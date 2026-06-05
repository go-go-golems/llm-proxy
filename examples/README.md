# llm-proxy prototype examples

This prototype exposes the legacy OpenAI Completions endpoint first:

- `GET /healthz`
- `GET /v1/models`
- `POST /v1/completions`

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

## Notes

- Provider setup is intentionally not proxy config; it lives in Geppetto profile YAML.
- If the current Geppetto YAML loader does not expand `${ENV}` in API key fields, use the existing Geppetto-supported credential/config mechanism or replace the example values with local test values outside version control.
- OpenAI Responses support is deferred; see the ticket design docs for the later Responses bridge plan.
