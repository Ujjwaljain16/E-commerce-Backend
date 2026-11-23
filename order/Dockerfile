# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/order-service ./order/cmd/order

# Final stage: no shell, no package manager, runs as an unprivileged user
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/order-service /order-service

EXPOSE 50053 9092

USER nonroot:nonroot

ENTRYPOINT ["/order-service"]
