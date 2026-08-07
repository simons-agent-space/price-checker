# price-checker

Self-hosted price-monitoring service. Status: PR #2 — database + store layer shipped.

## What works now

- `GET /healthz` — pings the DB, returns `{"status":"ok"}` (503 if DB unreachable)
- `internal/store` package: typed CRUD over `searches` and `products` tables
- Migrations run on startup via `goose` + `//go:embed`
- SQLite at `file:$DATABASE_URL` with WAL mode, busy timeout, FK enforcement

## End goal

OpenClaw creates product searches; the service polls URLs, tracks price history, detects good prices, and sends Telegram notifications. See the design document for the full architecture.

## Local development

```sh
export DATABASE_URL="file:price-checker.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
go run ./cmd/price-checker
curl localhost:3000/healthz
```

Tests: `go test ./...` (uses temp SQLite DBs, no external services).

## Deployment

DB file lives at `/data/price-checker.db` inside the container, backed by the `price-checker-data` named volume. Push to `main` → CI builds and (after Simon provisions via `sudo provision-compose-service`) deploys to the VPS via Compose on `127.0.0.1:${APP_HOST_PORT}`.
