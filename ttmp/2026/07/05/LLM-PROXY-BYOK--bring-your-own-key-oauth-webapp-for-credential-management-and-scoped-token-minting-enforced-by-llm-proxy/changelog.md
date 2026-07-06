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

