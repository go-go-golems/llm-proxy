# Tasks

## TODO

- [ ] Rate limiter: add periodic or lazy pruning of stale per-token windows in `pkg/byok/authmw/ratelimit.go`; test bounding the map size under synthetic token cardinality
- [ ] Rate limiter: document the fixed-window 2× boundary artifact as an accepted v1 property (code comment)
- [ ] Vault: add a `byok rekey` Glazed command (decrypt all secrets under old key, re-encrypt under new key, single transaction); test asserting all secrets decrypt under the new key and fail under the old
- [ ] Schema: add a `schema_version` table/pragma to `pkg/byok/store/sqlite/store.go` + a forward-only migration runner; document the migration contract; test against both backends
- [ ] Errors: export a shared OpenAI error-envelope writer from `pkg/server` (or a small `pkg/oaishape`); have `pkg/byok/authmw` call it instead of duplicating the envelope
- [ ] Dev-login: in `main.go`, refuse to start when `--byok-dev-user` is set and `--listen` resolves to a non-loopback address; loud error; test asserting the guard fires
- [ ] Username lookup: confirm `GetUserByUsername` oldest-match is acceptable for CLI convenience, or add a uniqueness constraint + normalized username index; document the decision in code
- [ ] Budgets: add `max_cost_usd` column to `tokens` + `total_cost_usd` to `token_counters`; design a price table (lean: config file with per-model rates); enforce cost budget in the same pre-check as token/request budgets (may split into a follow-on ticket if the price-table design needs a spike)
