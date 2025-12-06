$json = @"
{
  "email": "metrics-test@example.com",
  "password": "Test123!",
  "name": "Metrics Test",
  "phone": "9999999999"
}
"@

$json | grpcurl -plaintext -d @- localhost:50051 account.AccountService/Register
