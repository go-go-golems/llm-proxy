# Tasks

## TODO

- [ ] Stand up `deploy/docker-compose.yaml` (Keycloak 26.2 + realm import) and drive the full OIDC browser flow against the ported `pkg/byok/web/oidc.go` (dev-login disabled, login as alice, callback, session, credential CRUD, token mint, token used against `/v1/*`); fix any porting bugs
- [ ] Write `playbooks/01-live-keycloak-oidc-smoke.md` with the exact commands and expected output
- [ ] Verify the OIDC callback check order (state cookie → code exchange → ID-token signature → nonce) holds against the live IdP
- [ ] Session store: add a `sessions` table to the SQLite store (id, user_id, created_at, last_seen_at, expires_at, revoked); make the HMAC cookie carry a session id looked up server-side; reject revoked/expired sessions on every authed request
- [ ] Session store: add a session revocation endpoint (`POST /api/sessions/{id}/revoke`) and a session list endpoint; update tests
- [ ] Session store: decide and implement idle vs absolute timeout policy
- [ ] Meter circuit breaker: track consecutive ledger-write failures in `meter.Recorder` (or a health struct); trip a 503 data plane after a configurable threshold (`--byok-meter-failure-threshold`); reset on successful write; tests simulating persistent store failure
- [ ] Meter circuit breaker: decide default threshold and whether to distinguish transient `SQLITE_BUSY` from persistent (disk-full) failures
