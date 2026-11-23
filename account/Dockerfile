# Build stage
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/account-service ./account/cmd/account

# Final stage: no shell, no package manager, runs as an unprivileged user
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/account-service /account-service

EXPOSE 50051 9090

USER nonroot:nonroot

ENTRYPOINT ["/account-service"]
