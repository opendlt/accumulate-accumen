#!/bin/bash
#
# Run Accumulate devnet with logging
# This script starts the devnet and waits until RPC is available
#

set -e

# Default values
WORK_DIR=""
LOG_DIR="logs"
TIMEOUT_SECONDS=120

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --work-dir)
            WORK_DIR="$2"
            shift 2
            ;;
        --log-dir)
            LOG_DIR="$2"
            shift 2
            ;;
        --timeout)
            TIMEOUT_SECONDS="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 --work-dir <directory> [OPTIONS]"
            echo ""
            echo "Required:"
            echo "  --work-dir    Directory containing devnet configuration"
            echo ""
            echo "Options:"
            echo "  --log-dir     Directory for log files (default: logs)"
            echo "  --timeout     Timeout in seconds to wait for RPC (default: 120)"
            echo "  -h, --help    Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Color output functions
print_color() {
    local color=$1
    local message=$2
    case $color in
        red) echo -e "\033[31m$message\033[0m" ;;
        green) echo -e "\033[32m$message\033[0m" ;;
        yellow) echo -e "\033[33m$message\033[0m" ;;
        cyan) echo -e "\033[36m$message\033[0m" ;;
        white) echo -e "\033[37m$message\033[0m" ;;
        *) echo "$message" ;;
    esac
}

# Function to test RPC endpoint
test_rpc_endpoint() {
    local endpoint=$1
    if curl -s --connect-timeout 5 --max-time 10 "$endpoint/status" >/dev/null 2>&1; then
        return 0
    else
        return 1
    fi
}

# Function to cleanup on exit
cleanup() {
    if [ -n "$DEVNET_PID" ]; then
        print_color yellow "Stopping devnet process..."
        kill "$DEVNET_PID" 2>/dev/null || true
        wait "$DEVNET_PID" 2>/dev/null || true
        print_color green "Devnet process stopped"
    fi
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

if [ -z "$WORK_DIR" ]; then
    print_color red "Error: --work-dir parameter is required"
    print_color yellow "Usage: $0 --work-dir <path-to-devnet-directory>"
    exit 1
fi

if [ ! -d "$WORK_DIR" ]; then
    print_color red "Error: Work directory does not exist: $WORK_DIR"
    print_color yellow "Run ./ops/devnet-init.sh first to initialize the devnet"
    exit 1
fi

print_color cyan "=== Accumulate Devnet Runner ==="
print_color yellow "Work directory: $WORK_DIR"

# Create logs directory
if [ ! -d "$LOG_DIR" ]; then
    mkdir -p "$LOG_DIR"
    print_color green "Created logs directory: $LOG_DIR"
fi

# Set up log file
LOG_FILE="$LOG_DIR/devnet-$(date +%Y%m%d-%H%M%S).log"
print_color yellow "Log file: $LOG_FILE"

# Change to work directory
cd "$WORK_DIR"

print_color yellow "Starting Accumulate devnet..."

# Start accumulated run in background
go run gitlab.com/accumulatenetwork/accumulate/cmd/accumulated run > "$LOG_FILE" 2>&1 &
DEVNET_PID=$!

if [ -z "$DEVNET_PID" ]; then
    print_color red "Error: Failed to start devnet process"
    exit 1
fi

print_color green "Devnet process started with PID: $DEVNET_PID"
print_color yellow "Waiting for RPC endpoints to become available..."

# Wait for RPC to become available
ENDPOINTS=(
    "http://localhost:26660"  # Directory Network
    "http://localhost:26670"  # Block Validator 0
    "http://localhost:26680"  # Block Validator 1
    "http://localhost:26690"  # Block Validator 2
)

START_TIME=$(date +%s)
READY=false

while [ $(($(date +%s) - START_TIME)) -lt $TIMEOUT_SECONDS ]; do
    # Check if process is still running
    if ! kill -0 "$DEVNET_PID" 2>/dev/null; then
        print_color red "Error: Devnet process exited unexpectedly"
        print_color yellow "Check log file for details: $LOG_FILE"
        exit 1
    fi

    # Test primary endpoint (Directory Network)
    if test_rpc_endpoint "${ENDPOINTS[0]}"; then
        print_color green "Directory Network RPC is ready: ${ENDPOINTS[0]}"
        READY=true
        break
    fi

    echo -n "."
    sleep 2
done

echo  # New line after dots

if [ "$READY" = false ]; then
    print_color red "Timeout: RPC endpoints did not become available within $TIMEOUT_SECONDS seconds"
    print_color yellow "Check log file for details: $LOG_FILE"
    exit 1
fi

echo
print_color cyan "=== Devnet Status ==="

# Test all endpoints
for endpoint in "${ENDPOINTS[@]}"; do
    if test_rpc_endpoint "$endpoint"; then
        print_color green "✓ $endpoint - Ready"
    else
        print_color yellow "✗ $endpoint - Not Ready"
    fi
done

echo
print_color green "=== Devnet Running Successfully! ==="
print_color white "Process ID: $DEVNET_PID"
print_color white "Log file: $LOG_FILE"
echo
print_color cyan "Primary endpoints:"
print_color white "  Directory Network: http://localhost:26660"
print_color white "  Block Validator 0: http://localhost:26670"
print_color white "  Block Validator 1: http://localhost:26680"
print_color white "  Block Validator 2: http://localhost:26690"
echo
print_color green "The devnet is now ready for use with Accumen!"
print_color yellow "Press Ctrl+C to stop the devnet"

# Wait for process to exit or signal
wait "$DEVNET_PID"

print_color cyan "Devnet session completed"