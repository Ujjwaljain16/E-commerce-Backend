# Development Tools Installation Guide

## Prerequisites

### 1. Protocol Buffers Compiler (protoc)
```powershell
# Install via winget (Windows)
winget install --id Google.Protobuf -e

# Verify installation
protoc --version
```

### 2. Go Protobuf Plugins
```bash
# Install code generators
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Verify installation
protoc-gen-go --version
protoc-gen-go-grpc --version
```

### 3. golangci-lint
```powershell
# Install via go
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Verify installation
golangci-lint version
```

## Environment Setup

### Add Go bin to PATH
```powershell
# PowerShell (add to profile for persistence)
$env:PATH = "$env:USERPROFILE\go\bin;$env:PATH"
```

## Verify Setup
```bash
# Run from project root
make proto-gen  # Generate protobuf code
make test       # Run tests
make lint       # Run linter
```
