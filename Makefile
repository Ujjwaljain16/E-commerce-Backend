.PHONY: help proto-gen test lint build clean docker-up docker-down coverage

## help: Display available commands
help:
	@echo "Available commands:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

## proto-gen: Generate protobuf code for all services
proto-gen:
	@echo "Generating protobuf files..."
	cd account && protoc --go_out=./pb --go-grpc_out=./pb account.proto
	cd catalog && protoc --go_out=./pb --go-grpc_out=./pb catalog.proto
	cd order && protoc --go_out=./pb --go-grpc_out=./pb order.proto
	cd payment && protoc --go_out=./pb --go-grpc_out=./pb payment.proto
	@echo "✅ Protobuf generation complete"

## test: Run all unit tests
test:
	@echo "Running unit tests..."
	go test -v -short ./...
	@echo "✅ Tests passed"

## coverage: Run tests with coverage report
coverage:
	@echo "Running tests with coverage..."
	go test -v -short -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -func=coverage.out
	go tool cover -html=coverage.out -o coverage.html
	@echo "✅ Coverage report: coverage.html"

## integration-test: Run integration tests
integration-test:
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/integration/...
	@echo "✅ Integration tests complete"

## lint: Run linters
lint:
	@echo "Running linters..."
	gofmt -s -l .
	golangci-lint run --timeout 5m
	@echo "✅ Linting complete"

## fmt: Format code
fmt:
	@echo "Formatting code..."
	gofmt -s -w .
	@echo "✅ Code formatted"

## build: Build all services
build:
	@echo "Building all services..."
	cd account/cmd/account && go build -o ../../../bin/account
	cd catalog/cmd/catalog && go build -o ../../../bin/catalog
	cd order/cmd/order && go build -o ../../../bin/order
	cd payment/cmd/payment && go build -o ../../../bin/payment
	cd notification/cmd/notification && go build -o ../../../bin/notification
	cd graphql && go build -o ../bin/graphql
	@echo "✅ Build complete"

## docker-up: Start all services with Docker Compose
docker-up:
	@echo "Starting services..."
	docker-compose up -d
	@echo "✅ Services started"

## docker-down: Stop all services
docker-down:
	@echo "Stopping services..."
	docker-compose down
	@echo "✅ Services stopped"

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	@echo "✅ Cleaned"

## deps: Download and tidy dependencies
deps:
	@echo "Downloading dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies updated"
