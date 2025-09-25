#!/usr/bin/env pwsh

# Accumen Bootstrap Script for Windows
# Installs dependencies and runs codegen stubs

param(
    [switch]$SkipTools,
    [switch]$Verbose
)

$ErrorActionPreference = "Stop"

if ($Verbose) {
    $VerbosePreference = "Continue"
}

Write-Host "🚀 Bootstrapping Accumen development environment..." -ForegroundColor Green

# Check Go installation
Write-Host "Checking Go installation..." -ForegroundColor Yellow
try {
    $goVersion = go version
    Write-Host "✅ Found: $goVersion" -ForegroundColor Green
} catch {
    Write-Error "❌ Go not found. Please install Go 1.22+ from https://golang.org/dl/"
    exit 1
}

# Verify Go version
$goVersionOutput = go version
if ($goVersionOutput -match "go(\d+\.\d+)") {
    $version = [Version]$Matches[1]
    $minVersion = [Version]"1.22"

    if ($version -lt $minVersion) {
        Write-Error "❌ Go version $version found, but 1.22+ is required"
        exit 1
    }
    Write-Host "✅ Go version $version meets requirements" -ForegroundColor Green
}

# Install development tools
if (-not $SkipTools) {
    Write-Host "Installing development tools..." -ForegroundColor Yellow

    $tools = @(
        "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
        "google.golang.org/protobuf/cmd/protoc-gen-go@latest"
        "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    )

    foreach ($tool in $tools) {
        Write-Host "Installing $tool..." -ForegroundColor Cyan
        go install $tool
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "⚠️  Failed to install $tool"
        } else {
            Write-Host "✅ Installed $tool" -ForegroundColor Green
        }
    }
}

# Download Go dependencies
Write-Host "Downloading Go dependencies..." -ForegroundColor Yellow
go mod download
if ($LASTEXITCODE -ne 0) {
    Write-Error "❌ Failed to download dependencies"
    exit 1
}
Write-Host "✅ Dependencies downloaded" -ForegroundColor Green

# Tidy go.mod
Write-Host "Tidying go.mod..." -ForegroundColor Yellow
go mod tidy
if ($LASTEXITCODE -ne 0) {
    Write-Error "❌ Failed to tidy go.mod"
    exit 1
}
Write-Host "✅ go.mod tidied" -ForegroundColor Green

# Run codegen stubs (placeholder)
Write-Host "Running codegen stubs..." -ForegroundColor Yellow
Write-Host "TODO: Add protobuf generation" -ForegroundColor Cyan
Write-Host "TODO: Add JSON schema generation" -ForegroundColor Cyan
Write-Host "✅ Codegen stubs completed (placeholder)" -ForegroundColor Green

# Create example config directory
Write-Host "Creating example configuration..." -ForegroundColor Yellow
New-Item -ItemType Directory -Force -Path "config" | Out-Null
$exampleConfig = @{
    "node" = @{
        "role" = "sequencer"
        "listen_addr" = "0.0.0.0:8080"
    }
    "wasm" = @{
        "runtime" = "wazero"
        "max_memory" = "128MB"
    }
    "vdk" = @{
        "bridge_enabled" = $true
        "accumulate_endpoint" = "https://mainnet.accumulatenetwork.io"
    }
} | ConvertTo-Json -Depth 10

Set-Content -Path "config/example.json" -Value $exampleConfig
Write-Host "✅ Created config/example.json" -ForegroundColor Green

# Test build
Write-Host "Testing build..." -ForegroundColor Yellow
make build
if ($LASTEXITCODE -ne 0) {
    Write-Error "❌ Build failed"
    exit 1
}
Write-Host "✅ Build successful" -ForegroundColor Green

Write-Host "🎉 Bootstrap completed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  make run-sequencer    # Run as sequencer" -ForegroundColor White
Write-Host "  make test            # Run tests" -ForegroundColor White
Write-Host "  make -C ops help     # See all available targets" -ForegroundColor White