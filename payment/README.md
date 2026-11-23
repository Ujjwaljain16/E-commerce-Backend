# Payment Service

Charges orders and keeps their payment history. Payments are normally driven by `order.created` events; the gRPC API exposes lookups, manual charges, refunds and Razorpay checkout completion.

Two providers, picked with `PAYMENT_PROVIDER`:

- `mock` (default): charges synchronously inside the `order.created` handler, as a stand-in for a card processor.
- `razorpay`: the customer pays in Razorpay Checkout on the client, so the payment waits in `PENDING` until it is confirmed, reported by webhook, or expires.

## Features

- Idempotent charging: one payment per order, retries with the same idempotency key (defaults to the order ID) return the original payment
- Transactional outbox: status changes and their `payment.*` events are committed together, then relayed to Kafka
- Refunds for succeeded payments, triggered by `RefundPayment` or `order.cancelled`
- An order cancelled before it was charged gets a `CANCELLED` payment, so a late `order.created` is ignored
- `MockProcessor` declines method `card_declined` and amounts above 1,000,000 cents ($10,000)
- Razorpay Checkout over plain `net/http` (no SDK): order creation, signature checks, webhooks, capture, refunds and an expiry sweeper
- Schema migrations applied at startup
- gRPC health + reflection, Prometheus metrics, `/healthz`, panic recovery, graceful shutdown

## Layout

```
payment/
├── payment.proto        # gRPC contract
├── service.go           # business rules and gRPC handlers
├── consumer.go          # order event handler
├── processor.go         # Processor interface and MockProcessor
├── razorpay.go          # Razorpay API client and signature checks
├── checkout.go          # Razorpay checkout flow: confirm, webhooks, expiry, late-capture refunds
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
| `PAYMENT_PROVIDER` | `mock` | `mock` or `razorpay` |
| `PAYMENT_TIMEOUT` | `30m` | How long a Razorpay checkout may stay unpaid before the payment fails |
| `RAZORPAY_KEY_ID` | – | Required for `razorpay` |
| `RAZORPAY_KEY_SECRET` | – | Required for `razorpay`; also signs Checkout responses |
| `RAZORPAY_WEBHOOK_SECRET` | – | Required for `razorpay`; the secret set on the dashboard webhook |

With `PAYMENT_PROVIDER=razorpay` the service refuses to start unless all three `RAZORPAY_*` values are set. Secrets are never logged.

Migrations are tracked in `payment_schema_migrations`.

## Events

| Direction | Topic | Notes |
|-----------|-------|-------|
| Consumed | `order.created` | Mock: charges the order. Razorpay: creates the Razorpay order and waits, no event yet (consumer group `payment-service`) |
| Consumed | `order.cancelled` | Refunds a succeeded payment, cancels a Razorpay checkout that is still waiting, or records a `CANCELLED` payment if none exists |
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
| `ConfirmPayment` | Complete a Razorpay checkout from `razorpay_payment_id` + `razorpay_signature`. Bad signature: `INVALID_ARGUMENT`, nothing changes. Not a Razorpay checkout, or Razorpay disabled: `FAILED_PRECONDITION` |
| `HandleProviderWebhook` | Apply a Razorpay webhook (raw body + `X-Razorpay-Signature`, forwarded by the gateway). Bad signature: `INVALID_ARGUMENT`. Events for unknown orders and event types we don't use are acknowledged and ignored |

## Razorpay checkout

```
order.created ──▶ PENDING (Razorpay order created, receipt = our order ID, no event)
                    │
                    ├─ ConfirmPayment (valid signature) ─┐
                    ├─ webhook payment.captured ─────────┼─▶ SUCCEEDED ──▶ payment.succeeded
                    ├─ webhook payment.authorized ───────┘   (authorized payments are captured first)
                    ├─ webhook payment.failed ──────────────▶ FAILED ──▶ payment.failed (provider's error description)
                    ├─ PAYMENT_TIMEOUT passes ──────────────▶ FAILED ──▶ payment.failed ("payment not completed in time")
                    └─ order.cancelled ─────────────────────▶ CANCELLED (no event)

SUCCEEDED ── order.cancelled / RefundPayment ──▶ REFUNDED ──▶ payment.refunded
FAILED / CANCELLED ── capture reported later ──▶ refunded through Razorpay, REFUNDED (no event)
```

- Every transition is `UPDATE ... WHERE status = <expected>` in the same transaction as its outbox event, so confirm, webhook, expiry and cancel can race safely and replays change nothing.
- `order.created` is idempotent: a payment that already has a Razorpay order is returned as is. If an earlier attempt died after creating the Razorpay order, it is found again by receipt instead of creating a second one. A Razorpay `400` (e.g. below the ₹1 minimum) fails the payment; other API errors are retried.
- `ConfirmPayment` verifies `HMAC-SHA256(razorpay_order_id + "|" + razorpay_payment_id, key_secret)` against the stored Razorpay order ID, then fetches the payment and checks order, amount and currency before marking it `SUCCEEDED`.
- Webhooks are verified with `HMAC-SHA256(raw body, webhook_secret)`. Razorpay may deliver them more than once and out of order; a late `payment.failed` for an earlier attempt does not undo a success.
- The expiry sweeper runs every minute in every replica. Before failing a payment it lists the Razorpay order's payments, so a capture whose webhook was lost completes the payment instead. If Razorpay is unreachable the payment stays `PENDING` until the next sweep.
- Money captured after the order was cancelled or the payment failed or expired is refunded automatically (`X-Refund-Idempotency` keyed by the Razorpay payment ID, so retries never pay out twice).
- Amounts are integer minor units end to end; Razorpay expects paise, so run the order service with `CURRENCY=INR`.

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

Unit tests cover idempotency, declines, refund rules, cancel-before-create and event decoding; repository tests use sqlmock. Razorpay tests run the real client against an `httptest` fake of the API: order creation and reuse, API errors and timeouts, signature test vectors, confirm guards, webhooks (captured, authorized, failed, bad signature, unknown events, replays, out of order), the expiry sweeper across replicas and late-capture refunds.
