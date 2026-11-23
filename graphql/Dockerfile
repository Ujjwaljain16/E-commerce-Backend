# Build context is the repository root:
#   docker build -f graphql/Dockerfile .
FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/graphql-gateway ./graphql/cmd/graphql

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/graphql-gateway /graphql-gateway

USER nonroot:nonroot
EXPOSE 8000

ENTRYPOINT ["/graphql-gateway"]
