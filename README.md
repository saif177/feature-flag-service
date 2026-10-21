# feature-flag-service

A backend feature-flag / remote-config service, built incrementally as a daily practice project. Starts as a simple in-memory CRUD store and grows feature by feature — see [ROADMAP.md](ROADMAP.md) for what's done and what's next.

## Current features

- In-memory flag store: `Create`, `Get`, `Set`, `Delete`, `List`, `IsEnabled`

## Usage

```go
store := flags.NewStore()
store.Create("new-checkout", false)
store.IsEnabled("new-checkout") // false

store.Set("new-checkout", true)
store.IsEnabled("new-checkout") // true
```

## Run the demo

```
go run ./cmd/demo
```

## Test

```
go test ./...
```
