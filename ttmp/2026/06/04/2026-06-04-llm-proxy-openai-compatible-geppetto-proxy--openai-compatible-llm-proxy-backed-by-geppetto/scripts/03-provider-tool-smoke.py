#!/usr/bin/env python3
import json
import subprocess
import sys
from pathlib import Path

PORT = 18082
providers = [
    ("openai-chat", "groq-oss-20b", "Paris"),
    ("anthropic", "sonnet", "Berlin"),
    ("openai-responses", "gpt-5-nano", "Rome"),
]

def request(model, city, stream=False):
    return {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": f"You must call the lookup_weather function exactly once for city {city}. Do not answer directly; return only a tool call.",
            }
        ],
        "tools": [
            {
                "type": "function",
                "function": {
                    "name": "lookup_weather",
                    "description": "Look up the current weather for a city.",
                    "parameters": {
                        "type": "object",
                        "properties": {
                            "city": {"type": "string", "description": "City name"}
                        },
                        "required": ["city"],
                        "additionalProperties": False,
                    },
                },
            }
        ],
        "tool_choice": "required",
        "stream": stream,
    }

results = []
for label, model, city in providers:
    path = Path(f"/tmp/tool-smoke-{label}.json")
    path.write_text(json.dumps(request(model, city), indent=2))
    cmd = [
        "curl", "-sS", "-w", "\nHTTP_STATUS:%{http_code}\n",
        f"http://127.0.0.1:{PORT}/v1/chat/completions",
        "-H", "Content-Type: application/json",
        "--data-binary", f"@{path}",
    ]
    print(f"\n=== {label} model={model} city={city} ===", flush=True)
    proc = subprocess.run(cmd, text=True, capture_output=True, timeout=240)
    raw = proc.stdout
    print(raw[:3000])
    body, _, status_part = raw.partition("\nHTTP_STATUS:")
    status = status_part.strip() if status_part else "?"
    summary = {"label": label, "model": model, "city": city, "curl_rc": proc.returncode, "http_status": status}
    try:
        d = json.loads(body)
        choice = (d.get("choices") or [{}])[0]
        msg = choice.get("message") or {}
        summary.update({
            "object": d.get("object"),
            "finish_reason": choice.get("finish_reason"),
            "content": msg.get("content"),
            "tool_calls": msg.get("tool_calls"),
            "error": d.get("error"),
        })
    except Exception as e:
        summary["parse_error"] = str(e)
        summary["body_preview"] = body[:500]
    results.append(summary)

Path("/tmp/tool-smoke-summary.json").write_text(json.dumps(results, indent=2))
print("\n=== SUMMARY ===")
print(json.dumps(results, indent=2))

failed=[]
for r in results:
    calls = r.get("tool_calls") or []
    ok = r.get("http_status") == "200" and r.get("finish_reason") == "tool_calls" and calls and calls[0].get("function",{}).get("name") == "lookup_weather"
    if not ok:
        failed.append(r["label"])
if failed:
    print("FAILED_LABELS=" + ",".join(failed), file=sys.stderr)
    sys.exit(1)
