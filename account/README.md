# Account Service

Microservice for user authentication and account management in the E-commerce backend.

## Overview

The Account service provides user registration, authentication, profile management, and role-based access control (RBAC) functionality. It uses gRPC for inter-service communication and PostgreSQL for data persistence.

## Features

- ✅ User registration with email and password
- ✅ JWT-based authentication (access + refresh tokens)
- ✅ User profile management (get, update)
- ✅ Password change functionality
- ✅ Soft account deletion
- ✅ Token verification and refresh
- ✅ Role-based access control (USER/ADMIN)
- ✅ Health check endpoint
- ✅ Prometheus metrics integration
- ✅ Comprehensive test coverage (77.6%)

## Tech Stack

- **Language**: Go 1.24+
- **Database**: PostgreSQL 16
- **Communication**: gRPC + Protocol Buffers
- **Authentication**: JWT (HS256)
- **Password Hashing**: bcrypt (cost factor 10)
- **Testing**: testcontainers-go for integration tests
- **Metrics**: Prometheus
- **Health Checks**: gRPC Health Protocol

## Architecture

```
account/
├── account.proto          # gRPC service definition
├── service.go             # Business logic implementation
├── repository.go          # Database access layer
├── server.go              # gRPC server setup
├── cmd/account/           # Main entry point
├── pb/                    # Generated protobuf code
├── migrations/            # Database migrations
├── docs/                  # Documentation
│   ├── DATABASE_SCHEMA.md # Database schema reference
│   └── PROTO_SCHEMA.md    # Protocol buffer reference
├── service_test.go        # Unit tests with mocks
└── integration_test.go    # Integration tests
```

## Quick Start

### Prerequisites

- Go 1.24 or higher
- PostgreSQL 16
- Docker (for integration tests)

### Environment Variables

```bash
# Database
DB_HOST=localhost
DB_PORT=5432
DB_USER=postgres
DB_PASSWORD=postgres
DB_NAME=account_db

# JWT
JWT_SECRET=your-secret-key-change-in-production

# Server
GRPC_PORT=50051
METRICS_PORT=9090
```

### Running Locally

```bash
# Install dependencies
go mod download

# Run database migrations
psql -U postgres -d account_db -f migrations/001_create_accounts_table.up.sql
psql -U postgres -d account_db -f migrations/002_add_role_column.up.sql

# Run the service
go run cmd/account/main.go
```

### Running with Docker

```bash
# Build image
docker build -t account-service -f Dockerfile .

# Run with docker-compose
docker-compose up account
```

## Testing

### Unit Tests
```bash
# Run unit tests
go test ./account -run TestService -v

# With coverage
go test ./account -run TestService -coverprofile=coverage.out
go tool cover -html=coverage.out
```

### Integration Tests
```bash
# Run integration tests (requires Docker)
go test ./account -run TestIntegration -v
```

### All Tests
```bash
# Run all tests
go test ./account -v

# Coverage: 77.6%
# Total: 31 tests (22 unit + 9 integration)
```

## API Reference

### gRPC Methods

| Method | Description | Authentication |
|--------|-------------|----------------|
| `Register` | Create new user account | No |
| `Login` | Authenticate and get tokens | No |
| `GetProfile` | Retrieve user profile | Yes (Token) |
| `UpdateProfile` | Update user information | Yes (Token) |
| `ChangePassword` | Change user password | Yes (Token) |
| `DeleteAccount` | Soft-delete account | Yes (Token) |
| `VerifyToken` | Validate JWT token | No |
| `RefreshToken` | Get new access token | Yes (Refresh Token) |

For detailed message definitions and examples, see [PROTO_SCHEMA.md](./docs/PROTO_SCHEMA.md).

## Database Schema

The service uses a PostgreSQL database with the following main table:

**accounts** - Stores user account information
- UUID primary key
- Email (unique, indexed)
- Bcrypt password hash
- Profile fields (name, phone)
- Role (USER/ADMIN) with CHECK constraint
- Status flags (is_verified, is_active)
- Timestamps (auto-updated)

For complete schema details, see [DATABASE_SCHEMA.md](./docs/DATABASE_SCHEMA.md).

## Authentication Flow

1. **Registration**: User provides email, password, name → Service creates account → Returns user + tokens
2. **Login**: User provides credentials → Service validates → Returns user + tokens
3. **Access**: Client includes JWT in `Authorization: Bearer <token>` header
4. **Refresh**: Before access token expires (15 min), use refresh token (7 day) to get new tokens

## Security

- Passwords hashed with bcrypt (cost 10)
- JWT tokens signed with HS256
- Soft deletes preserve audit trail
- Input validation on all endpoints
- gRPC communication over TLS (production)
- Rate limiting recommended (implement in gateway)

## Monitoring

### Health Check
```bash
grpcurl -plaintext localhost:50051 grpc.health.v1.Health/Check
```

### Metrics
Access Prometheus metrics at: `http://localhost:9090/metrics`

Available metrics:
- `grpc_server_handled_total` - Total RPC calls by method and status
- `grpc_server_handling_seconds` - Request duration histogram
- Custom business metrics as needed

## Documentation

- [Database Schema](./docs/DATABASE_SCHEMA.md) - Complete database structure, indexes, and constraints
- [Proto Schema](./docs/PROTO_SCHEMA.md) - gRPC service definitions, message types, and validation rules
- [Testing Guide](../../docs/TESTING.md) - Testing strategies and best practices
- [Setup Guide](../../docs/SETUP.md) - Development environment setup

## Development

### Code Generation

Regenerate protobuf code after modifying `account.proto`:

```bash
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    account/account.proto
```

### Database Migrations

Create new migration:
```bash
# Create migration files
touch migrations/003_migration_name.up.sql
touch migrations/003_migration_name.down.sql

# Apply migration
psql -U postgres -d account_db -f migrations/003_migration_name.up.sql

# Rollback
psql -U postgres -d account_db -f migrations/003_migration_name.down.sql
```

### Adding New Features

1. Update `account.proto` with new RPC method/message
2. Regenerate protobuf code
3. Implement in `service.go`
4. Add repository method in `repository.go` if needed
5. Write unit tests in `service_test.go`
6. Write integration tests in `integration_test.go`
7. Update documentation

## Troubleshooting

### Common Issues

**Database Connection Failed**
- Check PostgreSQL is running: `systemctl status postgresql`
- Verify credentials in environment variables
- Ensure database exists: `psql -U postgres -l`

**JWT Token Invalid**
- Verify `JWT_SECRET` matches between registration and verification
- Check token hasn't expired (15 min for access, 7 days for refresh)
- Ensure proper Bearer token format in header

**gRPC Connection Refused**
- Check service is running: `lsof -i :50051` (Linux/Mac) or `netstat -ano | findstr :50051` (Windows)
- Verify `GRPC_PORT` environment variable
- Check firewall/security groups in production

**Test Failures**
- Integration tests require Docker running
- Ensure no port conflicts (PostgreSQL container uses random port)
- Check test database migrations applied correctly

## Contributing

1. Create feature branch from `main`
2. Implement changes with tests
3. Ensure all tests pass: `go test ./account -v`
4. Update documentation if needed
5. Submit pull request with clear description

## License

See [LICENSE](../LICENSE) in repository root.

## Support

For issues and questions:
- Create GitHub issue in repository
- Check documentation in `docs/` folder
- Review test files for usage examples
