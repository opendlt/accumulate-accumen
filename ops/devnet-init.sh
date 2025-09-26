#!/bin/bash
#
# Initialize Accumulate devnet in a temporary directory
# This script initializes the devnet configuration and prints RPC endpoints
#

set -e

# Default work directory
WORK_DIR=""

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --work-dir)
            WORK_DIR="$2"
            shift 2
            ;;
        -h|--help)
            echo "Usage: $0 [--work-dir <directory>]"
            echo ""
            echo "Options:"
            echo "  --work-dir    Directory to initialize devnet in (default: temp directory)"
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
        *) echo "$message" ;;
    esac
}

# Create work directory if not specified
if [ -z "$WORK_DIR" ]; then
    WORK_DIR=$(mktemp -d -t accumulate-devnet-$(date +%Y%m%d-%H%M%S)-XXXXXX)
fi

print_color cyan "=== Accumulate Devnet Initialization ==="
print_color yellow "Work directory: $WORK_DIR"

# Create work directory if it doesn't exist
if [ ! -d "$WORK_DIR" ]; then
    mkdir -p "$WORK_DIR"
    print_color green "Created work directory: $WORK_DIR"
fi

# Change to work directory
cd "$WORK_DIR"

print_color yellow "Initializing Accumulate devnet..."

# Run accumulated init devnet
if ! INIT_OUTPUT=$(go run gitlab.com/accumulatenetwork/accumulate/cmd/accumulated init devnet 2>&1); then
    print_color red "Error: Failed to initialize devnet"
    print_color red "$INIT_OUTPUT"
    exit 1
fi

print_color green "Devnet initialized successfully!"
echo "$INIT_OUTPUT"

echo
print_color cyan "=== RPC Endpoints ==="

# Parse and extract RPC endpoints from the configuration
CONFIG_PATH="$WORK_DIR/.accumulate/config.toml"

if [ -f "$CONFIG_PATH" ]; then
    print_color yellow "Configuration file: $CONFIG_PATH"

    # Extract RPC addresses from config
    RPC_ENDPOINTS=$(grep -A 5 '\[.*rpc.*\]' "$CONFIG_PATH" | grep 'listen.*=' | sed 's/.*listen.*=.*"\([^"]*\)".*/http:\/\/\1/' || true)

    if [ -n "$RPC_ENDPOINTS" ]; then
        echo "$RPC_ENDPOINTS" | while read -r endpoint; do
            print_color green "RPC Endpoint: $endpoint"
        done
    else
        print_color yellow "Warning: No RPC endpoints found in config"
        print_color white "Default endpoints:"
        print_color white "  Directory Network: http://localhost:26660"
        print_color white "  Block Validator 0: http://localhost:26670"
        print_color white "  Block Validator 1: http://localhost:26680"
        print_color white "  Block Validator 2: http://localhost:26690"
    fi
else
    print_color yellow "Warning: Configuration file not found at $CONFIG_PATH"
    print_color white "Default endpoints:"
    print_color white "  Directory Network: http://localhost:26660"
    print_color white "  Block Validator 0: http://localhost:26670"
    print_color white "  Block Validator 1: http://localhost:26680"
    print_color white "  Block Validator 2: http://localhost:26690"
fi

echo
print_color cyan "=== Configuration for Accumen ==="
print_color white "Add these endpoints to your Accumen config:"
echo
print_color white "l0:"
print_color white "  source: static"
print_color white "  static:"
print_color white "    endpoints:"
print_color white "      - http://localhost:26660  # Directory Network"
print_color white "      - http://localhost:26670  # Block Validator 0"
print_color white "      - http://localhost:26680  # Block Validator 1"
print_color white "      - http://localhost:26690  # Block Validator 2"
echo
print_color cyan "Next steps:"
print_color white "1. Run: ./ops/devnet-run.sh --work-dir '$WORK_DIR'"
print_color white "2. Update your Accumen configuration with the endpoints above"
print_color white "3. Start Accumen with the updated configuration"

echo
print_color green "Devnet initialization completed!"
print_color yellow "Work directory: $WORK_DIR"