# Test Role in Register
Write-Host "Testing Register with role field..."
$request = '{"email":"role-test@example.com","password":"Test123!","name":"Role Test","phone":"1111111111"}'
Write-Host "Request: $request"
$result = & grpcurl -plaintext -d $request localhost:50051 account.AccountService/Register
Write-Host "Result:"
$result
