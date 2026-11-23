# GraphQL Gateway

Public HTTP entry point for the E-commerce backend. It exposes one GraphQL API and translates each field into calls to the account, catalog, order, payment and notification gRPC services.

## Overview

- Single `/graphql` endpoint (GET for queries, POST for queries and mutations)
- JWT bearer auth, validated locally with the same secret the account service signs with
- Role checks (USER/ADMIN) and order ownership checks at the edge
- gRPC errors mapped to stable `extensions.code` values; internal details never reach clients
- Per-IP rate limiting, query complexity limit, 1 MB body limit, server timeouts
- `/healthz`, `/readyz` and Prometheus `/metrics`

## Architecture

```
graphql/
├── schema.graphql        # GraphQL schema (source of truth)
├── gqlgen.yml            # gqlgen config
├── graph/                # generated executor + models, resolvers, error mapping
├── server/               # config, gRPC clients, HTTP middleware, routes
├── cmd/graphql/          # main entry point
└── Dockerfile
```

Regenerate after editing the schema (from `graphql/`):

```bash
go run github.com/99designs/gqlgen generate
```

## Running Locally

```bash
JWT_SECRET=dev-secret-change-in-production PLAYGROUND_ENABLED=true go run ./graphql/cmd/graphql
# Playground: http://localhost:8000/playground
```

Docker (build context is the repo root):

```bash
docker build -f graphql/Dockerfile -t graphql-gateway .
docker run -p 8000:8000 -e JWT_SECRET=... -e ACCOUNT_SERVICE_ADDR=account-service:50051 graphql-gateway
```

## Environment Variables

| Variable | Default | Notes |
|---|---|---|
| `JWT_SECRET` | – | **Required.** Must match the account service. |
| `PORT` | `8000` | HTTP port |
| `ACCOUNT_SERVICE_ADDR` | `localhost:50051` | |
| `CATALOG_SERVICE_ADDR` | `localhost:50052` | |
| `ORDER_SERVICE_ADDR` | `localhost:50053` | |
| `PAYMENT_SERVICE_ADDR` | `localhost:50054` | |
| `NOTIFICATION_SERVICE_ADDR` | `localhost:50055` | |
| `GRPC_TIMEOUT` | `5s` | Per backend call |
| `RATE_LIMIT_RPS` | `20` | Requests per second per client IP |
| `RATE_LIMIT_BURST` | `40` | Bucket size per client IP |
| `TRUST_PROXY` | `false` | Use the last `X-Forwarded-For` hop as the client IP. Only enable behind a proxy you control. |
| `COMPLEXITY_LIMIT` | `200` | Max query complexity |
| `CORS_ALLOWED_ORIGINS` | empty | Comma-separated origins; empty sends no CORS headers |
| `PLAYGROUND_ENABLED` | `false` | Enables `/playground` and schema introspection |

## Authentication

Send the access token from `login`/`register`/`refreshToken`:

```
Authorization: Bearer <accessToken>
```

Access tokens last 15 minutes. `refreshToken` swaps a refresh token for a new pair and the old refresh token stops working; presenting a used one again signs out all of that user's sessions. `logout(refreshToken)` revokes it.

A missing, expired or invalid token does not fail the request; it just runs unauthenticated, and fields that need a user return `UNAUTHENTICATED`. The caller's identity always comes from the token, never from arguments. Admin-only fields return `FORBIDDEN` for regular users. Orders (and their payments) that belong to someone else return `NOT_FOUND`, exactly like orders that don't exist.

## Examples

Register and log in:

```graphql
mutation {
  register(input: { email: "alice@example.com", password: "s3cret-pass", name: "Alice" }) {
    accessToken
    refreshToken
    user { id email role }
  }
}

mutation {
  login(email: "alice@example.com", password: "s3cret-pass") { accessToken refreshToken }
}
```

Browse the catalog (no auth needed):

```graphql
query {
  products(page: 1, pageSize: 10, category: "books") {
    total
    items { id name priceCents stock }
  }
  searchProducts(query: "lamp") { items { id name } }
}
```

Money is always a `Money` scalar: an integer number of cents (`4950` is $49.50), 64-bit, never a float.

Place and inspect orders (auth required). Only product IDs and quantities are sent to the order service; clients cannot set prices:

```graphql
mutation {
  createOrder(input: {
    items: [{ productId: "p-123", quantity: 2 }]
    shippingAddress: "1 Main St"
    billingAddress: "1 Main St"
    paymentMethod: "card"
  }) {
    id status totalAmountCents
    items { productName quantity priceCents subtotalCents }
  }
}

query {
  me { id email name }
  myOrders(pageSize: 5) { total items { id status totalAmountCents } }
  paymentForOrder(orderId: "o-456") { status amountCents currency }
  myNotifications { items { subject status createdAt } }
}
```

Admin:

```graphql
mutation { updateOrderStatus(id: "o-456", status: SHIPPED) { id status } }
mutation { refundPayment(paymentId: "pay-789", reason: "damaged item") { id status } }
query { orders(status: PENDING, pageSize: 50) { total items { id userId } } }
```

## Errors

Every error carries `extensions.code`:

| Code | When |
|---|---|
| `BAD_USER_INPUT` | Invalid arguments (backend `InvalidArgument`, bad paging) |
| `UNAUTHENTICATED` | No valid token, or bad credentials |
| `FORBIDDEN` | Admin-only field |
| `NOT_FOUND` | Missing resource, or an order you don't own |
| `CONFLICT` | Already exists (e.g. email taken) |
| `FAILED_PRECONDITION` | Operation not allowed in the current state |
| `UNAVAILABLE` | Backend down or timed out |
| `INTERNAL` | Anything else; details are logged, not returned |
| `RATE_LIMITED` | HTTP 429, retry after a second |

## Limits

- Rate limit per client IP (token bucket), HTTP 429 with a JSON error body
- Query complexity capped by `COMPLEXITY_LIMIT`; `pageSize` capped at 100
- Request body capped at 1 MB (HTTP 413)
- Server timeouts: read header 5s, read 10s, write 30s, idle 120s
- No websockets, file uploads or batched operations

## Endpoints

| Path | Purpose |
|---|---|
| `/graphql` | GraphQL API |
| `/playground` | GraphQL playground (only with `PLAYGROUND_ENABLED=true`) |
| `/healthz` | Liveness, always 200 |
| `/readyz` | 200 unless a backend connection is failing |
| `/metrics` | Prometheus metrics |

## Testing

```bash
go test -short ./graphql/...
```

Resolver tests run against fake gRPC clients through the real gqlgen handler; middleware tests cover auth, rate limiting, body limits and CORS.
