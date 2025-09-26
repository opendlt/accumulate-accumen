#!/usr/bin/env pwsh
#
# Initialize Accumulate devnet in a temporary directory
# This script initializes the devnet configuration and prints RPC endpoints
#

param(
    [string]$WorkDir = $null
)

# Set error action preference
$ErrorActionPreference = "Stop"

# Function to write colored output
function Write-ColorOutput {
    param([string]$Message, [string]$Color = "White")
    Write-Host $Message -ForegroundColor $Color
}

# Create work directory if not specified
if (-not $WorkDir) {
    $WorkDir = Join-Path $env:TEMP "accumulate-devnet-$(Get-Date -Format 'yyyyMMdd-HHmmss')"
}

Write-ColorOutput "=== Accumulate Devnet Initialization ===" "Cyan"
Write-ColorOutput "Work directory: $WorkDir" "Yellow"

# Create work directory
if (-not (Test-Path $WorkDir)) {
    New-Item -ItemType Directory -Path $WorkDir -Force | Out-Null
    Write-ColorOutput "Created work directory: $WorkDir" "Green"
}

# Change to work directory
Push-Location $WorkDir

try {
    Write-ColorOutput "Initializing Accumulate devnet..." "Yellow"

    # Run accumulated init devnet
    $initOutput = & go run gitlab.com/accumulatenetwork/accumulate/cmd/accumulated init devnet 2>&1

    if ($LASTEXITCODE -ne 0) {
        Write-ColorOutput "Error: Failed to initialize devnet" "Red"
        Write-ColorOutput $initOutput "Red"
        exit 1
    }

    Write-ColorOutput "Devnet initialized successfully!" "Green"
    Write-ColorOutput $initOutput "White"

    Write-ColorOutput ""
    Write-ColorOutput "=== RPC Endpoints ===" "Cyan"

    # Parse and extract RPC endpoints from the configuration
    $configPath = Join-Path $WorkDir ".accumulate" "config.toml"

    if (Test-Path $configPath) {
        Write-ColorOutput "Configuration file: $configPath" "Yellow"

        # Read config and extract RPC addresses
        $config = Get-Content $configPath
        $rpcSections = @()
        $inRpcSection = $false

        foreach ($line in $config) {
            if ($line -match '^\[.*rpc.*\]') {
                $inRpcSection = $true
                $currentSection = $line.Trim()
            } elseif ($line -match '^\[.*\]' -and $inRpcSection) {
                $inRpcSection = $false
            } elseif ($inRpcSection -and $line -match 'listen.*=.*"([^"]+)"') {
                $address = $matches[1]
                Write-ColorOutput "RPC Endpoint: http://$address" "Green"
                $rpcSections += "http://$address"
            }
        }

        if ($rpcSections.Count -eq 0) {
            Write-ColorOutput "Warning: No RPC endpoints found in config" "Yellow"
            Write-ColorOutput "Default endpoints:" "White"
            Write-ColorOutput "  Directory Network: http://localhost:26660" "White"
            Write-ColorOutput "  Block Validator 0: http://localhost:26670" "White"
            Write-ColorOutput "  Block Validator 1: http://localhost:26680" "White"
            Write-ColorOutput "  Block Validator 2: http://localhost:26690" "White"
        }

    } else {
        Write-ColorOutput "Warning: Configuration file not found at $configPath" "Yellow"
        Write-ColorOutput "Default endpoints:" "White"
        Write-ColorOutput "  Directory Network: http://localhost:26660" "White"
        Write-ColorOutput "  Block Validator 0: http://localhost:26670" "White"
        Write-ColorOutput "  Block Validator 1: http://localhost:26680" "White"
        Write-ColorOutput "  Block Validator 2: http://localhost:26690" "White"
    }

    Write-ColorOutput ""
    Write-ColorOutput "=== Configuration for Accumen ===" "Cyan"
    Write-ColorOutput "Add these endpoints to your Accumen config:" "White"
    Write-ColorOutput ""
    Write-ColorOutput "l0:" "White"
    Write-ColorOutput "  source: static" "White"
    Write-ColorOutput "  static:" "White"
    Write-ColorOutput "    endpoints:" "White"
    Write-ColorOutput "      - http://localhost:26660  # Directory Network" "White"
    Write-ColorOutput "      - http://localhost:26670  # Block Validator 0" "White"
    Write-ColorOutput "      - http://localhost:26680  # Block Validator 1" "White"
    Write-ColorOutput "      - http://localhost:26690  # Block Validator 2" "White"
    Write-ColorOutput ""
    Write-ColorOutput "Next steps:" "Cyan"
    Write-ColorOutput "1. Run: ./ops/devnet-run.ps1 -WorkDir '$WorkDir'" "White"
    Write-ColorOutput "2. Update your Accumen configuration with the endpoints above" "White"
    Write-ColorOutput "3. Start Accumen with the updated configuration" "White"

} catch {
    Write-ColorOutput "Error during initialization: $_" "Red"
    exit 1
} finally {
    Pop-Location
}

Write-ColorOutput ""
Write-ColorOutput "Devnet initialization completed!" "Green"
Write-ColorOutput "Work directory: $WorkDir" "Yellow"