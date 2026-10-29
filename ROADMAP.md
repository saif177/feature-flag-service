# Roadmap

Incremental features, roughly in order. Pick the next unchecked item each session.

- [x] Day 1: In-memory flag store with Create/Get/Set/Delete/List + IsEnabled
- [x] Percentage-based rollouts (flag enabled for X% of evaluations, consistent per user ID)
- [x] Targeting rules (enable for specific user IDs / attributes, e.g. email domain)
- [x] Multi-environment support (dev/staging/prod have independent flag states)
- [ ] Persistence: swap in-memory store for SQLite
- [ ] HTTP API: CRUD endpoints for flags + an evaluation endpoint
- [ ] API key auth for the HTTP API
- [ ] Audit log (who changed what flag, when, old value -> new value)
- [ ] Webhooks on flag change
- [ ] Caching layer for evaluation reads + invalidation on change
- [ ] Client SDK (simple Go package wrapping the HTTP API with local caching)
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
