# feature-flag-service

A backend feature-flag / remote-config service, built incrementally as a daily practice project. Starts as a simple in-memory CRUD store and grows feature by feature — see [ROADMAP.md](ROADMAP.md) for what's done and what's next.

## Current features

- In-memory flag store: `Create`, `Get`, `Set`, `Delete`, `List`, `IsEnabled`
- Percentage-based rollouts: `SetRollout` + `IsEnabledFor(key, userID)` gives consistent per-user results
- Targeting rules: `AddRule` with `equals`/`ends_with`/`starts_with`/`contains` on user ID or arbitrary attributes (e.g. email domain), evaluated via `Evaluate(key, Context{...})`

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
```

## Run the demo

```
go run ./cmd/demo
```

## Test

```
go test ./...
```
