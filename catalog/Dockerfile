# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/catalog-service ./catalog/cmd/catalog

# Final stage: no shell, no package manager, runs as an unprivileged user
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/catalog-service /catalog-service

EXPOSE 50052 9091

USER nonroot:nonroot

ENTRYPOINT ["/catalog-service"]
