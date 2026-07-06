# Changelog

## 2026-07-05

- Initial workspace created


## 2026-07-05

Created LLM-PROXY-BYOK ticket. Verified that the 2026-04-17--byok-host workspace is the prior BYOK broker work (BYOK-BROKER, BYOK-BROKER-WEB-UI, BYOK-KEYCLOAK-STORAGE tickets with delegated-broker design and Go smoke prototypes, no production code). Analyzed llm-proxy (no auth/metering today) and wrote the architecture proposal: OAuth/OIDC control-plane webapp with encrypted credential vault and restricted token minting, llm-proxy as the enforcement data plane (token validation, model allowlist, budget metering, per-user credential resolution via Geppetto profiles). Added phased task list.

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/ttmp/2026/07/05/LLM-PROXY-BYOK--bring-your-own-key-oauth-webapp-for-credential-management-and-scoped-token-minting-enforced-by-llm-proxy/design-doc/01-byok-for-llm-proxy-prior-art-analysis-and-architecture-proposal.md — Primary analysis and architecture proposal


## 2026-07-05

Wrote the full intern guide (design-doc/02): threat model, llm-proxy internals with file/line anchors, byok-host prior-art inventory with gaps, target architecture (one binary, control+data plane, SQLite), full DDL with encrypted vault and usage ledger, token design (llmp_ prefix, SHA-256 hashed), enforcement pseudocode (middleware, VaultEngineProvider, metering from result.Usage), control-plane API reference, phased implementation plan, pitfalls, and acceptance test.

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/ttmp/2026/07/05/LLM-PROXY-BYOK--bring-your-own-key-oauth-webapp-for-credential-management-and-scoped-token-minting-enforced-by-llm-proxy/design-doc/02-intern-guide-byok-system-analysis-design-and-implementation.md — The intern guide


## 2026-07-05

Uploaded the intern guide + architecture proposal bundle to reMarkable at /ai/2026/07/05/LLM-PROXY-BYOK (LLM-PROXY-BYOK Intern Design Guide.pdf).


## 2026-07-05

Step 1 / Phase 0: BYOK store layer with SQLite and memory backends, conformance-tested (commit ba0fb4c).

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/store/sqlite/store.go — SQLite backend with DDL and transactional metering
- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/store/store.go — Store interfaces


## 2026-07-05

Step 2 / Phase 1: bearer-token enforcement middleware, scoped model listing, OpenAI-shaped auth errors, byok CLI (user/token), --byok-db wiring; live curl smoke passed (commit e6b3b1f).

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/cmd/llm-proxy-server/byok.go — BYOK CLI command group
- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/authmw/middleware.go — TokenAuth middleware


## 2026-07-05

Step 3 / Phase 2: AES-GCM vault, VaultEngineProvider key injection with scrubbing, UsageRecorder metering incl. streaming, credential CLI, in-process end-to-end test (commit 388cb6d).

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/engines/provider.go — Per-request credential injection
- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/integration_test.go — CI-runnable end-to-end test


## 2026-07-05

Step 4: byok CLI rewritten on Glazed (env via LLM_PROXY_* built-in source, GlazeCommand lists, WriterCommand mutations); glazed-lint + logcopter enforced in pre-commit; two glazed integration bugs documented (hyphenated env prefix, embedded-struct decode skip) (commits 1327bef, ff91e0b).

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/cmd/llm-proxy-server/cmds/byok/common.go — Glazed group plumbing and AppName rationale


## 2026-07-05

Step 5 / Phase 3: control-plane webapp — OIDC RP (Keycloak dev compose under deploy/), HMAC session cookies, JSON management API with same-origin checks, embedded Bootstrap dashboard; verified live incl. real-browser rendering (commit 6b71c01).

### Related Files

- /home/manuel/workspaces/2026-07-05/llm-proxy-byok/llm-proxy/pkg/byok/web/web.go — Control plane assembly and routes

