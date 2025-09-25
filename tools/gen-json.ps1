#!/usr/bin/env pwsh

# Accumen JSON Schema Generation Tool
# Generates Go types from JSON schemas using various code generation tools

param(
    [string]$SchemaDir = "types/json",
    [string]$OutputDir = "types/generated/json",
    [string]$Package = "jsontypes",
    [switch]$Validate,
    [switch]$Clean,
    [switch]$Verbose,
    [switch]$Help
)

$ErrorActionPreference = "Stop"

if ($Help) {
    Write-Host @"
Accumen JSON Schema Generation Tool

USAGE:
    ./tools/gen-json.ps1 [OPTIONS]

OPTIONS:
    -SchemaDir <path>    Directory containing JSON schema files (default: types/json)
    -OutputDir <path>    Output directory for generated Go files (default: types/generated/json)
    -Package <name>      Go package name for generated files (default: jsontypes)
    -Validate           Validate schemas before generation
    -Clean              Clean output directory before generation
    -Verbose            Enable verbose output
    -Help               Show this help message

EXAMPLES:
    ./tools/gen-json.ps1                                    # Generate with defaults
    ./tools/gen-json.ps1 -Validate -Clean -Verbose         # Full generation with validation
    ./tools/gen-json.ps1 -SchemaDir custom/schemas         # Use custom schema directory
"@
    exit 0
}

if ($Verbose) {
    $VerbosePreference = "Continue"
}

Write-Host "🔧 Accumen JSON Schema Generation Tool" -ForegroundColor Green
Write-Host "Schema Directory: $SchemaDir" -ForegroundColor Yellow
Write-Host "Output Directory: $OutputDir" -ForegroundColor Yellow
Write-Host "Package Name: $Package" -ForegroundColor Yellow

# Check if schema directory exists
if (-not (Test-Path $SchemaDir)) {
    Write-Error "❌ Schema directory not found: $SchemaDir"
    exit 1
}

# Clean output directory if requested
if ($Clean -and (Test-Path $OutputDir)) {
    Write-Host "🧹 Cleaning output directory..." -ForegroundColor Yellow
    Remove-Item -Path $OutputDir -Recurse -Force
}

# Create output directory
New-Item -ItemType Directory -Path $OutputDir -Force | Out-Null

# Find all JSON schema files
$schemaFiles = Get-ChildItem -Path $SchemaDir -Filter "*.schema.json" -Recurse

if ($schemaFiles.Count -eq 0) {
    Write-Warning "⚠️ No JSON schema files found in $SchemaDir"
    exit 0
}

Write-Host "📋 Found $($schemaFiles.Count) schema file(s)" -ForegroundColor Green

# Validate schemas if requested
if ($Validate) {
    Write-Host "✅ Validating JSON schemas..." -ForegroundColor Yellow

    foreach ($file in $schemaFiles) {
        Write-Verbose "Validating: $($file.Name)"

        try {
            $schema = Get-Content $file.FullName | ConvertFrom-Json

            # Basic validation checks
            if (-not $schema.'$schema') {
                Write-Warning "Schema $($file.Name) missing '\$schema' property"
            }

            if (-not $schema.title) {
                Write-Warning "Schema $($file.Name) missing 'title' property"
            }

            if (-not $schema.type) {
                Write-Warning "Schema $($file.Name) missing 'type' property"
            }

            Write-Host "  ✓ $($file.Name)" -ForegroundColor Green
        }
        catch {
            Write-Error "❌ Invalid JSON in $($file.Name): $_"
            exit 1
        }
    }
}

# Check for required tools
$tools = @("go")
foreach ($tool in $tools) {
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        Write-Error "❌ Required tool not found: $tool"
        Write-Host "Please install Go from https://golang.org/dl/"
        exit 1
    }
}

# Generate Go types from schemas
Write-Host "🏗️ Generating Go types from JSON schemas..." -ForegroundColor Yellow

foreach ($file in $schemaFiles) {
    $baseName = $file.BaseName -replace '\.schema$', ''
    $outputFile = Join-Path $OutputDir "$baseName.go"

    Write-Verbose "Processing: $($file.Name) -> $baseName.go"

    try {
        # Create a simple Go file with struct definitions
        # In a real implementation, you'd use a tool like quicktype or json-schema-codegen

        $goContent = @"
// Code generated from JSON schema. DO NOT EDIT.
// Source: $($file.Name)

package $Package

import (
    "encoding/json"
    "time"
)

"@

        # Parse the schema to generate basic struct
        $schema = Get-Content $file.FullName | ConvertFrom-Json
        $structName = ($schema.title -replace '[^A-Za-z0-9]', '') + "Metadata"

        $goContent += @"

// $structName represents the $($schema.title)
type $structName struct {
"@

        # Generate basic struct fields based on schema properties
        if ($schema.properties) {
            $schema.properties.PSObject.Properties | ForEach-Object {
                $fieldName = ($_.Name -split '_' | ForEach-Object {
                    $_.Substring(0,1).ToUpper() + $_.Substring(1).ToLower()
                }) -join ''

                $fieldType = "string"
                if ($_.Value.type -eq "integer") {
                    $fieldType = "uint64"
                } elseif ($_.Value.type -eq "boolean") {
                    $fieldType = "bool"
                } elseif ($_.Value.format -eq "date-time") {
                    $fieldType = "time.Time"
                } elseif ($_.Value.type -eq "array") {
                    $fieldType = "[]interface{}"
                } elseif ($_.Value.type -eq "object") {
                    $fieldType = "map[string]interface{}"
                }

                $jsonTag = "`json:`"$($_.Name)`"`"
                $goContent += "`n    $fieldName $fieldType $jsonTag"

                if ($_.Value.description) {
                    $goContent = $goContent -replace "($fieldName $fieldType $jsonTag)", "// $($_.Value.description)`n    `$1"
                }
            }
        }

        $goContent += @"

}

// Validate validates the $structName
func (m *$structName) Validate() error {
    // TODO: Add validation logic based on schema constraints
    return nil
}

// ToJSON converts the $structName to JSON
func (m *$structName) ToJSON() ([]byte, error) {
    return json.Marshal(m)
}

// FromJSON creates a $structName from JSON
func (m *$structName) FromJSON(data []byte) error {
    return json.Unmarshal(data, m)
}
"@

        # Write the generated Go file
        Set-Content -Path $outputFile -Value $goContent -Encoding UTF8
        Write-Host "  ✓ Generated $baseName.go" -ForegroundColor Green

    }
    catch {
        Write-Error "❌ Failed to process $($file.Name): $_"
        exit 1
    }
}

# Generate package-level files
$packageFile = Join-Path $OutputDir "doc.go"
$packageContent = @"
// Package $Package contains generated Go types from JSON schemas.
//
// This package is auto-generated from JSON schema files in the types/json directory.
// Do not edit these files directly. Instead, modify the source schemas and regenerate.
//
// Generated by: tools/gen-json.ps1
// Generated at: $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss UTC')
package $Package
"@

Set-Content -Path $packageFile -Value $packageContent -Encoding UTF8
Write-Host "  ✓ Generated package documentation" -ForegroundColor Green

# Run go fmt on generated files
Write-Host "🎨 Formatting generated Go code..." -ForegroundColor Yellow
try {
    Push-Location $OutputDir
    go fmt ./...
    Write-Host "  ✓ Code formatted successfully" -ForegroundColor Green
}
catch {
    Write-Warning "⚠️ Failed to format Go code: $_"
}
finally {
    Pop-Location
}

# Generate build tags and metadata
$buildFile = Join-Path $OutputDir "build.go"
$buildContent = @"
//go:build ignore
// +build ignore

package main

// Build information for generated JSON types
const (
    GeneratedAt = "$(Get-Date -Format 'yyyy-MM-dd HH:mm:ss UTC')"
    GeneratedBy = "tools/gen-json.ps1"
    SchemaCount = $($schemaFiles.Count)
)
"@

Set-Content -Path $buildFile -Value $buildContent -Encoding UTF8

Write-Host "🎉 JSON schema generation completed successfully!" -ForegroundColor Green
Write-Host ""
Write-Host "Generated files:" -ForegroundColor Cyan
Get-ChildItem -Path $OutputDir -Filter "*.go" | ForEach-Object {
    Write-Host "  $($_.Name)" -ForegroundColor White
}

Write-Host ""
Write-Host "Next steps:" -ForegroundColor Cyan
Write-Host "  go mod tidy              # Update dependencies" -ForegroundColor White
Write-Host "  go test ./$OutputDir     # Test generated types" -ForegroundColor White
Write-Host "  go build ./$OutputDir    # Build generated package" -ForegroundColor White