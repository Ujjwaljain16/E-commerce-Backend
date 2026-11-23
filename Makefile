.PHONY: help proto-gen test integration-test e2e coverage lint fmt build clean docker-up docker-down docker-logs monitoring-up k8s-render deps

SERVICES := account catalog order payment notification graphql

## help: Display available commands
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'

## proto-gen: Regenerate protobuf code with buf (needs protoc-gen-go and protoc-gen-go-grpc on PATH)
proto-gen:
	go run github.com/bufbuild/buf/cmd/buf@v1.47.2 generate

## test: Run unit tests (no Docker needed)
test:
	go test -short ./...

## integration-test: Run repository tests against throwaway Postgres containers (needs Docker)
integration-test:
	go test -count=1 ./account/... ./catalog/... ./order/...

## e2e: Run the end-to-end flow against a running stack (make docker-up first)
e2e:
	go test -count=1 -tags=e2e ./tests/e2e/...

## coverage: Unit test coverage report
coverage:
	go test -short -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out | tail -1

## lint: gofmt, vet and staticcheck
lint:
	gofmt -s -l $$(git ls-files '*.go')
	go vet ./...
	go run honnef.co/go/tools/cmd/staticcheck@2025.1.1 ./...

## fmt: Format code
fmt:
	gofmt -s -w $$(git ls-files '*.go')

## build: Build all service binaries into bin/
build:
	@for s in $(SERVICES); do go build -trimpath -o bin/$$s ./$$s/cmd/$$s || exit 1; done

## docker-up: Build and start the full stack
docker-up:
	docker compose up -d --build

## monitoring-up: Start the stack plus Prometheus (localhost:9190) and Grafana (localhost:3000)
monitoring-up:
	docker compose --profile monitoring up -d --build

## docker-down: Stop the stack (add -v to the command to also drop data volumes)
docker-down:
	docker compose --profile monitoring down

## docker-logs: Follow service logs
docker-logs:
	docker compose logs -f --tail=100

## k8s-render: Render the Kubernetes manifests
k8s-render:
	kubectl kustomize k8s

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html

## deps: Download dependencies
deps:
	go mod download
