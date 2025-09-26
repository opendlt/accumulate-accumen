#!/usr/bin/env pwsh
#
# Run Accumulate devnet with logging
# This script starts the devnet and waits until RPC is available
#

param(
    [string]$WorkDir,
    [string]$LogDir = "logs",
    [int]$TimeoutSeconds = 120
)

# Set error action preference
$ErrorActionPreference = "Stop"

# Function to write colored output
function Write-ColorOutput {
    param([string]$Message, [string]$Color = "White")
    Write-Host $Message -ForegroundColor $Color
}

# Function to test RPC endpoint
function Test-RpcEndpoint {
    param([string]$Endpoint)
    try {
        $response = Invoke-RestMethod -Uri "$Endpoint/status" -Method Get -TimeoutSec 5 -ErrorAction SilentlyContinue
        return $true
    } catch {
        return $false
    }
}

if (-not $WorkDir) {
    Write-ColorOutput "Error: WorkDir parameter is required" "Red"
    Write-ColorOutput "Usage: ./ops/devnet-run.ps1 -WorkDir <path-to-devnet-directory>" "Yellow"
    exit 1
}

if (-not (Test-Path $WorkDir)) {
    Write-ColorOutput "Error: Work directory does not exist: $WorkDir" "Red"
    Write-ColorOutput "Run ./ops/devnet-init.ps1 first to initialize the devnet" "Yellow"
    exit 1
}

Write-ColorOutput "=== Accumulate Devnet Runner ===" "Cyan"
Write-ColorOutput "Work directory: $WorkDir" "Yellow"

# Create logs directory
$LogsPath = Join-Path (Get-Location) $LogDir
if (-not (Test-Path $LogsPath)) {
    New-Item -ItemType Directory -Path $LogsPath -Force | Out-Null
    Write-ColorOutput "Created logs directory: $LogsPath" "Green"
}

# Set up log file
$LogFile = Join-Path $LogsPath "devnet-$(Get-Date -Format 'yyyyMMdd-HHmmss').log"
Write-ColorOutput "Log file: $LogFile" "Yellow"

# Change to work directory
Push-Location $WorkDir

try {
    Write-ColorOutput "Starting Accumulate devnet..." "Yellow"

    # Start accumulated run in background
    $processArgs = @{
        FilePath = "go"
        ArgumentList = @("run", "gitlab.com/accumulatenetwork/accumulate/cmd/accumulated", "run")
        RedirectStandardOutput = $LogFile
        RedirectStandardError = $LogFile
        NoNewWindow = $true
        PassThru = $true
    }

    $devnetProcess = Start-Process @processArgs

    if (-not $devnetProcess) {
        Write-ColorOutput "Error: Failed to start devnet process" "Red"
        exit 1
    }

    Write-ColorOutput "Devnet process started with PID: $($devnetProcess.Id)" "Green"
    Write-ColorOutput "Waiting for RPC endpoints to become available..." "Yellow"

    # Wait for RPC to become available
    $endpoints = @(
        "http://localhost:26660",  # Directory Network
        "http://localhost:26670",  # Block Validator 0
        "http://localhost:26680",  # Block Validator 1
        "http://localhost:26690"   # Block Validator 2
    )

    $startTime = Get-Date
    $ready = $false

    while ((Get-Date).Subtract($startTime).TotalSeconds -lt $TimeoutSeconds) {
        # Check if process is still running
        if ($devnetProcess.HasExited) {
            Write-ColorOutput "Error: Devnet process exited unexpectedly" "Red"
            Write-ColorOutput "Check log file for details: $LogFile" "Yellow"
            exit 1
        }

        # Test primary endpoint (Directory Network)
        if (Test-RpcEndpoint $endpoints[0]) {
            Write-ColorOutput "Directory Network RPC is ready: $($endpoints[0])" "Green"
            $ready = $true
            break
        }

        Write-Host "." -NoNewline -ForegroundColor Yellow
        Start-Sleep 2
    }

    Write-Host ""  # New line after dots

    if (-not $ready) {
        Write-ColorOutput "Timeout: RPC endpoints did not become available within $TimeoutSeconds seconds" "Red"
        Write-ColorOutput "Check log file for details: $LogFile" "Yellow"

        # Kill the process
        if (-not $devnetProcess.HasExited) {
            $devnetProcess.Kill()
            Write-ColorOutput "Killed devnet process" "Yellow"
        }
        exit 1
    }

    Write-ColorOutput ""
    Write-ColorOutput "=== Devnet Status ===" "Cyan"

    # Test all endpoints
    foreach ($endpoint in $endpoints) {
        if (Test-RpcEndpoint $endpoint) {
            Write-ColorOutput "✓ $endpoint - Ready" "Green"
        } else {
            Write-ColorOutput "✗ $endpoint - Not Ready" "Yellow"
        }
    }

    Write-ColorOutput ""
    Write-ColorOutput "=== Devnet Running Successfully! ===" "Green"
    Write-ColorOutput "Process ID: $($devnetProcess.Id)" "White"
    Write-ColorOutput "Log file: $LogFile" "White"
    Write-ColorOutput ""
    Write-ColorOutput "Primary endpoints:" "Cyan"
    Write-ColorOutput "  Directory Network: http://localhost:26660" "White"
    Write-ColorOutput "  Block Validator 0: http://localhost:26670" "White"
    Write-ColorOutput "  Block Validator 1: http://localhost:26680" "White"
    Write-ColorOutput "  Block Validator 2: http://localhost:26690" "White"
    Write-ColorOutput ""
    Write-ColorOutput "The devnet is now ready for use with Accumen!" "Green"
    Write-ColorOutput "Press Ctrl+C to stop the devnet" "Yellow"

    # Wait for process to exit or Ctrl+C
    try {
        $devnetProcess.WaitForExit()
    } catch {
        Write-ColorOutput "Received interrupt signal" "Yellow"
    }

} catch {
    Write-ColorOutput "Error during devnet execution: $_" "Red"
    exit 1
} finally {
    # Cleanup
    if ($devnetProcess -and -not $devnetProcess.HasExited) {
        Write-ColorOutput "Stopping devnet process..." "Yellow"
        $devnetProcess.Kill()
        $devnetProcess.WaitForExit(5000)  # Wait up to 5 seconds
        Write-ColorOutput "Devnet process stopped" "Green"
    }

    Pop-Location
}

Write-ColorOutput "Devnet session completed" "Cyan"