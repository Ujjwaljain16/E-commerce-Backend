# Build from the repository root: docker build -f notification/Dockerfile .
FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/notification-service ./notification/cmd/notification

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/notification-service /notification-service

EXPOSE 50055 9094

USER nonroot:nonroot

ENTRYPOINT ["/notification-service"]
