# E-Commerce Microservices Platform

A distributed e-commerce backend in Go: six services talking gRPC, an event-driven checkout saga over Kafka, a GraphQL gateway, and the Docker/Kubernetes/CI setup to run it.

Design choices and their trade-offs are in [decisions.md](decisions.md).

## Architecture

```
                       clients
                          │  GraphQL (JWT, rate limit, complexity limit)
                  ┌───────▼────────┐
                  │ GraphQL gateway│
                  └───────┬────────┘
                          │ gRPC
   ┌──────────┬───────────┼───────────┬─────────────┐
┌──▼────┐ ┌───▼────┐ ┌────▼───┐ ┌─────▼───┐ ┌───────▼──────┐
│Account│ │Catalog │◀┤ Order  │ │ Payment │ │ Notification │
└──┬────┘ └─┬───┬──┘ └──┬─────┘ └──┬──────┘ └──────┬───────┘
   │        │   │ Redis │  outbox  │ outbox        │ SMTP
   │        │   │       │          │               │
   └────────┴───┼───────┴──────────┴───────────────┘
           PostgreSQL         Kafka (order.*, payment.* events)
```

**Checkout:** `createOrder` reserves stock and prices the items in Catalog (client prices are ignored), stores the order as `PENDING` and emits `order.created`. Payment charges it and emits `payment.succeeded` / `payment.failed`; Order then confirms or cancels. A cancellation (customer, admin or failed payment) emits `order.cancelled`, which makes Catalog put the stock back and Payment refund or void the charge. Notification emails the customer at each step. Events go through a transactional outbox and every consumer is idempotent.

| Service | gRPC | Metrics / health | Owns |
|---|---|---|---|
| account | 50051 | 9090 | users, bcrypt passwords, JWT access/refresh tokens, USER/ADMIN roles |
| catalog | 50052 | 9091 | products, stock reservations, full-text search, Redis cache |
| order | 50053 | 9092 | orders, status transitions, order events |
| payment | 50054 | 9093 | payments (mock provider), refunds, idempotency |
| notification | 50055 | 9094 | email templates, SMTP delivery, notification history |
| graphql | HTTP 8000 | `/metrics`, `/healthz`, `/readyz` | public API, auth, ownership checks |

## Run it

Requirements: Go 1.24+, Docker.

```bash
docker compose up -d --build                      # whole stack
docker compose --profile monitoring up -d         # + Prometheus :9190, Grafana :3000
```

- GraphQL playground: http://localhost:8000/playground
- Emails sent by the notification service: http://localhost:8025 (Mailpit)
- Dev admin: `admin@example.com` / `admin-password-123` (set by `ADMIN_EMAIL` / `ADMIN_PASSWORD`)

Each service applies its own migrations on startup. Compose secrets are for local use only.

### Example

```graphql
mutation { register(input: {email: "jo@example.com", password: "password123", name: "Jo"}) { accessToken user { id } } }
```

```graphql
# Authorization: Bearer <admin token>
mutation { createProduct(input: {name: "Grinder", price: 49.5, sku: "GR-1", stock: 10}) { id } }
```

```graphql
# Authorization: Bearer <user token>
mutation {
  createOrder(input: {
    items: [{productId: "<id>", quantity: 2}]
    shippingAddress: "221B Baker Street", billingAddress: "221B Baker Street", paymentMethod: "card"
  }) { id status totalAmount }
}
```

Poll `order(id: "...") { status }` to watch it go from `PENDING` to `CONFIRMED`. Use `paymentMethod: "card_declined"` to see the failure path. See [graphql/README.md](graphql/README.md) for the full API.

## Test

```bash
make test               # unit tests, no Docker
make integration-test   # repository tests against throwaway Postgres containers
make e2e                # full checkout saga against the running compose stack
make lint               # gofmt, vet, staticcheck
```

## Deploy

```bash
kubectl -n ecommerce create secret generic ecommerce-secrets --from-env-file=secrets.env   # see k8s/infrastructure/secrets.example.yaml
kubectl apply -k k8s
```

The manifests include probes, resource limits, restricted pod security, HPA/PDB for the gateway, catalog and order, an Ingress for the gateway and NetworkPolicies that keep backend services internal. The bundled Postgres, Kafka and Redis are single-node and meant for dev/staging.

CI (`.github/workflows/ci.yml`) runs lint, protobuf drift checks, race-enabled unit tests, integration tests, govulncheck, image builds with a Trivy scan, and manifest rendering. Images are pushed to GHCR from `main`.

## Layout

```
account/ catalog/ order/ payment/ notification/   service code, proto, migrations, Dockerfile
graphql/                 GraphQL gateway (gqlgen)
pkg/                     auth, cache, database, events, kafka, logger, metrics, outbox, server
monitoring/              Prometheus config + alerts, Grafana dashboard
k8s/                     kustomize manifests
tests/e2e/               end-to-end tests (build tag e2e)
```

Regenerate protobuf code with `make proto-gen` (uses buf; needs `protoc-gen-go` and `protoc-gen-go-grpc`).

## License

MIT, see [LICENSE](LICENSE).

**Author:** Ujjwal Jain ([@Ujjwaljain16](https://github.com/Ujjwaljain16))
