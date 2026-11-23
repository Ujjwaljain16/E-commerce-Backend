``WORK IN PROGRESS ``

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

Requirements: Go 1.26+, Docker.

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
mutation { createProduct(input: {name: "Grinder", priceCents: 4950, sku: "GR-1", stock: 10}) { id } }
```

```graphql
# Authorization: Bearer <user token>
mutation {
  createOrder(input: {
    items: [{productId: "<id>", quantity: 2}]
    shippingAddress: "221B Baker Street", billingAddress: "221B Baker Street", paymentMethod: "card"
  }) { id status totalAmountCents }
}
```

Poll `order(id: "...") { status }` to watch it go from `PENDING` to `CONFIRMED`. Use `paymentMethod: "card_declined"` to see the failure path. See [graphql/README.md](graphql/README.md) for the full API.

## Test

```bash
make test               # unit tests, no Docker
make integration-test   # repository tests against throwaway Postgres containers
make e2e                # full checkout saga against the running compose stack
make lint               # gofmt, vet, staticcheck
make load               # 20 users for 30s through the gateway (see below)
```

### Load test

`tests/load` mixes 70% product reads, 20% searches and 10% checkouts through the gateway. Measured on one laptop (i5-13450HX, 16 GB), with every service, Postgres, Kafka and Redis in Docker on the same machine as the load generator, 20 users for 30 s:

| operation | req/s | p50 | p95 | p99 | errors |
|---|---|---|---|---|---|
| product | 1,272 | 3.1 ms | 10.1 ms | 18.1 ms | 0 |
| searchProducts | 358 | 10.8 ms | 32.2 ms | 77.5 ms | 0 |
| createOrder | 177 | 47.3 ms | 99.9 ms | 156 ms | 0 |

That is ~1,800 req/s overall. All 5,320 orders were paid and confirmed within 12 s of the run ending. A run with the order service restarted mid-test also finished with 0 errors. The gateway limits each client IP, so run the stack with `RATE_LIMIT_RPS=100000 RATE_LIMIT_BURST=100000 docker compose up -d` first.

## Deploy

```bash
kubectl create namespace ecommerce
kubectl -n ecommerce create secret generic ecommerce-secrets --from-env-file=secrets.env   # see k8s/base/infrastructure/secrets.example.yaml
kubectl apply -k k8s
```

The manifests include probes, resource limits, restricted pod security, HPA/PDB for the gateway, catalog and order, an Ingress for the gateway and NetworkPolicies that keep backend services internal. The bundled Postgres, Kafka and Redis are single-node and meant for dev/staging.

### Run on minikube

The `k8s/overlays/local` overlay uses locally built `:dev` images and dev-only secrets:

```bash
minikube start -p ecommerce --driver=docker --memory=4096 --cpus=4
for s in account catalog order payment notification graphql; do docker build -t ecommerce-$s:dev -f $s/Dockerfile . && minikube -p ecommerce image load ecommerce-$s:dev; done
kubectl apply -k k8s/overlays/local
kubectl -n ecommerce wait --for=condition=Available deployment --all --timeout=5m
kubectl -n ecommerce port-forward svc/graphql-gateway 8000:8000   # forward svc/<name>-service 50051..50055 too, then: make e2e
```

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
