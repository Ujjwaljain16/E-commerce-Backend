# Payment Service

Charges orders and keeps their payment history. Payments are normally driven by `order.created` events; the gRPC API exposes lookups, manual charges and refunds.

## Features

- Idempotent charging: one payment per order, retries with the same idempotency key (defaults to the order ID) return the original payment
- Transactional outbox: status changes and their `payment.*` events are committed together, then relayed to Kafka
- Refunds for succeeded payments, triggered by `RefundPayment` or `order.cancelled`
- An order cancelled before it was charged gets a `CANCELLED` payment, so a late `order.created` is ignored
- Pluggable `Processor`; ships with `MockProcessor` (declines method `card_declined` and amounts above 1,000,000 cents, i.e. $10,000)
- Schema migrations applied at startup
- gRPC health + reflection, Prometheus metrics, `/healthz`, panic recovery, graceful shutdown

## Layout

```
payment/
├── payment.proto        # gRPC contract
├── service.go           # business rules and gRPC handlers
├── consumer.go          # order event handler
├── processor.go         # Processor interface and MockProcessor
├── repository.go        # PostgreSQL access + outbox writes
├── migrations/          # embedded SQL migrations
└── cmd/payment/         # entry point
```

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/ecommerce?sslmode=disable` | PostgreSQL connection string |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker list |
| `PORT` | `50054` | gRPC port |
| `METRICS_PORT` | `9093` | HTTP port for `/metrics` and `/healthz` |

Migrations are tracked in `payment_schema_migrations`.

## Events

| Direction | Topic | Notes |
|-----------|-------|-------|
| Consumed | `order.created` | Charges the order (consumer group `payment-service`) |
| Consumed | `order.cancelled` | Refunds a succeeded payment, or records a `CANCELLED` payment if none exists |
| Produced | `payment.succeeded` | Keyed by order ID |
| Produced | `payment.failed` | Includes the decline reason |
| Produced | `payment.refunded` | Includes the refund reason |

Malformed or invalid messages are skipped; transient failures are retried and then sent to `<topic>.dlq`.

## gRPC API

| Method | Description |
|--------|-------------|
| `ProcessPayment` | Charge an order. A declined charge returns the payment with status `FAILED`, not an error |
| `GetPayment` | Payment by ID |
| `GetPaymentByOrder` | Payment for an order |
| `ListPayments` | A user's payments, newest first (`page_size` 1–100, default 10) |
| `RefundPayment` | Refund a `SUCCEEDED` payment; anything else is `FAILED_PRECONDITION` |

## Running

```bash
# Locally (needs PostgreSQL and Kafka)
go run ./payment/cmd/payment

# Docker (build context is the repo root)
docker build -f payment/Dockerfile -t payment-service .

# Smoke test
grpcurl -plaintext localhost:50054 grpc.health.v1.Health/Check
curl localhost:9093/healthz
```

## Testing

```bash
go test -short ./payment/...
```

Unit tests cover idempotency, declines, refund rules, cancel-before-create and event decoding; repository tests use sqlmock.
