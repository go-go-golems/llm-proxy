# Tasks

## TODO

- [x] Add tasks here

- [x] Phase 0: define DB schema (users, credentials, tokens, scopes, usage_ledger)
- [x] Phase 1: bearer-token auth middleware in llm-proxy with SQLite token store and model allowlist
- [x] Phase 1: CLI token minting (llm-proxy-server token mint) for testing without UI
- [x] Phase 2: encrypted per-user credential vault and per-request profile/credential resolution
- [x] Phase 2: usage ledger + budget/rate enforcement incl. SSE streaming usage accounting
- [ ] Phase 3: control-plane webapp — OIDC login (Keycloak compose), credential CRUD, token mint/revoke UI, usage dashboard
- [ ] Phase 4 (optional): delegated third-party website consent flow with audience-bound tokens
- [ ] Write tmux smoke-test playbooks per phase (byok-host style)
- [ ] Resolve open questions: single binary vs two services; token counting for streaming; profile-slug vs provider/model scopes; control-plane repo location
