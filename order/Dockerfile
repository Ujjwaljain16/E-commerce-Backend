# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the order service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o order-service ./order/cmd/order

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/order-service .

EXPOSE 50053

CMD ["./order-service"]
