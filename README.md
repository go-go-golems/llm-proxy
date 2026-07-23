# llm-proxy

An **OpenAI-compatible proxy** that translates `v1/completions` and `v1/chat/completions` requests into
[Geppetto](https://github.com/go-go-golems/geppetto) engine-inference calls.
Provider setup lives in Geppetto profile YAML. Optional BYOK mode adds encrypted
per-user provider credentials, scoped capability tokens, budgets, audit, and
usage metering.

## Quick start

```bash
go run ./cmd/llm-proxy-server serve \
  --profiles examples/profiles.yaml \
  --listen 127.0.0.1:8080
```

With profiles loaded, the proxy turns any standard OpenAI SDK call into a Geppetto inference run.
Without profiles it still serves `GET /healthz` and returns static stub responses.

---

## Endpoints

| Method | Path              | Description                             |
|--------|-------------------|-----------------------------------------|
| `GET`  | `/healthz`        | Process liveness                         |
| `GET`  | `/readyz`         | Dependency readiness; 503 when durable BYOK metering is unhealthy |
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

## BYOK durability and metering safety

Passing `--byok-db` enables the encrypted credential vault, scoped bearer-token
enforcement, and durable usage accounting. BYOK requires `--profiles` and a
vault master key supplied through the documented secret input. Security-sensitive
credential and token mutations commit with their typed audit event in one
transaction.

The SQLite store uses forward-only `PRAGMA user_version` migrations. Startup:

- creates and stamps an empty database;
- validates and upgrades the legacy unversioned schema without deleting data;
- validates current tables, security-critical indexes, and foreign keys;
- rejects malformed or newer schemas instead of guessing or downgrading.

Metering has a shared fail-closed circuit. Persistent write failures open it
immediately; transient SQLite busy/locked failures open it after a bounded
threshold. While open, `/v1/*` returns `503 metering_unavailable` before provider
dispatch and `/readyz` returns 503. After the cooldown, one caller runs a
committed write probe; inference resumes only if that probe succeeds.

Relevant options:

| Option | Default | Purpose |
| --- | --- | --- |
| `--byok-meter-transient-failure-threshold` | `3` | Consecutive transient metering failures before opening |
| `--byok-meter-recovery-cooldown` | `5s` | Delay before attempting a committed recovery probe |

`/healthz` remains a liveness endpoint and stays healthy while a dependency is
down, preventing restart loops caused by a temporarily unavailable database.

---

## Coding-agent grants and device login

When BYOK and the agent OIDC resource-client options are enabled, browser users
can create revocable agent grants under `/app`. A grant binds an owned provider
credential to an explicit profile/model allowlist, per-capability limits, a
bounded capability TTL, a per-installation active-token limit, and cumulative
grant request/token budgets. Cumulative counters belong to the grant and are
not reset by capability rotation or reissue. Credential deletion and grant
revocation cascade to child capabilities in the same audited transaction.

The coding-agent CLI uses RFC 8628 and never receives provider credentials:

```bash
llm-proxy-server byok agent login \
  --issuer https://idp.localhost:18443 \
  --client-id llm-proxy-agent \
  --audience https://proxy.localhost:18443/agent/v1 \
  --broker https://proxy.localhost:18443
llm-proxy-server byok agent status
llm-proxy-server byok agent logout
```

If more than one grant is eligible, login fails until the operator supplies an
explicit `--grant-id`; the client never chooses a credential or model policy.
On POSIX systems the generated installation ID and scoped `llmp_...` capability
are lock-serialized and atomically stored in a mode-`0600` regular file below a
mode-`0700` directory. Symlinks and permissive files are rejected. Persistent
agent credential caching intentionally fails closed on unsupported non-POSIX
platforms.

The token classes are deliberately disjoint. A validated tiny-idp access token
with exact issuer, resource audience, authorized client, expiry, Bearer type,
and `llm.tokens.issue` scope is accepted only by `/agent/v1/*`. The exchanged
`llmp_...` capability is accepted only by `/v1/*`. This does **not** claim
OpenAI Responses, Anthropic-native Messages, arbitrary coding-agent, or
live-provider compatibility; the currently implemented inference surface is
the documented OpenAI-compatible Chat Completions and legacy Completions
surface. A bounded live acceptance on 2026-07-23 validated the
`umans-glm-5.2` Pinocchio profile through `/v1/chat/completions`: the provider
returned HTTP 200 with the requested content, durable usage recorded 19 prompt
and 24 completion tokens, revocation changed subsequent capability access to
401, and the credential was deleted afterward. A follow-up live matrix verified
429 rejection for exhausted token, request, cumulative-grant, and RPM limits;
per-capability request budgets reset on rotation while grant counters persisted.
Token ceilings are post-accounting stops: one completed request can cross the
ceiling because final provider usage is unknown, and the next request is
rejected before dispatch. This is an exact tested target, not a claim about
other Umans models or clients.

---

## Local tiny-idp control plane

`deploy/docker-compose.yaml` is the Phase 1 production-shaped local topology.
It replaces the old Keycloak realm with tiny-idp `v0.0.5` (commit
`486a3e3108f3eeda3d100f3db613aecc74f4d13d`), pinned by immutable multi-platform
OCI digest. Caddy terminates local TLS on `127.0.0.1:18443`; a root-only
one-shot issuer obtains a 30-day `idp.localhost`/`proxy.localhost` leaf from the
workstation's persistent `tinyidp-local-caddy-pki` authority, while the
long-running Caddy remains non-root. The public root is exported separately and
llm-proxy startup is gated on a CA-verifying tiny-idp `/readyz` probe.

The external `tinyidp-local-caddy-pki` volume must already exist and contain the
Caddy authority created by the tiny-idp shared local stack. It is deliberately
not owned by this Compose project, so `docker compose down -v` cannot rotate the
trusted workstation CA:

```bash
docker volume inspect tinyidp-local-caddy-pki
```

Create four untracked secret files (each must contain one value), then run:

```bash
export TINYIDP_BOOTSTRAP_PASSWORD_FILE=/absolute/path/owner-password.txt
export LLM_PROXY_BYOK_MASTER_KEY_FILE=/absolute/path/byok-master-key.txt
export LLM_PROXY_BYOK_SESSION_SECRET_FILE=/absolute/path/session-secret.txt
export TINYIDP_RESOURCE_CLIENT_SECRET_FILE=/absolute/path/resource-client-secret.txt
docker compose -f deploy/docker-compose.yaml up --build --wait
```

The long-running Caddy, tiny-idp, and llm-proxy containers run as non-root.
Root is limited to the short-lived volume initializer and certificate issuer;
the persistent authority is mounted read-only and its private keys never enter
a long-running BYOK container. Secrets are Docker
secret mounts and llm-proxy reads them through `--byok-*-file` options; do not
put them in Compose, environment variables, or command-line values. On POSIX,
secret files are opened without following symlinks and validated through the
opened descriptor with a 4096-byte bound. Local Docker Compose bind-mounts
file-backed secrets without remapping ownership; place all four files inside an
owner-only mode-`0700` directory and make the individual files readable by the
non-root container account (mode `0644` was used for local acceptance). The
private parent prevents host users from traversing to those files. Production
orchestrators should instead assign secret ownership/mode directly. Secret-file
input fails closed on unsupported non-POSIX platforms until equivalent native
protections exist.

The Compose bootstrap creates a single local owner account, a public browser
client (`llm-proxy-web`) with the exact `https://proxy.localhost:18443/auth/callback`
redirect, the public `llm-proxy-agent` device client, and the confidential
`llm-proxy-resource` introspection client. The resource client receives its
secret only from the operator-managed file, is restricted to the exact agent
audience, and has no authorization-code, refresh, device, or other token grant.

Browser OIDC uses PKCE S256, one-time server-side auth transactions,
issuer-aware identities, and opaque revocable server-side sessions. Logout
revokes local state before navigating through tiny-idp's validated end-session
endpoint. The local tiny-idp browser flow is usable after importing the persistent public root into
the workstation/browser trust store. Do not use `--byok-dev-user` as a
substitute for OIDC outside explicit development testing.

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

- Without `--byok-db`, the proxy retains its prototype behavior and profile YAML supplies provider keys. With BYOK enabled, `/v1/*` requires scoped capabilities and provider keys come from the encrypted per-user vault.
- **OpenAI Responses API** (`/v1/responses`) support is deferred.
- **Smoke testing through Pinocchio** — Pinocchio can be pointed at the proxy as an OpenAI-compatible provider by
  configuring a Pinocchio profile with the proxy's base URL and model slug. (See `examples/README.md` for details.)

---

## License

MIT — see `LICENSE`.
