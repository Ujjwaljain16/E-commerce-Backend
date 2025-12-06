# Catalog Service

The Catalog Service provides product catalog management functionality for the e-commerce platform. It manages product information, pricing, inventory, and supports search and filtering capabilities.

## Features

- ✅ **Product Management**: Create, read, update, and delete products
- ✅ **Unique SKU**: Enforce unique Stock Keeping Units for each product
- ✅ **Inventory Tracking**: Track product stock levels
- ✅ **Product Search**: Case-insensitive search by product name
- ✅ **Category Filtering**: Filter products by category
- ✅ **Pagination**: Efficient pagination for product listings
- ✅ **Image Management**: Support for multiple product images
- ✅ **gRPC API**: Protocol Buffers-based service interface
- ✅ **Health Checks**: Built-in health check endpoint
- ✅ **Prometheus Metrics**: Automatic metrics collection

## Architecture

### Tech Stack
- **Language**: Go 1.24
- **Database**: PostgreSQL 16
- **RPC Framework**: gRPC
- **Serialization**: Protocol Buffers (proto3)
- **Metrics**: Prometheus
- **Testing**: go-sqlmock, testcontainers-go

### Project Structure
```
catalog/
├── catalog.proto              # Protocol Buffers schema
├── repository.go             # PostgreSQL data access layer
├── service.go                # gRPC service implementation
├── integration_test.go       # End-to-end tests with real DB
├── repository_test.go        # Repository unit tests with mocks
├── service_test.go           # Service unit tests with mocks
├── Dockerfile                # Container image definition
├── cmd/
│   └── catalog/
│       └── main.go           # Application entry point
├── docs/
│   ├── DATABASE_SCHEMA.md    # Database schema documentation
│   └── PROTO_SCHEMA.md       # gRPC API documentation
├── migrations/
│   ├── 001_create_products_table.up.sql
│   └── 001_create_products_table.down.sql
└── pb/
    ├── catalog.pb.go         # Generated Protobuf code
    └── catalog_grpc.pb.go    # Generated gRPC code
```

## Database Schema

### Products Table
```sql
CREATE TABLE products (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price DECIMAL(10, 2) NOT NULL CHECK (price >= 0),
    sku VARCHAR(100) UNIQUE NOT NULL,
    stock INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
    images TEXT[],
    category VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

**Indexes:**
- `idx_products_sku` (unique) - Fast product lookup by SKU
- `idx_products_category` - Category filtering
- `idx_products_name` - Product name search

See [DATABASE_SCHEMA.md](./docs/DATABASE_SCHEMA.md) for complete documentation.

## gRPC API

### Service Methods

| Method | Description |
|--------|-------------|
| `CreateProduct` | Create a new product |
| `GetProduct` | Get product by ID |
| `ListProducts` | List products with pagination and filtering |
| `UpdateProduct` | Update existing product |
| `DeleteProduct` | Delete product by ID |
| `SearchProducts` | Search products by name |

See [PROTO_SCHEMA.md](./docs/PROTO_SCHEMA.md) for complete API documentation.

## Getting Started

### Prerequisites
- Go 1.24+
- PostgreSQL 16
- Protocol Buffers compiler (`protoc`)
- Docker (optional, for containerized deployment)

### Installation

1. **Install dependencies:**
```bash
go mod download
```

2. **Generate Protobuf code:**
```bash
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    catalog/catalog.proto
```

3. **Set up database:**
```bash
# Create database
createdb ecommerce

# Run migrations
psql ecommerce < catalog/migrations/001_create_products_table.up.sql
```

### Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5433/ecommerce?sslmode=disable` | PostgreSQL connection string |
| `PORT` | `50052` | gRPC server port |
| `METRICS_PORT` | `9091` | Prometheus metrics port |

### Running the Service

**Local development:**
```bash
go run ./catalog/cmd/catalog
```

**Using Docker Compose:**
```bash
docker-compose up catalog-service
```

**Build binary:**
```bash
go build -o catalog-service ./catalog/cmd/catalog
./catalog-service
```

## Testing

### Unit Tests
```bash
# Repository tests (with sqlmock)
go test ./catalog -v -run TestCreate
go test ./catalog -v -run TestGet
go test ./catalog -v -run TestList
go test ./catalog -v -run TestUpdate
go test ./catalog -v -run TestDelete
go test ./catalog -v -run TestSearch

# Service tests (with mock repository)
go test ./catalog -v -run TestCreateProduct
go test ./catalog -v -run TestGetProduct
go test ./catalog -v -run TestListProducts
go test ./catalog -v -run TestUpdateProduct
go test ./catalog -v -run TestDeleteProduct
go test ./catalog -v -run TestSearchProducts
```

### Integration Tests
```bash
# Requires Docker for testcontainers
go test ./catalog -v -run TestIntegration
```

### All Tests
```bash
go test ./catalog -v
```

**Test Coverage:**
- Repository: 12 tests
- Service: 18 tests
- Integration: 9 tests
- **Total: 39 tests**

## API Examples

### Create Product
```bash
grpcurl -plaintext -d '{
  "name": "Laptop",
  "description": "High-performance laptop",
  "price": 1299.99,
  "sku": "LAPTOP-001",
  "stock": 50,
  "images": ["https://example.com/img1.jpg"],
  "category": "Electronics"
}' localhost:50052 catalog.CatalogService/CreateProduct
```

### List Products
```bash
grpcurl -plaintext -d '{
  "page": 1,
  "page_size": 10,
  "category": "Electronics"
}' localhost:50052 catalog.CatalogService/ListProducts
```

### Search Products
```bash
grpcurl -plaintext -d '{
  "query": "laptop",
  "page": 1,
  "page_size": 10
}' localhost:50052 catalog.CatalogService/SearchProducts
```

## Business Rules

1. **Price Validation**: Price must be greater than 0
2. **Stock Validation**: Stock must be >= 0
3. **SKU Uniqueness**: Each product must have a unique SKU
4. **SKU Immutability**: SKU cannot be changed after product creation
5. **Name Requirement**: Product name is required and cannot be empty

## Monitoring

### Health Check
```bash
grpcurl -plaintext localhost:50052 grpc.health.v1.Health/Check
```

### Prometheus Metrics
Available at `http://localhost:9091/metrics`

**Key Metrics:**
- `grpc_server_handled_total` - Total RPC requests handled
- `grpc_server_handling_seconds` - Request duration histogram
- `grpc_server_msg_received_total` - Messages received
- `grpc_server_msg_sent_total` - Messages sent

## Development

### Code Organization
- **repository.go**: Database operations with PostgreSQL
- **service.go**: Business logic and gRPC handler implementation
- **main.go**: Server setup, health checks, metrics
- ***_test.go**: Unit and integration tests

### Adding New Features
1. Update `catalog.proto` with new RPC methods/messages
2. Regenerate code: `protoc --go_out=. --go-grpc_out=. catalog/catalog.proto`
3. Implement in `service.go` with validation
4. Add repository methods if needed in `repository.go`
5. Write tests in `*_test.go`
6. Update documentation

## Deployment

### Docker
```bash
# Build image
docker build -f catalog/Dockerfile -t catalog-service:latest .

# Run container
docker run -p 50052:50052 -p 9091:9091 \
  -e DATABASE_URL=postgres://postgres:postgres@host.docker.internal:5432/ecommerce \
  catalog-service:latest
```

### Docker Compose
```yaml
catalog-service:
  build:
    context: .
    dockerfile: catalog/Dockerfile
  environment:
    DATABASE_URL: postgres://postgres:postgres@postgres:5432/ecommerce?sslmode=disable
    PORT: 50052
    METRICS_PORT: 9091
  ports:
    - "50052:50052"
    - "9091:9091"
  depends_on:
    postgres:
      condition: service_healthy
```

## Performance Considerations

1. **Indexes**: Three indexes optimize common queries (SKU, category, name)
2. **Pagination**: LIMIT/OFFSET prevents loading entire datasets
3. **Connection Pooling**: Database connection pool configured in main.go
4. **Array Storage**: PostgreSQL TEXT[] efficient for image URLs (< 100 items)

## Security

1. **SQL Injection**: Parameterized queries prevent SQL injection
2. **Input Validation**: All inputs validated before database operations
3. **Price Constraints**: Database CHECK constraint prevents negative prices
4. **Stock Constraints**: Database CHECK constraint prevents negative stock

## Contributing

1. Follow the established pattern: Proto → Repository → Tests → Service → Tests
2. Write tests for all new features
3. Update documentation (PROTO_SCHEMA.md, DATABASE_SCHEMA.md)
4. Use small, focused commits
5. Run all tests before committing

## License

See LICENSE file in project root.
