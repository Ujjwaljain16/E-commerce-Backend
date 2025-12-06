# Account Service - Database Schema

## Overview
The Account service uses PostgreSQL to store user account information. This document provides a comprehensive reference for the database schema.

## Tables

### accounts

Stores user account information including credentials, profile data, and status flags.

#### Schema Definition

```sql
CREATE TABLE IF NOT EXISTS accounts (
    id VARCHAR(36) PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    name VARCHAR(255) NOT NULL,
    phone VARCHAR(20),
    role VARCHAR(20) NOT NULL DEFAULT 'USER',
    is_verified BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

#### Columns

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| `id` | VARCHAR(36) | PRIMARY KEY | - | UUID identifier for the user account |
| `email` | VARCHAR(255) | UNIQUE NOT NULL | - | User's email address (used for login) |
| `password_hash` | VARCHAR(255) | NOT NULL | - | Bcrypt hash of user's password (cost factor: 10) |
| `name` | VARCHAR(255) | NOT NULL | - | User's full name |
| `phone` | VARCHAR(20) | - | - | User's phone number (optional) |
| `role` | VARCHAR(20) | NOT NULL, CHECK | 'USER' | User role: 'USER' or 'ADMIN' |
| `is_verified` | BOOLEAN | - | FALSE | Email verification status |
| `is_active` | BOOLEAN | - | TRUE | Account active status (FALSE = soft deleted) |
| `created_at` | TIMESTAMP WITH TIME ZONE | - | CURRENT_TIMESTAMP | Account creation timestamp |
| `updated_at` | TIMESTAMP WITH TIME ZONE | - | CURRENT_TIMESTAMP | Last update timestamp (auto-updated) |

#### Constraints

- **Primary Key**: `id` - Unique identifier
- **Unique**: `email` - Ensures no duplicate email addresses
- **Check Constraint**: `role IN ('USER', 'ADMIN')` - Enforces valid role values
- **Not Null**: `email`, `password_hash`, `name`, `role` - Required fields

#### Indexes

```sql
-- Email index for fast login lookups
CREATE INDEX idx_accounts_email ON accounts(email);

-- Active status index for filtering active accounts
CREATE INDEX idx_accounts_is_active ON accounts(is_active);

-- Role index for role-based queries
CREATE INDEX idx_accounts_role ON accounts(role);
```

| Index Name | Column(s) | Purpose |
|------------|-----------|---------|
| `idx_accounts_email` | email | Fast user lookup during login |
| `idx_accounts_is_active` | is_active | Efficiently filter active/deleted accounts |
| `idx_accounts_role` | role | Support role-based access control queries |

#### Triggers

```sql
-- Automatically update updated_at on row modification
CREATE TRIGGER update_accounts_updated_at
    BEFORE UPDATE ON accounts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
```

The `update_accounts_updated_at` trigger automatically sets `updated_at` to the current timestamp whenever a row is modified.

## Migration History

| Migration | File | Description |
|-----------|------|-------------|
| 001 | `001_create_accounts_table.up.sql` | Initial table creation with core fields |
| 002 | `002_add_role_column.up.sql` | Added `role` column for RBAC support |

## Data Types and Formats

### ID Format
- **Type**: UUID v4
- **Format**: `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx`
- **Example**: `550e8400-e29b-41d4-a716-446655440000`
- **Generation**: `github.com/google/uuid`

### Password Hash Format
- **Algorithm**: bcrypt
- **Cost Factor**: 10
- **Format**: `$2a$10$[53 character hash]`
- **Example**: `$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy`
- **Library**: `golang.org/x/crypto/bcrypt`

### Email Format
- **Validation**: Standard email format validation
- **Case**: Stored as-is (case-sensitive)
- **Maximum Length**: 255 characters

### Phone Format
- **Validation**: Optional field, no strict format enforced
- **Maximum Length**: 20 characters
- **Recommendation**: Store with country code (e.g., +1234567890)

### Role Values
- **Allowed Values**: 
  - `USER` - Standard user account (default)
  - `ADMIN` - Administrator with elevated privileges
- **Validation**: Enforced by CHECK constraint

### Timestamps
- **Type**: TIMESTAMP WITH TIME ZONE
- **Format**: ISO 8601 with timezone
- **Example**: `2024-01-15 14:30:00+00:00`
- **Timezone**: UTC recommended for storage

## Common Queries

### Find User by Email
```sql
SELECT * FROM accounts WHERE email = $1 AND is_active = TRUE;
```

### Get All Active Admins
```sql
SELECT * FROM accounts WHERE role = 'ADMIN' AND is_active = TRUE;
```

### Soft Delete Account
```sql
UPDATE accounts SET is_active = FALSE, updated_at = CURRENT_TIMESTAMP WHERE id = $1;
```

### Update User Profile
```sql
UPDATE accounts 
SET name = $1, phone = $2, updated_at = CURRENT_TIMESTAMP 
WHERE id = $3 AND is_active = TRUE;
```

### Check Email Exists
```sql
SELECT EXISTS(SELECT 1 FROM accounts WHERE email = $1) AS exists;
```

## Security Considerations

1. **Password Storage**:
   - Never store plain-text passwords
   - Always use bcrypt with cost factor ≥ 10
   - Password hashes are one-way and cannot be decrypted

2. **Email Privacy**:
   - Email is sensitive PII (Personally Identifiable Information)
   - Ensure proper access controls in application layer
   - Consider encryption for email field in production

3. **Soft Deletes**:
   - Use `is_active = FALSE` instead of hard deletes
   - Preserves referential integrity
   - Allows account recovery if needed
   - Always filter by `is_active = TRUE` in queries

4. **Role-Based Access**:
   - Validate role on every privileged operation
   - Check constraint ensures only valid roles in database
   - Additional validation in application layer recommended

## Future Enhancements

- [ ] Add `email_verified_at` timestamp for audit trail
- [ ] Add `last_login_at` for security monitoring
- [ ] Add `password_changed_at` for password rotation policies
- [ ] Add composite index on `(email, is_active)` for login optimization
- [ ] Consider partitioning by `created_at` for large datasets
- [ ] Add `failed_login_attempts` and `locked_until` for account security
- [ ] Add `preferences` JSONB column for user settings

## Related Documentation

- [Proto Schema](./PROTO_SCHEMA.md) - gRPC service definitions
- [Migration Files](../migrations/) - SQL migration scripts
- [Repository Implementation](../repository.go) - Database access layer
