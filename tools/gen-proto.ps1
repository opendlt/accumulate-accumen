#!/usr/bin/env pwsh

# Accumen Protobuf Code Generation Tool
# Generates Go code from protobuf definitions using protoc and protoc-gen-go

param(
    [string]$ProtoDir = "types/proto",
    [string]$OutputDir = "types/generated/proto",
    [switch]$Validate,
    [switch]$Clean,
    [switch]$Verbose,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

if ($Help) {
    Write-Host @"
Accumen Protobuf Code Generation Tool

USAGE:
    ./tools/gen-proto.ps1 [OPTIONS]

OPTIONS:
    -ProtoDir <path>     Directory containing .proto files (default: types/proto)
    -OutputDir <path>    Output directory for generated Go files (default: types/generated/proto)
    -Validate           Validate protobuf files before generation
    -Clean              Clean output directory before generation
    -Verbose            Enable verbose output
    -Help               Show this help message

EXAMPLES:
    ./tools/gen-proto.ps1                                    # Generate with defaults
    ./tools/gen-proto.ps1 -Validate -Clean -Verbose         # Full generation with validation
    ./tools/gen-proto.ps1 -ProtoDir custom/proto           # Use custom proto directory

REQUIREMENTS:
    - protoc (Protocol Buffer compiler)
    - protoc-gen-go (Go plugin for protoc)
    - Go 1.22+ (for code generation and formatting)
"@
    exit 0
}

if ($Verbose) {
    $VerbosePreference = "Continue"
}

Write-Host "🔧 Accumen Protobuf Code Generation Tool" -ForegroundColor Green
Write-Host "Proto Directory: $ProtoDir" -ForegroundColor Yellow
Write-Host "Output Directory: $OutputDir" -ForegroundColor Yellow

# Check if proto directory exists
if (-not (Test-Path $ProtoDir)) {
    Write-Error "❌ Proto directory not found: $ProtoDir"
    exit 1
}

# Clean output directory if requested
if ($Clean -and (Test-Path $OutputDir)) {
    Write-Host "🧹 Cleaning output directory..." -ForegroundColor Yellow
    Remove-Item -Path $OutputDir -Recurse -Force
}

# Create output directory
New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

# Find all .proto files
$protoFiles = Get-ChildItem -Path $ProtoDir -Filter "*.proto" -Recurse

if ($protoFiles.Count -eq 0) {
    Write-Warning "⚠️ No .proto files found in $ProtoDir"
    exit 0
}

Write-Host "📋 Found $($protoFiles.Count) proto file(s)" -ForegroundColor Green

# Check for required tools
$tools = @("protoc", "protoc-gen-go")
foreach ($tool in $tools) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        Write-Error "❌ Required tool not found: $tool"
        switch ($tool) {
            "protoc" {
                Write-Host "Please install protoc from https://grpc.io/docs/protoc-installation/" -ForegroundColor Red
            }
            "protoc-gen-go" {
                Write-Host "Please install protoc-gen-go: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" -ForegroundColor Red
            }
        }
        exit 1
    }
}

# Validate proto files if requested
if ($Validate) {
    Write-Host "✅ Validating protobuf files..." -ForegroundColor Yellow

    foreach ($file in $protoFiles) {
        Write-Verbose "Validating: $($file.Name)"

        # Run protoc with --descriptor_set_out to validate syntax
        $tempDescriptor = [System.IO.Path]::GetTempFileName()
        try {
            $validateArgs = @(
                "--descriptor_set_out=$tempDescriptor",
                "--proto_path=$ProtoDir",
                $file.FullName
            )

            & protoc @validateArgs 2>$null
            if ($LASTEXITCODE -ne 0) {
                Write-Error "❌ Validation failed for $($file.Name)"
                exit 1
            }

            Write-Host "  ✓ $($file.Name)" -ForegroundColor Green
        }
        finally {
            if (Test-Path $tempDescriptor) {
                Remove-Item $tempDescriptor -Force
            }
        }
    }
}

# Generate Go code from proto files
Write-Host "🏗️ Generating Go code from protobuf files..." -ForegroundColor Yellow

# Get the module path from go.mod
$moduleRoot = Get-Location
$goModFile = Join-Path $moduleRoot "go.mod"
$moduleName = ""

if (Test-Path $goModFile) {
    $goModContent = Get-Content $goModFile
    $moduleLine = $goModContent | Where-Object { $_ -match "^module\s+" } | Select-Object -First 1
    if ($moduleLine) {
        $moduleName = ($moduleLine -replace "^module\s+", "").Trim()
    }
}

if (-not $moduleName) {
    Write-Warning "Could not determine module name from go.mod, using default"
    $moduleName = "github.com/opendlt/accumulate-accumen"
}

foreach ($file in $protoFiles) {
    Write-Verbose "Processing: $($file.Name)"

    # Calculate relative path from proto directory
    $relativePath = [System.IO.Path]::GetRelativePath($ProtoDir, $file.DirectoryName)
    $outputSubDir = Join-Path $OutputDir $relativePath

    # Create subdirectory if needed
    if (-not (Test-Path $outputSubDir)) {
        New-Item -ItemType Directory -Path $outputSubDir -Force | Out-Null
    }

    # Generate protoc arguments
    $protocArgs = @(
        "--go_out=$OutputDir",
        "--go_opt=paths=source_relative",
        "--go_opt=module=$moduleName/$($OutputDir -replace '\\', '/')",
        "--proto_path=$ProtoDir"
    )

    # Add common proto paths (for well-known types)
    if (Get-Command "protoc" -ErrorAction SilentlyContinue) {
        $protocVersion = & protoc --version 2>$null
        if ($protocVersion -match "libprotoc (\d+)\.") {
            $protocArgs += "--proto_path=$env:GOPATH/src"
            $protocArgs += "--proto_path=$env:GOROOT/src"
        }
    }

    $protocArgs += $file.FullName

    try {
        & protoc @protocArgs
        if ($LASTEXITCODE -ne 0) {
            Write-Error "❌ Failed to generate code for $($file.Name)"
            exit 1
        }

        Write-Host "  ✓ Generated code for $($file.Name)" -ForegroundColor Green
    }
    catch {
        Write-Error "❌ Failed to process $($file.Name): $_"
        exit 1
    }
}

# Run go fmt on generated files
Write-Host "🎨 Formatting generated Go code..." -ForegroundColor Yellow
try {
    Push-Location $OutputDir
    & go fmt ./...
    if ($LASTEXITCODE -eq 0) {
        Write-Host "  ✓ Code formatted successfully" -ForegroundColor Green
    } else {
        Write-Warning "⚠️ Failed to format some Go files"
    }
}
catch {
    Write-Warning "⚠️ Failed to format Go code: $_"
}
finally {
    Pop-Location
}

# Generate go.mod for the generated package if it doesn't exist
$generatedGoMod = Join-Path $OutputDir "go.mod"
if (-not (Test-Path $generatedGoMod)) {
    $generatedModuleName = "$moduleName/$($OutputDir -replace '\\', '/')"
    $goModContent = @"
module $generatedModuleName

go 1.22

require (
    google.golang.org/protobuf v1.31.0
)
"@
    Set-Content -Path $generatedGoMod -Value $goModContent -Encoding UTF8
    Write-Host "  ✓ Generated go.mod for protobuf package" -ForegroundColor Green
}

# Generate package documentation
$docFile = Join-Path $OutputDir "doc.go"
$docContent = @"
// Package proto contains generated Go types from protobuf definitions.
//
// This package is auto-generated from .proto files in the types/proto directory.
// Do not edit these files directly. Instead, modify the source .proto files and regenerate.
//
// Generated by: tools/gen-proto.ps1
// Generated at: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss UTC')
package proto
"@

Set-Content -Path $docFile -Value $docContent -Encoding UTF8
Write-Host "  ✓ Generated package documentation" -ForegroundColor Green

# Generate build metadata
$buildFile = Join-Path $OutputDir "build.go"
$buildContent = @"
//go:build ignore
// +build ignore

package main

// Build information for generated protobuf types
const (
    GeneratedAt = "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss UTC')"
    GeneratedBy = "tools/gen-proto.ps1"
    ProtoCount = $($protoFiles.Count)
)
"@

Set-Content -Path $buildFile -Value $buildContent -Encoding UTF8

Write-Host "🎉 Protobuf code generation completed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "Generated files:" -ForegroundColor Cyan
Get-ChildItem -Path $OutputDir -Filter "*.go" -Recurse | ForEach-Object {
    $relativePath = [System.IO.Path]::GetRelativePath($OutputDir, $_.FullName)
    Write-Host "  $relativePath" -ForegroundColor White
}

Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  go mod tidy              # Update dependencies" -ForegroundColor White
Write-Host "  go test ./$OutputDir     # Test generated types" -ForegroundColor White
Write-Host "  go build ./$OutputDir    # Build generated package" -ForegroundColor White