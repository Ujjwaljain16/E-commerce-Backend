# Manual Test Files

This directory contains JSON files for manual testing of gRPC endpoints.

## Usage

From the project root:

```powershell
# Start services
docker-compose up -d

# Run a test (from project root)
cmd /c 'type tests/manual/test-register.json | grpcurl -plaintext -d @ localhost:50051 account.AccountService/Register'

# Or from this directory
cd tests/manual
cmd /c 'type test-register.json | grpcurl -plaintext -d @ localhost:50051 account.AccountService/Register'
```

## Test Files

### Account Service
- `test-register.json` - Register new user
- `test-login.json` - Login with original password
- `test-login-newpass.json` - Login with changed password
- `test-getprofile.json` - Get user profile by ID
- `test-updateprofile.json` - Update user name and phone
- `test-changepassword.json` - Change user password
- `test-verifytoken.json` - Verify JWT access token
- `test-refreshtoken.json` - Refresh JWT tokens
- `test-deleteaccount.json` - Soft delete user account

### PowerShell Scripts
- `test-grpc.ps1` - PowerShell script for testing (example)

## Notes

- Update `user_id` fields with actual IDs from registration
- Update `token` and `refresh_token` fields with actual tokens from login/register
- All tests assume the service is running on `localhost:50051`
