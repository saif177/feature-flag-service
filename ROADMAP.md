# Roadmap

Incremental features, roughly in order. Pick the next unchecked item each session.

- [x] Day 1: In-memory flag store with Create/Get/Set/Delete/List + IsEnabled
- [x] Percentage-based rollouts (flag enabled for X% of evaluations, consistent per user ID)
- [x] Targeting rules (enable for specific user IDs / attributes, e.g. email domain)
- [x] Multi-environment support (dev/staging/prod have independent flag states)
- [x] Persistence: swap in-memory store for SQLite
- [x] HTTP API: CRUD endpoints for flags + an evaluation endpoint
- [x] API key auth for the HTTP API
- [x] Audit log (who changed what flag, when, old value -> new value)
- [x] Webhooks on flag change
- [x] Caching layer for evaluation reads + invalidation on change
- [x] Client SDK (simple Go package wrapping the HTTP API with local caching)
- [ ] Scheduled flag changes (enable/disable at a future timestamp)
- [ ] Flag dependencies (flag B only evaluates if flag A is enabled)
- [ ] Bulk import/export of flag configuration (JSON/YAML)
- [ ] Simple admin dashboard (list/toggle flags from a web UI)
- [ ] Metrics: evaluation counts per flag, exposed via a metrics endpoint
- [ ] Postgres backend option (swap-able storage interface)
- [ ] Rate limiting on the HTTP API
- [ ] Load test + README with benchmarks

## Done

- Day 1 (2026-10-22): scaffolded repo, in-memory flag store with CRUD + evaluation.
- Day 2 (2026-10-22): percentage-based rollouts — `Flag.Rollout` (default 100), `SetRollout`, and deterministic per-user `IsEnabledFor` using an FNV hash of flag key + user ID (stable, monotonic as the percentage grows). `IsEnabled` is now true only for full rollouts.
- Day 3 (2026-10-27): targeting rules — `Rule` (attribute + operator `equals`/`ends_with`/`starts_with`/`contains` + values), `AddRule`/`ClearRules`, and `Evaluate(key, Context{UserID, Attributes})`. A matching rule turns an enabled flag on regardless of rollout (e.g. email domain, user ID allow-list); otherwise the percentage rollout applies. `IsEnabledFor` now delegates to `Evaluate`.
- Day 4 (2026-10-29): multi-environment support — `Environments` registry holding an independent `Store` per environment (default `dev`/`staging`/`prod`, or custom names), `Env(name)`, `Names()`, and `Promote(key, from, to)` to copy a flag's enabled state, rollout and rules between environments (deep-copied so later edits don't leak).
- Day 5 (2026-11-03): SQLite persistence — `SQLiteStore` (`OpenSQLite(path)`, pure-Go `modernc.org/sqlite`) with the same API as the in-memory `Store`, rules stored as JSON, data surviving reopen. Added a shared `FlagStore` interface and extracted `evaluateFlag` so both backends evaluate identically.
- Day 6 (2026-11-10): HTTP API — `Server` (`NewServer(envs)` / `NewServerFunc` for any `FlagStore` backend) with per-environment routes under `/v1/envs/{env}/flags`: list, create, get, PATCH (enabled/rollout/rules, validated before applying), delete, and `POST .../{key}/evaluate`. JSON errors with 400/404/409 mapping; `cmd/server` runs it. Store now returns the shared `ErrAlreadyExists`.
- Day 7 (2026-11-12): API key auth — `Authenticator` (`NewAuthenticator`, `ParseAPIKeys`) with `read`/`write` scoped keys stored as SHA-256 digests and compared in constant time; `Middleware` accepts `Authorization: Bearer` or `X-API-Key`, returns 401 for missing/invalid keys and 403 when a read-only key attempts a mutation (evaluate counts as a read). `cmd/server` enables it via `FLAGS_API_KEYS=name:scope:key,...`.
- Day 8 (2026-11-17): audit log — `AuditLog` (bounded, thread-safe, newest-first `Query` by env/key/actor/limit) records every successful create/update/delete made through the API with actor, timestamp and old/new flag snapshots. Auth middleware now passes the API key name via context (`WithActor`/`ActorFrom`; `anonymous` when auth is off). New `GET /v1/envs/{env}/audit?key=&actor=&limit=` (read scope).
- Day 9 (2026-11-19): webhooks — `Webhooks` registry + async dispatcher (worker pool, bounded queue, exponential-backoff retries, optional HMAC-SHA256 `X-Flags-Signature`). Every audited create/update/delete POSTs a `flag.changed` payload to matching subscribers, filterable by env and event type. New `GET/POST /v1/envs/{env}/webhooks` and `DELETE .../webhooks/{id}` (secret never returned); URLs must be http(s).
- Day 10 (2026-11-24): caching layer — `CachedStore` wraps any `FlagStore` with a read-through cache for flag lookups (used by `Evaluate`/`IsEnabled*`), TTL-bounded, with per-key invalidation on every write through the wrapper, race-safe fills (generation counter), defensive copies, and hit/miss/invalidation `Stats()`. `cmd/server` wraps each environment with it (`-cache-ttl`, default 30s).
- Day 11 (2026-11-26): client SDK — new `client` package (`client.New(Options{BaseURL, Env, APIKey, TTL, StaleTTL})`) with `Enabled(ctx, key, EvalContext, default)` calling the evaluate endpoint. Results are cached per flag+user+attributes for a TTL (order-independent cache key), stale values are served when the service is unreachable (bounded by `StaleTTL`), a 404 is authoritative and returns the caller's default, plus `Invalidate`/`Flush`, `Stats()`, typed `StatusError`/`IsNotFound`, and Bearer API-key support. Tested end-to-end against the real server and auth middleware.
