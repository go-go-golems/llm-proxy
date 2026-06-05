#!/usr/bin/env python3
"""Generate a temporary llm-proxy backend smoke-test profile file.

This script intentionally does not store credentials in the ticket. It reads the
operator's local Pinocchio profile file, extracts credentials at runtime, and
writes a temporary profile YAML for llm-proxy smoke tests.

Default output:
  /tmp/llm-proxy-backend-smoke-profiles.yaml
"""

from __future__ import annotations

import argparse
from pathlib import Path

import yaml


def require_profile(profiles: dict, slug: str) -> dict:
    try:
        return profiles[slug]
    except KeyError as exc:
        raise SystemExit(f"missing profile {slug!r} in source profile file") from exc


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument(
        "--source",
        default=str(Path.home() / ".config/pinocchio/profiles.yaml"),
        help="Pinocchio profile YAML to read credentials from",
    )
    parser.add_argument(
        "--output",
        default="/tmp/llm-proxy-backend-smoke-profiles.yaml",
        help="temporary Geppetto profile YAML to write",
    )
    args = parser.parse_args()

    source = Path(args.source)
    data = yaml.safe_load(source.read_text())
    profiles = data["profiles"]

    claude_base = require_profile(profiles, "claude-base")
    openai_responses_base = require_profile(profiles, "openai-responses-base")
    groq_base = require_profile(profiles, "groq-base")

    claude_key = claude_base["inference_settings"]["api"]["api_keys"]["claude-api-key"]
    openai_key = openai_responses_base["inference_settings"]["api"]["api_keys"]["openai-api-key"]
    groq_key = groq_base["inference_settings"]["api"]["api_keys"]["openai-api-key"]

    smoke = {
        "slug": "default",
        "profiles": {
            "anthropic-haiku-smoke": {
                "inference_settings": {
                    "chat": {
                        "api_type": "claude",
                        "engine": "claude-haiku-4-5",
                        "max_response_tokens": 4096,
                    },
                    "api": {
                        "api_keys": {"claude-api-key": claude_key},
                        "base_urls": {"claude-base-url": "https://api.anthropic.com"},
                    },
                    "client": {"timeout": 60},
                    "claude": {},
                },
            },
            "openai-responses-smoke": {
                "inference_settings": {
                    "chat": {
                        "api_type": "openai-responses",
                        "engine": "gpt-5-nano",
                        "max_response_tokens": 4096,
                    },
                    "api": {"api_keys": {"openai-api-key": openai_key}},
                    "client": {"timeout": 120},
                    "openai": {},
                },
            },
            "openai-chat-smoke": {
                "inference_settings": {
                    "chat": {
                        "api_type": "openai",
                        "engine": "openai/gpt-oss-20b",
                        "max_response_tokens": 4096,
                    },
                    "api": {
                        "api_keys": {"openai-api-key": groq_key},
                        "base_urls": {"openai-base-url": "https://api.groq.com/openai/v1"},
                    },
                    "client": {"timeout": 60},
                    "openai": {},
                },
            },
        },
    }

    output = Path(args.output)
    output.write_text(yaml.safe_dump(smoke, sort_keys=False, allow_unicode=True, width=1000))
    print(output)


if __name__ == "__main__":
    main()
