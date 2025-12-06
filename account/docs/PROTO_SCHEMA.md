# Account Service - Protocol Buffer Schema

## Overview
The Account service uses Protocol Buffers (proto3) for gRPC service definitions. This document provides a comprehensive reference for all messages and RPC methods.

## Proto Package
- **Syntax**: `proto3`
- **Package**: `account`
- **Go Package**: `github.com/Ujjwaljain16/E-commerce-Backend/account/pb`

## Service Definition

### AccountService

The main gRPC service providing user authentication and profile management.

```protobuf
service AccountService {
  rpc Register(RegisterRequest) returns (RegisterResponse);
  rpc Login(LoginRequest) returns (LoginResponse);
  rpc GetProfile(GetProfileRequest) returns (GetProfileResponse);
  rpc UpdateProfile(UpdateProfileRequest) returns (UpdateProfileResponse);
  rpc ChangePassword(ChangePasswordRequest) returns (ChangePasswordResponse);
  rpc DeleteAccount(DeleteAccountRequest) returns (DeleteAccountResponse);
  rpc VerifyToken(VerifyTokenRequest) returns (VerifyTokenResponse);
  rpc RefreshToken(RefreshTokenRequest) returns (RefreshTokenResponse);
}
```

## Message Definitions

### Core Messages

#### User

Represents a user account with all profile and metadata fields.

```protobuf
message User {
  string id = 1;
  string email = 2;
  string name = 3;
  string phone = 4;
  google.protobuf.Timestamp created_at = 5;
  google.protobuf.Timestamp updated_at = 6;
  bool is_verified = 7;
  bool is_active = 8;
  string role = 9;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `id` | string | 1 | UUID of the user account |
| `email` | string | 2 | User's email address |
| `name` | string | 3 | User's full name |
| `phone` | string | 4 | User's phone number (optional) |
| `created_at` | Timestamp | 5 | Account creation time (UTC) |
| `updated_at` | Timestamp | 6 | Last modification time (UTC) |
| `is_verified` | bool | 7 | Email verification status |
| `is_active` | bool | 8 | Account active status (false = soft deleted) |
| `role` | string | 9 | User role: "USER" or "ADMIN" |

**Notes**:
- `id` is a UUID v4 string
- `password_hash` is never included in User message for security
- Timestamps use `google.protobuf.Timestamp` for interoperability

---

### Registration

#### RegisterRequest

Request to create a new user account.

```protobuf
message RegisterRequest {
  string email = 1;
  string password = 2;
  string name = 3;
  string phone = 4;
}
```

| Field | Type | Tag | Required | Validation |
|-------|------|-----|----------|------------|
| `email` | string | 1 | Yes | Valid email format, unique |
| `password` | string | 2 | Yes | Minimum 6 characters recommended |
| `name` | string | 3 | Yes | Non-empty |
| `phone` | string | 4 | No | Optional, max 20 characters |

**Error Codes**:
- `InvalidArgument` - Missing required fields or invalid format
- `AlreadyExists` - Email already registered

#### RegisterResponse

Response containing the created user and authentication tokens.

```protobuf
message RegisterResponse {
  User user = 1;
  string access_token = 2;
  string refresh_token = 3;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `user` | User | 1 | Created user object (without password_hash) |
| `access_token` | string | 2 | JWT access token (15 min expiry) |
| `refresh_token` | string | 3 | JWT refresh token (7 day expiry) |

---

### Authentication

#### LoginRequest

Request to authenticate a user.

```protobuf
message LoginRequest {
  string email = 1;
  string password = 2;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `email` | string | 1 | Yes | Registered email address |
| `password` | string | 2 | Yes | Plain-text password (hashed by service) |

**Error Codes**:
- `InvalidArgument` - Missing email or password
- `Unauthenticated` - Invalid credentials
- `FailedPrecondition` - Account inactive or deleted

#### LoginResponse

Response containing user info and authentication tokens.

```protobuf
message LoginResponse {
  User user = 1;
  string access_token = 2;
  string refresh_token = 3;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `user` | User | 1 | Authenticated user object |
| `access_token` | string | 2 | JWT access token for API calls |
| `refresh_token` | string | 3 | JWT refresh token for token renewal |

---

### Profile Management

#### GetProfileRequest

Request to retrieve user profile information.

```protobuf
message GetProfileRequest {
  string user_id = 1;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `user_id` | string | 1 | Yes | UUID of the user |

**Error Codes**:
- `InvalidArgument` - Missing user_id
- `NotFound` - User not found or inactive

#### GetProfileResponse

Response containing user profile data.

```protobuf
message GetProfileResponse {
  User user = 1;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `user` | User | 1 | Complete user profile |

#### UpdateProfileRequest

Request to update user profile fields.

```protobuf
message UpdateProfileRequest {
  string user_id = 1;
  string name = 2;
  string phone = 3;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `user_id` | string | 1 | Yes | UUID of the user to update |
| `name` | string | 2 | Yes | New name (non-empty) |
| `phone` | string | 3 | No | New phone number (optional) |

**Error Codes**:
- `InvalidArgument` - Missing user_id or name
- `NotFound` - User not found

#### UpdateProfileResponse

Response containing the updated user.

```protobuf
message UpdateProfileResponse {
  User user = 1;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `user` | User | 1 | Updated user object |

---

### Password Management

#### ChangePasswordRequest

Request to change user's password.

```protobuf
message ChangePasswordRequest {
  string user_id = 1;
  string old_password = 2;
  string new_password = 3;
}
```

| Field | Type | Tag | Required | Validation |
|-------|------|-----|----------|------------|
| `user_id` | string | 1 | Yes | UUID of the user |
| `old_password` | string | 2 | Yes | Current password for verification |
| `new_password` | string | 3 | Yes | New password (min 6 chars recommended) |

**Error Codes**:
- `InvalidArgument` - Missing required fields
- `Unauthenticated` - Old password incorrect
- `NotFound` - User not found

#### ChangePasswordResponse

Response confirming password change.

```protobuf
message ChangePasswordResponse {
  bool success = 1;
  string message = 2;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `success` | bool | 1 | True if password changed successfully |
| `message` | string | 2 | Confirmation or error message |

---

### Account Deletion

#### DeleteAccountRequest

Request to soft-delete a user account.

```protobuf
message DeleteAccountRequest {
  string user_id = 1;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `user_id` | string | 1 | Yes | UUID of the user to delete |

**Note**: This performs a soft delete by setting `is_active = false`. Data is preserved for audit purposes.

**Error Codes**:
- `InvalidArgument` - Missing user_id
- `NotFound` - User not found or already deleted

#### DeleteAccountResponse

Response confirming account deletion.

```protobuf
message DeleteAccountResponse {
  bool success = 1;
  string message = 2;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `success` | bool | 1 | True if account deleted successfully |
| `message` | string | 2 | Confirmation message |

---

### Token Management

#### VerifyTokenRequest

Request to validate a JWT access token.

```protobuf
message VerifyTokenRequest {
  string token = 1;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `token` | string | 1 | Yes | JWT access token to verify |

**Error Codes**:
- `InvalidArgument` - Empty token
- `Unauthenticated` - Invalid or expired token

#### VerifyTokenResponse

Response containing token validation result.

```protobuf
message VerifyTokenResponse {
  bool valid = 1;
  string user_id = 2;
  google.protobuf.Timestamp expires_at = 3;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `valid` | bool | 1 | True if token is valid and not expired |
| `user_id` | string | 2 | UUID from token claims (empty if invalid) |
| `expires_at` | Timestamp | 3 | Token expiration time (empty if invalid) |

#### RefreshTokenRequest

Request to generate new access token from refresh token.

```protobuf
message RefreshTokenRequest {
  string refresh_token = 1;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `refresh_token` | string | 1 | Yes | JWT refresh token |

**Error Codes**:
- `InvalidArgument` - Missing refresh token
- `Unauthenticated` - Invalid or expired refresh token

#### RefreshTokenResponse

Response containing new authentication tokens.

```protobuf
message RefreshTokenResponse {
  string access_token = 1;
  string refresh_token = 2;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `access_token` | string | 1 | New JWT access token (15 min expiry) |
| `refresh_token` | string | 2 | New JWT refresh token (7 day expiry) |

---

## JWT Token Structure

While not defined in the proto file, the JWT tokens use the following claims:

### Access Token Claims
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "email": "user@example.com",
  "role": "USER",
  "exp": 1705329000,
  "iat": 1705328100
}
```

### Refresh Token Claims
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "type": "refresh",
  "exp": 1705933900,
  "iat": 1705328100
}
```

| Claim | Description |
|-------|-------------|
| `user_id` | UUID of the authenticated user |
| `email` | User's email (access token only) |
| `role` | User's role for RBAC (access token only) |
| `type` | Token type: "refresh" (refresh token only) |
| `exp` | Expiration time (Unix timestamp) |
| `iat` | Issued at time (Unix timestamp) |

**Token Lifetimes**:
- Access Token: 15 minutes
- Refresh Token: 7 days

**Signing**:
- Algorithm: HS256 (HMAC-SHA256)
- Secret: Configured via `JWT_SECRET` environment variable

---

## gRPC Status Codes

The service uses standard gRPC status codes for error handling:

| Code | Usage | Example |
|------|-------|---------|
| `OK` | Success | Successful registration |
| `InvalidArgument` | Invalid input | Missing required field, invalid email format |
| `Unauthenticated` | Auth failure | Wrong password, invalid token |
| `NotFound` | Resource not found | User ID doesn't exist |
| `AlreadyExists` | Duplicate resource | Email already registered |
| `FailedPrecondition` | Operation not allowed | Account inactive/deleted |
| `Internal` | Server error | Database error, unexpected failure |

---

## Field Validation Rules

### Email
- **Format**: RFC 5322 compliant email
- **Max Length**: 255 characters
- **Uniqueness**: Must be unique across all active accounts
- **Example**: `user@example.com`

### Password
- **Min Length**: 6 characters (recommendation: 8+)
- **Encoding**: UTF-8
- **Storage**: Bcrypt hash (cost factor: 10)
- **Transmission**: Plain-text over TLS-encrypted gRPC

### Name
- **Min Length**: 1 character (non-empty)
- **Max Length**: 255 characters
- **Format**: Any UTF-8 string
- **Example**: `John Doe`

### Phone
- **Max Length**: 20 characters
- **Format**: No strict validation (recommendation: E.164 format)
- **Optional**: Can be empty string
- **Example**: `+12345678901`

### User ID
- **Format**: UUID v4 (RFC 4122)
- **Example**: `550e8400-e29b-41d4-a716-446655440000`
- **Generation**: Server-side via `github.com/google/uuid`

### Role
- **Allowed Values**: `USER`, `ADMIN`
- **Default**: `USER` (set during registration)
- **Validation**: Case-sensitive, must match exactly

---

## Usage Examples

### Register New User
```protobuf
RegisterRequest {
  email: "john@example.com"
  password: "SecurePass123"
  name: "John Doe"
  phone: "+12345678901"
}
```

### Login
```protobuf
LoginRequest {
  email: "john@example.com"
  password: "SecurePass123"
}
```

### Get Profile
```protobuf
GetProfileRequest {
  user_id: "550e8400-e29b-41d4-a716-446655440000"
}
```

### Update Profile
```protobuf
UpdateProfileRequest {
  user_id: "550e8400-e29b-41d4-a716-446655440000"
  name: "John Smith"
  phone: "+19876543210"
}
```

### Change Password
```protobuf
ChangePasswordRequest {
  user_id: "550e8400-e29b-41d4-a716-446655440000"
  old_password: "SecurePass123"
  new_password: "NewSecurePass456"
}
```

### Verify Token
```protobuf
VerifyTokenRequest {
  token: "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."
}
```

---

## Code Generation

To regenerate Go code from proto files:

```bash
# Install protoc compiler and Go plugins
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Generate code
protoc --go_out=. --go_opt=paths=source_relative \
    --go-grpc_out=. --go-grpc_opt=paths=source_relative \
    account/account.proto
```

**Generated Files**:
- `account/pb/account.pb.go` - Message type definitions
- `account/pb/account_grpc.pb.go` - Service client and server interfaces

---

## Security Best Practices

1. **Password Transmission**:
   - Always use TLS for gRPC connections
   - Passwords sent as plain-text are hashed server-side with bcrypt
   - Never log or cache passwords

2. **Token Handling**:
   - Store tokens securely on client (e.g., secure cookies, encrypted storage)
   - Include tokens in `Authorization: Bearer <token>` header for authenticated requests
   - Refresh tokens before access token expiry

3. **Input Validation**:
   - Validate all inputs on server-side (don't trust client)
   - Sanitize email and name fields to prevent injection
   - Enforce password complexity in production

4. **Rate Limiting**:
   - Implement rate limiting for Register and Login endpoints
   - Prevent brute-force attacks on password verification
   - Consider account lockout after failed attempts

---

## Related Documentation

- [Database Schema](./DATABASE_SCHEMA.md) - PostgreSQL table definitions
- [Service Implementation](../service.go) - gRPC service logic
- [JWT Package](../../pkg/auth/) - Token generation and validation
- [Proto File](../account.proto) - Source proto definition
