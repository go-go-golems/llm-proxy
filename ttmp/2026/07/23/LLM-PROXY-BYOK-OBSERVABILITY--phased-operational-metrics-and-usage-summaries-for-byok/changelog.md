# Changelog

## 2026-07-23

- Initial workspace created


## 2026-07-23

Created evidence-backed phased observability design: owner usage-summary API/UI MVPs first, then loopback metering-health metrics, bounded inference metrics, and optional OIDC/device/dashboard phases

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/meter/health.go — Existing safe first metrics source
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/store/sqlite/store.go — Existing durable accounting and summary query source


## 2026-07-23

Validated the ticket and uploaded the index, 1,268-line intern design guide, 267-line diary, and phased tasks as one reMarkable bundle with ToC

## 2026-07-23

Addressed PR #8 review: split Phase 4 metrics to a reviewed 134-series ceiling, moved timed completion observations to route-aware runtime call sites, and required exact-route instrumentation of every middleware rejection return

### Related Files

- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/byok/authmw/middleware.go — Shows broad /v1 protection, incomplete audit-helper coverage, and need for exact inference classification
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/runtime/chat_service.go — Provides chat route and provider execution duration at completion call sites
- /home/manuel/code/wesen/go-go-golems/llm-proxy/pkg/runtime/completion_service.go — Provides legacy completion route and provider execution duration
