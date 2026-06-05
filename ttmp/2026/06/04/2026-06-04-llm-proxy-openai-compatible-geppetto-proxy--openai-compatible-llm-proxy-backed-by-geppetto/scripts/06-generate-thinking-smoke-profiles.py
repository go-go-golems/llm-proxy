#!/usr/bin/env python3
"""Generate temporary live-provider profiles for reasoning/thinking smoke tests.

The output profile file contains local API keys and must stay in /tmp. A redacted
copy is written to the ticket artifacts directory for review.
"""

from __future__ import annotations

import argparse
from pathlib import Path

import yaml


TICKET_ARTIFACTS = Path(
    "ttmp/2026/06/04/2026-06-04-llm-proxy-openai-compatible-geppetto-proxy--openai-compatible-llm-proxy-backed-by-geppetto/scripts/artifacts"
)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--source", default=str(Path.home() / ".config/pinocchio/profiles.yaml"))
    parser.add_argument("--output", default="/tmp/llm-proxy-thinking-smoke-profiles.yaml")
    parser.add_argument("--redacted-output", default=str(TICKET_ARTIFACTS / "llm-proxy-thinking-smoke-profiles.redacted.yaml"))
    args = parser.parse_args()

    source = Path(args.source)
    data = yaml.safe_load(source.read_text())
    profiles = data["profiles"]

    claude_key = profiles["claude-base"]["inference_settings"]["api"]["api_keys"]["claude-api-key"]
    openai_key = profiles["openai-responses-base"]["inference_settings"]["api"]["api_keys"]["openai-api-key"]

    smoke = {
        "slug": "default",
        "profiles": {
            "claude-thinking-sonnet-smoke": {
                "inference_settings": {
                    "chat": {
                        "api_type": "claude",
                        "engine": "claude-sonnet-4-6",
                        "max_response_tokens": 4096,
                        # Intentionally false: Geppetto Claude RunInference should force streaming.
                        "stream": False,
                    },
                    "inference": {
                        "thinking_budget": 1024,
                    },
                    "api": {
                        "api_keys": {"claude-api-key": claude_key},
                        "base_urls": {"claude-base-url": "https://api.anthropic.com"},
                    },
                    "client": {"timeout": 120},
                    "claude": {},
                },
            },
            "openai-responses-thinking-smoke": {
                "inference_settings": {
                    "chat": {
                        "api_type": "openai-responses",
                        "engine": "gpt-5-nano",
                        "max_response_tokens": 8192,
                    },
                    "inference": {
                        "reasoning_effort": "low",
                        "reasoning_summary": "auto",
                    },
                    "api": {"api_keys": {"openai-api-key": openai_key}},
                    "client": {"timeout": 180},
                    "openai": {
                        "reasoning_effort": "low",
                        "reasoning_summary": "auto",
                    },
                    "model_info": {"reasoning": True},
                },
            },
        },
    }

    output = Path(args.output)
    output.write_text(yaml.safe_dump(smoke, sort_keys=False, allow_unicode=True, width=1000))
    print(output)

    redacted = yaml.safe_load(yaml.safe_dump(smoke))
    for profile in redacted.get("profiles", {}).values():
        keys = profile.get("inference_settings", {}).get("api", {}).get("api_keys", {})
        for key in list(keys):
            keys[key] = "<redacted>"
    redacted_output = Path(args.redacted_output)
    redacted_output.parent.mkdir(parents=True, exist_ok=True)
    redacted_output.write_text(yaml.safe_dump(redacted, sort_keys=False, allow_unicode=True, width=1000))
    print(redacted_output)


if __name__ == "__main__":
    main()
