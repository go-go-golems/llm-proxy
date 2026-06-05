#!/usr/bin/env python3
"""Compare Claude proxy behavior with chat.stream false vs true.

This script reads local Pinocchio credentials, writes a temporary Geppetto profile
YAML with two Claude profiles, and sends the same Chat Completions request to
both profiles through a running llm-proxy server.

It is designed to preserve the key debugging evidence for the Anthropic
`no response` issue: the current Claude engine calls the streaming Anthropic API
path regardless of profile stream setting, so a profile with `stream: false` can
produce a non-SSE response that the streaming parser ignores.
"""

from __future__ import annotations

import argparse
import json
import subprocess
from pathlib import Path

import yaml


def read_claude_key(source: Path) -> str:
    data = yaml.safe_load(source.read_text())
    return data["profiles"]["claude-base"]["inference_settings"]["api"]["api_keys"]["claude-api-key"]


def write_profiles(path: Path, api_key: str) -> None:
    profiles = {
        "slug": "default",
        "profiles": {
            "anthropic-stream-false": {
                "inference_settings": {
                    "chat": {
                        "api_type": "claude",
                        "engine": "claude-haiku-4-5",
                        "max_response_tokens": 4096,
                        "stream": False,
                    },
                    "api": {
                        "api_keys": {"claude-api-key": api_key},
                        "base_urls": {"claude-base-url": "https://api.anthropic.com"},
                    },
                    "client": {"timeout": 60},
                    "claude": {},
                },
            },
            "anthropic-stream-true": {
                "inference_settings": {
                    "chat": {
                        "api_type": "claude",
                        "engine": "claude-haiku-4-5",
                        "max_response_tokens": 4096,
                        "stream": True,
                    },
                    "api": {
                        "api_keys": {"claude-api-key": api_key},
                        "base_urls": {"claude-base-url": "https://api.anthropic.com"},
                    },
                    "client": {"timeout": 60},
                    "claude": {},
                },
            },
        },
    }
    path.write_text(yaml.safe_dump(profiles, sort_keys=False, allow_unicode=True, width=1000))


def redact_profiles(src: Path, dst: Path) -> None:
    data = yaml.safe_load(src.read_text())
    for profile in data.get("profiles", {}).values():
        keys = profile.get("inference_settings", {}).get("api", {}).get("api_keys", {})
        for key in list(keys):
            keys[key] = "<redacted>"
    dst.write_text(yaml.safe_dump(data, sort_keys=False, allow_unicode=True, width=1000))


def post_request(base_url: str, model: str, out_dir: Path) -> dict:
    request = {
        "model": model,
        "messages": [
            {"role": "user", "content": "Reply with exactly: anthropic stream flag ok"},
        ],
        "stream": False,
    }
    request_path = out_dir / f"claude-{model}-request.json"
    response_path = out_dir / f"claude-{model}-response.raw"
    request_path.write_text(json.dumps(request, indent=2) + "\n")
    proc = subprocess.run(
        [
            "curl",
            "-sS",
            "-w",
            "\nHTTP_STATUS:%{http_code}\n",
            f"{base_url.rstrip('/')}/v1/chat/completions",
            "-H",
            "Content-Type: application/json",
            "--data-binary",
            f"@{request_path}",
        ],
        capture_output=True,
        text=True,
        timeout=180,
        check=False,
    )
    response_path.write_text(proc.stdout)
    body, _, status = proc.stdout.partition("\nHTTP_STATUS:")
    parsed: dict
    try:
        parsed = json.loads(body)
    except Exception as exc:  # noqa: BLE001 - debugging script preserves parse failures.
        parsed = {"parse_error": str(exc), "body_preview": body[:500]}
    return {
        "model": model,
        "curl_returncode": proc.returncode,
        "http_status": status.strip() if status else "",
        "request_path": str(request_path),
        "response_path": str(response_path),
        "parsed": parsed,
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", default=str(Path.home() / ".config/pinocchio/profiles.yaml"))
    parser.add_argument("--profiles-output", default="/tmp/llm-proxy-claude-stream-flag-profiles.yaml")
    parser.add_argument("--base-url", default="http://127.0.0.1:18085")
    parser.add_argument("--artifacts", default="/tmp/llm-proxy-claude-stream-flag-artifacts")
    args = parser.parse_args()

    source = Path(args.source)
    profiles_output = Path(args.profiles_output)
    artifacts = Path(args.artifacts)
    artifacts.mkdir(parents=True, exist_ok=True)

    write_profiles(profiles_output, read_claude_key(source))
    redact_profiles(profiles_output, artifacts / "llm-proxy-claude-stream-flag-profiles.redacted.yaml")

    results = [
        post_request(args.base_url, "anthropic-stream-false", artifacts),
        post_request(args.base_url, "anthropic-stream-true", artifacts),
    ]
    summary_path = artifacts / "claude-stream-flag-summary.json"
    summary_path.write_text(json.dumps(results, indent=2) + "\n")
    print(json.dumps(results, indent=2))


if __name__ == "__main__":
    main()
