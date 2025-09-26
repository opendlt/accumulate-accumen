# PowerShell script to run Accumen sequencer locally
# Reads config/local.yaml and starts the sequencer with RPC server

param(
    [string]$Config = "config/local.yaml",
    [string]$Role = "sequencer",
    [string]$RPC = ":8666",
    [string]$LogLevel = "info",
    [ValidateSet("devnet", "testnet", "mainnet", "")]
    [string]$Network = ""
)

# Set error handling
$ErrorActionPreference = "Stop"

# Colors for output
$Green = [System.ConsoleColor]::Green
$Yellow = [System.ConsoleColor]::Yellow
$Red = [System.ConsoleColor]::Red
$Blue = [System.ConsoleColor]::Blue

function Write-ColoredOutput {
    param([string]$Message, [System.ConsoleColor]$Color = [System.ConsoleColor]::White)
    $originalColor = $Host.UI.RawUI.ForegroundColor
    $Host.UI.RawUI.ForegroundColor = $Color
    Write-Host $Message
    $Host.UI.RawUI.ForegroundColor = $originalColor
}

Write-ColoredOutput "🚀 Starting Accumen Node..." $Blue
Write-ColoredOutput "   Role: $Role" $Yellow
Write-ColoredOutput "   Config: $Config" $Yellow
Write-ColoredOutput "   RPC: $RPC" $Yellow
Write-ColoredOutput "   Log Level: $LogLevel" $Yellow
if ($Network) {
    Write-ColoredOutput "   Network: $Network" $Yellow
}
Write-ColoredOutput ""

# Check if config file exists
if (-not (Test-Path $Config)) {
    Write-ColoredOutput "❌ Config file not found: $Config" $Red
    Write-ColoredOutput "   Please create the config file or specify a different path with -Config parameter" $Yellow
    exit 1
}

# Check if Go is installed
try {
    $goVersion = go version 2>$null
    if ($LASTEXITCODE -ne 0) {
        throw "Go not found"
    }
    Write-ColoredOutput "✅ Go detected: $($goVersion.Split()[2])" $Green
} catch {
    Write-ColoredOutput "❌ Go is not installed or not in PATH" $Red
    Write-ColoredOutput "   Please install Go 1.22+ from https://golang.org/dl/" $Yellow
    exit 1
}

# Check if we're in the right directory
if (-not (Test-Path "cmd/accumen")) {
    Write-ColoredOutput "❌ Not in Accumen root directory" $Red
    Write-ColoredOutput "   Please run this script from the accumulate-accumen root directory" $Yellow
    exit 1
}

# Extract storage path from config and ensure data directory exists
try {
    if (Get-Command "yq" -ErrorAction SilentlyContinue) {
        $storagePath = yq '.storage.path // "data/l1"' $Config
        if ($storagePath -and $storagePath -ne "null") {
            Write-ColoredOutput "📁 Ensuring storage directory exists: $storagePath" $Yellow
            New-Item -ItemType Directory -Force -Path $storagePath | Out-Null
        }
    } else {
        # Default path if yq is not available
        $defaultPath = "data/l1"
        Write-ColoredOutput "📁 Ensuring default storage directory exists: $defaultPath" $Yellow
        New-Item -ItemType Directory -Force -Path $defaultPath | Out-Null
    }
} catch {
    Write-ColoredOutput "⚠️  Warning: Could not create storage directory: $($_.Exception.Message)" $Yellow
}

# Display config summary
Write-ColoredOutput "📋 Configuration Summary:" $Blue
try {
    if (Get-Command "yq" -ErrorAction SilentlyContinue) {
        Write-ColoredOutput "   Chain Config:" $Yellow
        yq '.chainId, .blockTime, .anchorEvery' $Config | ForEach-Object {
            Write-ColoredOutput "     $_" $Yellow
        }
    } else {
        Write-ColoredOutput "   Config file: $Config (install 'yq' for detailed preview)" $Yellow
    }
} catch {
    Write-ColoredOutput "   Config file: $Config" $Yellow
}

Write-ColoredOutput ""
Write-ColoredOutput "🔧 Build and run command:" $Blue
$cmdPreview = "   go run ./cmd/accumen --role=$Role --config=$Config --rpc=$RPC --log-level=$LogLevel"
if ($Network) {
    $cmdPreview += " --network=$Network"
}
Write-ColoredOutput $cmdPreview $Yellow
Write-ColoredOutput ""

# Set up signal handling
$signalReceived = $false
Register-EngineEvent PowerShell.Exiting -Action {
    $global:signalReceived = $true
    Write-ColoredOutput "`n🛑 Shutdown signal received..." $Yellow
}

# Set network environment variable if specified
if ($Network) {
    Write-ColoredOutput "🌐 Setting network environment: ACCUMEN_NETWORK=$Network" $Yellow
    $env:ACCUMEN_NETWORK = $Network
}

# Run the application
try {
    Write-ColoredOutput "▶️  Starting Accumen..." $Green
    Write-ColoredOutput ""

    # Build command arguments
    $args = @("./cmd/accumen", "--role=$Role", "--config=$Config", "--rpc=$RPC", "--log-level=$LogLevel")
    if ($Network) {
        $args += "--network=$Network"
    }

    # Execute the go run command
    & go run $args

    if ($LASTEXITCODE -ne 0) {
        Write-ColoredOutput "❌ Accumen exited with error code: $LASTEXITCODE" $Red
        exit $LASTEXITCODE
    }

} catch {
    Write-ColoredOutput "❌ Error running Accumen: $($_.Exception.Message)" $Red
    exit 1
} finally {
    Write-ColoredOutput ""
    Write-ColoredOutput "🏁 Accumen stopped" $Blue
}

# If we get here, it was a clean exit
Write-ColoredOutput "✅ Accumen shutdown complete" $Green