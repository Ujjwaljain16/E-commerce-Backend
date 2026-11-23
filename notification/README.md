# Notification Service

Emails customers when their orders are created, confirmed or cancelled, and keeps a history of what was sent.

## Features

- Consumes order events and sends one email per event (deduplicated by `event_id`)
- Looks up the recipient through the account service `GetProfile` RPC
- Subjects and bodies rendered from `text/template` per event type
- `LogSender` by default (logs the email as JSON); `SMTPSender` (STARTTLS + PLAIN auth) when `SMTP_HOST` is set
- Failed deliveries are stored as `FAILED` and retried; a retry updates the same row instead of adding a new one
- Schema migrations applied at startup
- gRPC health + reflection, Prometheus metrics, `/healthz`, panic recovery, graceful shutdown

## Layout

```
notification/
├── notification.proto   # gRPC contract
├── consumer.go          # order event handler
├── templates.go         # email templates
├── sender.go            # LogSender and SMTPSender
├── service.go           # ListNotifications
├── repository.go        # PostgreSQL access
├── migrations/          # embedded SQL migrations
└── cmd/notification/    # entry point
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable` | PostgreSQL connection string |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |
| `ACCOUNT_SERVICE_ADDR` | `localhost:50051` | Account service gRPC address |
| `PORT` | `50055` | gRPC port |
| `METRICS_PORT` | `9094` | HTTP port for `/metrics` and `/healthz` |
| `SMTP_HOST` | _(unset)_ | Enables SMTP delivery when set |
| `SMTP_PORT` | `587` | SMTP port (STARTTLS is used when the server offers it) |
| `SMTP_USERNAME` / `SMTP_PASSWORD` | _(unset)_ | PLAIN auth credentials, optional |
| `SMTP_FROM` | _(unset)_ | Sender address, required with `SMTP_HOST` |

Migrations are tracked in `notification_schema_migrations`.

## Events

Consumer group `notification-service`:

| Topic | Email |
|-------|-------|
| `order.created` | "We received your order", with total and items |
| `order.confirmed` | "Your order is confirmed", with total |
| `order.cancelled` | "Your order was cancelled", with the reason |

Malformed events and users the account service does not know are skipped. Account service outages and send failures are retried and then sent to `<topic>.dlq`. The service produces no events.

## gRPC API

| Method | Description |
|--------|-------------|
| `ListNotifications` | A user's notifications, newest first (`page_size` 1–100, default 10) |

## Running

```bash
# Locally (needs PostgreSQL, Kafka and the account service)
go run ./notification/cmd/notification

# Docker (build context is the repo root)
docker build -f notification/Dockerfile -t notification-service .

# Smoke test
grpcurl -plaintext localhost:50055 grpc.health.v1.Health/Check
curl localhost:9094/healthz
```

## Testing

```bash
go test -short ./notification/...
```

Unit tests cover dedupe, account lookup errors, send-failure retries, template rendering, header-injection safety and SMTP delivery against an in-process fake server; repository tests use sqlmock.
