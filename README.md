# price-checker

Self-hosted price-monitoring service. Polls product URLs on a schedule, tracks price history, detects good deals, sends Telegram notifications, and serves a small web dashboard for the rest of it.

## What works (v1)

- **JSON API** — `POST/GET/DELETE /searches`, `GET /searches/{id}` (OpenClaw-facing contract)
- **Scheduler** — background loop, polls due searches, calls the checker
- **Checker** — fetches each product URL, parses the price, records the check, detects deals (rolling median, 20% threshold)
- **Telegram notifier** — alerts on detected deals; silently `Noop` when `TELEGRAM_BOT_TOKEN` or `TELEGRAM_CHAT_ID` is missing (with a warning when exactly one is set)
- **Web UI** — neo-brutalist HTML dashboard at `GET /`, `/searches/{id}`, `/products/{id}`, `/deals`; basic-auth-gated; disabled when `HTTP_BASIC_AUTH_*` is unset
- **Store** — typed CRUD over `searches`, `products`, `price_checks` tables; SQLite + WAL + FK enforcement; migrations on startup via `goose` + `//go:embed`
- **Health** — `GET /healthz` pings the DB; `CMD /price-checker -healthcheck` for the compose probe (distroless has no shell)

## Configuration

All optional. The service runs with sensible defaults; everything below turns features on or tunes intervals.

| Env var | Default | Effect |
|---|---|---|
| `DATABASE_URL` | `file:price-checker.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)` | SQLite connection string. The compose file pins this to `/data/price-checker.db` inside the container. |
| `HTTP_ADDR` | `:3000` | Listen address for both the JSON API and the web UI. |
| `SCHEDULER_INTERVAL` | `30s` | How often the scheduler checks for due searches. Anything `time.ParseDuration` accepts. |
| `HTTP_BASIC_AUTH_USER` + `HTTP_BASIC_AUTH_PASS` | unset | When both are set, the web UI requires basic auth. When either is unset, the web routes are not registered at all and only the JSON API is reachable. |
| `TELEGRAM_BOT_TOKEN` + `TELEGRAM_CHAT_ID` | unset | When both are set, detected deals are sent to the configured chat. When either is unset, notifications are silent (`Noop`); a warning is logged when exactly one is set. |

## Local development

```sh
export DATABASE_URL="file:price-checker.db?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
go run ./cmd/price-checker
curl localhost:3000/healthz
# With the web UI:
export HTTP_BASIC_AUTH_USER=admin HTTP_BASIC_AUTH_PASS=secret
go run ./cmd/price-checker
open http://localhost:3000/
```

Tests use temp-file SQLite DBs, no external services:

```sh
go test ./...
```

## End goal

OpenClaw creates product searches via the JSON API; the service polls URLs, tracks price history, detects good prices, sends Telegram notifications, and serves a web dashboard. Self-hosted Go service on the VPS, deliberately boring.

## Deployment

DB file lives at `/data/price-checker.db` inside the container, backed by a named volume. Push to `main` → CI builds and, after `sudo provision-compose-service` on the VPS, deploys via Compose on `127.0.0.1:${APP_HOST_PORT}`. Per the established merge-driven flow; Simon's merges are the deployment approval.
