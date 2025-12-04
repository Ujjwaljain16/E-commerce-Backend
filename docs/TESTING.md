# Account Service Manual Testing Guide

## Prerequisites
- Docker and Docker Compose installed
- grpcurl installed (for testing gRPC endpoints)

## Install grpcurl (if not installed)
```powershell
# Using Go
go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest

# Or using Chocolatey
choco install grpcurl
```

## Start Services
```bash
docker-compose up -d
```

## Wait for services to be ready
```bash
docker-compose logs -f account-service
# Wait until you see: "Account Service listening on port 50051"
```

## Test 1: Register a new user
```bash
grpcurl -plaintext -d '{
  "email": "test@example.com",
  "password": "password123",
  "name": "Test User",
  "phone": "1234567890"
}' localhost:50051 account.AccountService/Register
```

**Expected Response:**
```json
{
  "user": {
    "id": "...",
    "email": "test@example.com",
    "name": "Test User",
    "phone": "1234567890",
    "isVerified": false,
    "isActive": true,
    "createdAt": "...",
    "updatedAt": "..."
  },
  "accessToken": "eyJhbGc...",
  "refreshToken": "eyJhbGc..."
}
```

## Test 2: Login
```bash
grpcurl -plaintext -d '{
  "email": "test@example.com",
  "password": "password123"
}' localhost:50051 account.AccountService/Login
```

## Test 3: Get Profile
```bash
# Replace USER_ID with the ID from registration response
grpcurl -plaintext -d '{
  "user_id": "YOUR_USER_ID_HERE"
}' localhost:50051 account.AccountService/GetProfile
```

## Test 4: Update Profile
```bash
grpcurl -plaintext -d '{
  "user_id": "YOUR_USER_ID_HERE",
  "name": "Updated Name",
  "phone": "9876543210"
}' localhost:50051 account.AccountService/UpdateProfile
```

## Test 5: Verify Token
```bash
# Replace TOKEN with access_token from login/register
grpcurl -plaintext -d '{
  "token": "YOUR_ACCESS_TOKEN_HERE"
}' localhost:50051 account.AccountService/VerifyToken
```

## Test 6: Change Password
```bash
grpcurl -plaintext -d '{
  "user_id": "YOUR_USER_ID_HERE",
  "old_password": "password123",
  "new_password": "newpassword456"
}' localhost:50051 account.AccountService/ChangePassword
```

## Test 7: Refresh Token
```bash
grpcurl -plaintext -d '{
  "refresh_token": "YOUR_REFRESH_TOKEN_HERE"
}' localhost:50051 account.AccountService/RefreshToken
```

## Check Database
```bash
# Connect to PostgreSQL
docker exec -it ecommerce-postgres psql -U postgres -d ecommerce

# List accounts
SELECT id, email, name, phone, is_verified, is_active, created_at FROM accounts;

# Exit
\q
```

## View Logs
```bash
# Account service logs
docker-compose logs -f account-service

# Database logs
docker-compose logs -f postgres
```

## Stop Services
```bash
docker-compose down

# To remove volumes (database data)
docker-compose down -v
```

## Troubleshooting

### Service won't start
```bash
# Check logs
docker-compose logs account-service

# Rebuild
docker-compose up --build -d
```

### Can't connect to database
```bash
# Check postgres health
docker-compose ps

# Check connection from service
docker exec -it account-service nc -zv postgres 5432
```

### gRPC errors
```bash
# List available services
grpcurl -plaintext localhost:50051 list

# Describe service
grpcurl -plaintext localhost:50051 describe account.AccountService
```
