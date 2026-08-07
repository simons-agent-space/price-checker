# price-checker

Self-hosted price-monitoring service. Status: placeholder scaffold.

## What works now

- `GET /healthz` — returns `{"status":"ok"}`. Single route used to validate the deployment pipeline.

## End goal

OpenClaw creates product searches; the service polls URLs, tracks price history, detects unusually good prices, and sends Telegram notifications. See the design document for the full architecture.

## Local development

```sh
go run ./cmd/price-checker
curl localhost:3000/healthz
```

## Deployment

Push to `main` → CI builds and (after Simon provisions via `sudo provision-compose-service`) deploys to the VPS via Compose on `127.0.0.1:${APP_HOST_PORT}`.
