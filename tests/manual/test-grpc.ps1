# Test Account Service Registration
$requestBody = @{
    email = "test@example.com"
    password = "password123"
    name = "Test User"
    phone = "1234567890"
} | ConvertTo-Json -Compress

Write-Host "Testing Register endpoint..."
Write-Host "Request: $requestBody"
Write-Host ""

& grpcurl -plaintext -d $requestBody localhost:50051 account.AccountService/Register
