# feature-flag-service

A backend feature-flag / remote-config service, built incrementally as a daily practice project. Starts as a simple in-memory CRUD store and grows feature by feature — see [ROADMAP.md](ROADMAP.md) for what's done and what's next.

## Current features

- In-memory flag store: `Create`, `Get`, `Set`, `Delete`, `List`, `IsEnabled`
- Percentage-based rollouts: `SetRollout` + `IsEnabledFor(key, userID)` gives consistent per-user results
- Targeting rules: `AddRule` with `equals`/`ends_with`/`starts_with`/`contains` on user ID or arbitrary attributes (e.g. email domain), evaluated via `Evaluate(key, Context{...})`
- Multi-environment: `NewEnvironments()` gives independent dev/staging/prod stores; `Promote` copies a flag between them
- SQLite persistence: `OpenSQLite("flags.db")` returns a `SQLiteStore` with the same API as `Store`; both satisfy `FlagStore`
- HTTP API: `NewServer(envs)` exposes flag CRUD and an evaluate endpoint per environment (`go run ./cmd/server`)
- API key auth: scoped `read`/`write` keys via `Authenticator.Middleware` (Bearer or `X-API-Key`), enabled in the server with `FLAGS_API_KEYS`
- Audit log: every API change is recorded (actor, time, old/new state), queryable at `GET /v1/envs/{env}/audit`

## Usage

```go
store := flags.NewStore()
store.Create("new-checkout", false)
store.IsEnabled("new-checkout") // false

store.Set("new-checkout", true)
store.IsEnabled("new-checkout") // true

// Roll out to 25% of users, consistently per user ID
store.SetRollout("new-checkout", 25)
store.IsEnabledFor("new-checkout", "user-42")

// Always enable for a company email domain
store.AddRule("new-checkout", flags.Rule{Attribute: "email", Operator: flags.OpEndsWith, Values: []string{"@acme.com"}})
store.Evaluate("new-checkout", flags.Context{UserID: "u1", Attributes: map[string]string{"email": "a@acme.com"}}) // true

// Independent environments
envs := flags.NewEnvironments()
dev, _ := envs.Env(flags.EnvDev)
dev.Create("new-checkout", true)
envs.Promote("new-checkout", flags.EnvDev, flags.EnvProd)
```

## HTTP API

```
go run ./cmd/server -addr :8080
curl -X POST localhost:8080/v1/envs/dev/flags -d '{"key":"new-checkout","enabled":true,"rollout":25}'
curl -X POST localhost:8080/v1/envs/dev/flags/new-checkout/evaluate -d '{"user_id":"u1"}'
```

Routes: `GET/POST /v1/envs/{env}/flags`, `GET/PATCH/DELETE /v1/envs/{env}/flags/{key}`, `POST /v1/envs/{env}/flags/{key}/evaluate`, `GET /v1/envs/{env}/audit` (optional `key`, `actor`, `limit` filters; newest first).

### Auth

```
FLAGS_API_KEYS="ci:read:abc123,admin:write:s3cret" go run ./cmd/server
curl -H "Authorization: Bearer s3cret" localhost:8080/v1/envs/dev/flags
```

`read` keys can list/get/evaluate; `write` keys can also create/update/delete. Missing or invalid keys get 401, insufficient scope 403.

## Run the demo

```
go run ./cmd/demo
```

## Test

```
go test ./...
```
