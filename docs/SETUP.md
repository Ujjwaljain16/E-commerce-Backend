# Development Setup

## Required

- **Go 1.26+** (`go.mod` pins the toolchain; older Go versions download it automatically).
- **Docker** with Compose, for the local stack, integration tests (testcontainers) and image builds.

## Protobuf code generation

Generated code is committed, so you only need this when you change a `.proto` file. The repo uses
[buf](https://buf.build) instead of `protoc`:

```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.10
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.0
make proto-gen          # runs buf generate (buf.yaml / buf.gen.yaml at the repo root)
```

CI regenerates the code and fails if the committed files are out of date.

## GraphQL code generation

After changing `graphql/schema.graphql`, run from `graphql/`:

```bash
GOFLAGS=-mod=mod go run github.com/99designs/gqlgen@v0.17.84 generate
```

then restore `go.mod`/`go.sum` if gqlgen touched them (its code generator needs modules the app doesn't).

## Linting

`make lint` runs gofmt, `go vet` and staticcheck, the same checks as CI.

## Optional

- **kubectl** and **minikube** to try the Kubernetes manifests (see "Run on minikube" in the README).
- **grpcurl** to call services directly (see [TESTING.md](TESTING.md)).

## Running

See the README: `docker compose up -d --build` starts everything, `make test`, `make integration-test`,
`make e2e` and `make load` cover testing.
