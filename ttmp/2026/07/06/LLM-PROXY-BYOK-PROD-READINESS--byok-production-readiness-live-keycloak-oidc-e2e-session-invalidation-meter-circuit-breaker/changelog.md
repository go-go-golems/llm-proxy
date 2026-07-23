# Changelog

## 2026-07-06

- Initial workspace created


## 2026-07-22

Completed meter circuit-breaker tasks through LLM-PROXY-BYOK-TINYIDP Phase 0: persistent failures open immediately, transient busy/locked failures use threshold 3, recovery cooldown defaults to 5s, one committed write probe gates recovery, /readyz reflects state, and tests prove zero provider dispatch while open.

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/authmw/middleware.go — 503 fail-closed enforcement before dispatch
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/meter/health.go — Meter health state machine and recovery

