# Build from the repository root: docker build -f payment/Dockerfile .
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/payment-service ./payment/cmd/payment

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/payment-service /payment-service

EXPOSE 50054 9093

USER nonroot:nonroot

ENTRYPOINT ["/payment-service"]
