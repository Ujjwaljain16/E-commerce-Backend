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

# Build the account service
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o account-service ./account/cmd/account

# Final stage
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /root/

# Copy binary from builder
COPY --from=builder /app/account-service .

EXPOSE 50051

CMD ["./account-service"]
