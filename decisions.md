# Decisions

Short record of the architectural choices in this repo and why. Newest last.

## 1. Choreographed saga over Kafka for checkout

**Decision.** Checkout is an event-driven saga, not a synchronous call chain.

```
gateway ──createOrder──▶ order ──ReserveStock (gRPC)──▶ catalog
                           │
                           └─ order.created ─▶ payment ─ payment.succeeded ─▶ order ─ order.confirmed ─▶ notification
                                                       └ payment.failed ───▶ order ─ order.cancelled ─▶ catalog (release stock)
                                                                                                    ├▶ payment (refund / mark cancelled)
                                                                                                    └▶ notification
```

**Why.** Payment can be slow or down without blocking order creation, every step can be retried
independently, and new consumers (notification) plug in without touching the order service.
Stock is reserved synchronously because the customer needs an immediate "out of stock" answer.

**Trade-off.** The order is `PENDING` for a moment after `createOrder` returns; clients poll `order(id)`.

## 2. Transactional outbox for every published event

**Decision.** Order and payment write events to an `*_outbox` table in the same transaction as the
state change; a relay (`pkg/outbox`) publishes them and marks them sent. `FOR UPDATE SKIP LOCKED`
lets multiple replicas run the relay.

**Why.** Writing to Postgres and Kafka separately loses events (crash between the two) or publishes
events for rolled-back changes. The outbox makes "state changed" and "event will be published" atomic.

**Trade-off.** Delivery is at-least-once, so every consumer is idempotent (see 3). Published rows are
kept for 7 days for debugging, then deleted in batches by the relay so the tables don't grow forever.

## 3. Idempotency everywhere instead of exactly-once

- Catalog: one `stock_reservations` row per order; `ReserveStock` returns the existing reservation on retry, `ReleaseStock` is a no-op once released.
  A reservation moves `RESERVED → COMMITTED` when `order.created` arrives. If none arrives within
  `RESERVATION_TTL` (15m) the order failed to save, so a sweeper returns the stock (`EXPIRED`). A late
  `order.created` re-reserves if stock allows; if not it is logged loudly and skipped (the order needs
  a human or a refund). A cancellation that overtakes `order.created` wins, because the two travel on
  different topics and Kafka only orders within a partition.
- Payment: `order_id` and `idempotency_key` are unique; a late `order.created` for an already-cancelled order finds a `CANCELLED` row and does nothing.
- Order: payment results only apply to `PENDING` orders (`UPDATE ... WHERE status = PENDING`).
- Notification: `event_id` is unique, so redelivered events never send a second email.

Consumers retry with backoff, then park poison messages on `<topic>.dlq` so one bad message cannot block a partition. Messages are keyed by order ID, so all events of one order stay ordered within a topic.

## 4. Server-side pricing

**Decision.** `CreateOrder` ignores client-supplied prices and names. Catalog returns the authoritative
name and unit price as part of `ReserveStock`, and the order stores those.

**Why.** The original implementation trusted the client's price, which let anyone buy anything for 0.01.

## 5. One Postgres database, schema owned per service

**Decision.** All services share one Postgres instance/database in dev, but each owns its tables and
runs its own embedded migrations at startup, tracked in its own `<service>_schema_migrations` table
(`pkg/database`). Services never read each other's tables.

**Why.** Keeps local and small deployments cheap while preserving the "database per service" boundary
logically; splitting into separate databases later is a connection-string change.

The migration runner is ~100 lines and uses a Postgres advisory lock so replicas starting together
don't race. `golang-migrate` was considered but brings a large dependency tree for what is "apply
these files in order once".

## 6. Authentication at the gateway, trusted internal network

**Decision.** The GraphQL gateway validates JWTs (HS256, `pkg/auth`) and passes the caller's user ID
to backend services. Services do not re-validate tokens. In Kubernetes, NetworkPolicies make the
gateway the only pod reachable from outside the namespace.

- Access and refresh tokens carry a `typ` claim and are not interchangeable (previously a refresh token worked as an access token for 7 days).
- Self-registration always creates `USER`; the first `ADMIN` is bootstrapped from `ADMIN_EMAIL`/`ADMIN_PASSWORD`.
- Ownership checks live in the gateway; another user's order returns `NOT_FOUND`, not `FORBIDDEN`, so IDs can't be probed.
- `JWT_SECRET` is required (32+ chars); there is no default secret anymore.

**Trade-off.** A compromised pod inside the namespace could call services directly. mTLS (service mesh) is the next step if that matters.

## 7. Postgres full-text search instead of Elasticsearch

**Decision.** Product search uses a generated, weighted `tsvector` column with a GIN index and
`websearch_to_tsquery`, ranked by `ts_rank`.

**Why.** Good relevance for a catalog this size, no extra cluster to run or keep in sync, and it fits a
16 GB dev machine. Revisit if search needs typo tolerance, facets or multi-language analysis.

## 8. Redis as a read-through cache only

**Decision.** Catalog caches `GetProduct` for 5 minutes and invalidates on update, delete, reservation
and release. Any Redis error falls through to Postgres. Redis runs without persistence and with LRU eviction.

**Why.** Product reads dominate; correctness never depends on the cache. Listing/search are not cached
because invalidation would be broad for little gain at this scale.

## 9. Mock payment provider behind an interface

**Decision.** `payment.Processor` has a mock implementation that declines `card_declined` and amounts
over $10,000 (1,000,000 cents). A real provider (e.g. Stripe) is a new implementation of the same interface, using our
idempotency key as the provider's idempotency key.

## 10. gRPC between services, GraphQL at the edge

Kept from the original design: protobuf contracts per service (generated with `buf`, checked for drift
in CI), GraphQL (gqlgen) for clients. The gateway adds per-IP rate limiting (in-memory token bucket per
replica), a query complexity limit, a 1 MB body limit and maps gRPC codes to GraphQL error codes without
leaking internal messages. Playground and introspection are off unless `PLAYGROUND_ENABLED=true`.

Rate limits are per replica; with N gateway replicas the effective limit is roughly N× higher. A shared
Redis limiter is the upgrade path if that matters.

## 11. Money as integer cents

**Decision.** Every amount is an `int64` number of minor units (cents; the only currency is USD) end to
end: proto fields are `*_cents` (`price_cents`, `unit_price_cents`, `subtotal_cents`,
`total_amount_cents`, `amount_cents`), Kafka events carry the same names, and Postgres stores `BIGINT`
columns with `CHECK (... >= 0)`. Order subtotals are `quantity * unit_price_cents` and the total is their
sum, both in integer math that refuses to overflow instead of wrapping. Notification emails format cents
as dollars (`$39.98`) with integer division.

GraphQL exposes these fields as a `Money` scalar backed by gqlgen's `Int64` (`priceCents`,
`subtotalCents`, `totalAmountCents`, `amountCents`). GraphQL's built-in `Int` is 32-bit, which caps out
at about $21M and would make large order totals unrepresentable; a 64-bit scalar avoids that without any
custom marshalling code. Fractional input such as `12.5` is rejected.

Catalog accepts `0 < price_cents <= 1e12` and returns `InvalidArgument` otherwise.

**Why.** `double` cannot represent most decimal amounts exactly, so totals needed rounding hacks and
could drift from what was charged. Existing `DECIMAL` data was converted by new migrations
(`round(x * 100)::bigint`); wire compatibility was broken deliberately since nothing is deployed.

## 12. Stay on a supported Go release

**Decision.** The module targets Go 1.26 (`go 1.26.11`), and images build on `golang:1.26-alpine`.

**Why.** Go only patches the two newest releases. On Go 1.24 govulncheck reported reachable standard
library vulnerabilities (net/http, crypto/tls, crypto/x509) whose fixes exist only in 1.26.x, plus
vulnerable `golang.org/x/net` and `klauspost/compress` versions. CI runs govulncheck so this is caught
again when the next release ships.

## 13. Operations

- **Images:** multi-stage builds, distroless `nonroot` runtime, static binaries (`CGO_ENABLED=0`, `-trimpath`).
- **Health:** each service serves `/metrics` and `/healthz` (DB ping) on its metrics port; the gRPC health status follows the same check, and Kubernetes uses native gRPC readiness probes.
- **Shutdown:** SIGTERM drains in-flight RPCs (10s cap), stops consumers and the outbox relay, then closes the DB.
- **Observability:** Prometheus metrics for gRPC/HTTP rate, errors and latency, Kafka produced/consumed/dead-lettered, and cache hit ratio; alert rules and a Grafana dashboard live in `monitoring/`. Logs are structured JSON on stdout, ready for any log collector (ELK, Loki) without coupling services to one.
- **Kubernetes:** kustomize manifests with resource limits, restricted pod security, HPA + PDB on the hot path (gateway, catalog, order). The bundled Postgres/Kafka/Redis are single-node for dev/staging; production should use managed services.
- **CI:** gofmt, vet, staticcheck, protobuf drift, race-enabled unit tests, testcontainers integration tests, govulncheck, image build + Trivy scan, manifest rendering. Images are pushed to GHCR only from `main`.
