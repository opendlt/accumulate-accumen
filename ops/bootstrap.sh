#!/bin/bash

# Accumen Bootstrap Script for Unix/Linux/macOS
# Installs dependencies and runs codegen stubs

set -euo pipefail

SKIP_TOOLS=false
VERBOSE=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-tools)
            SKIP_TOOLS=true
            shift
            ;;
        --verbose|-v)
            VERBOSE=true
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--skip-tools] [--verbose] [--help]"
            echo "  --skip-tools  Skip installing development tools"
            echo "  --verbose     Enable verbose output"
            echo "  --help        Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

if [[ "$VERBOSE" == "true" ]]; then
    set -x
fi

echo "🚀 Bootstrapping Accumen development environment..."

# Check Go installation
echo "Checking Go installation..."
if ! command -v go &> /dev/null; then
    echo "❌ Go not found. Please install Go 1.22+ from https://golang.org/dl/"
    exit 1
fi

GO_VERSION=$(go version | cut -d' ' -f3 | sed 's/go//')
echo "✅ Found Go version: $GO_VERSION"

# Verify Go version (basic check)
MAJOR_MINOR=$(echo "$GO_VERSION" | cut -d'.' -f1,2)
if [[ "$(echo -e "1.22\n$MAJOR_MINOR" | sort -V | head -n1)" != "1.22" ]]; then
    echo "❌ Go version $GO_VERSION found, but 1.22+ is required"
    exit 1
fi
echo "✅ Go version meets requirements"

# Install development tools
if [[ "$SKIP_TOOLS" != "true" ]]; then
    echo "Installing development tools..."

    TOOLS=(
        "github.com/golangci/golangci-lint/cmd/golangci-lint@latest"
        "google.golang.org/protobuf/cmd/protoc-gen-go@latest"
        "google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest"
    )

    for tool in "${TOOLS[@]}"; do
        echo "Installing $tool..."
        if go install "$tool"; then
            echo "✅ Installed $tool"
        else
            echo "⚠️  Failed to install $tool"
        fi
    done
fi

# Download Go dependencies
echo "Downloading Go dependencies..."
if ! go mod download; then
    echo "❌ Failed to download dependencies"
    exit 1
fi
echo "✅ Dependencies downloaded"

# Tidy go.mod
echo "Tidying go.mod..."
if ! go mod tidy; then
    echo "❌ Failed to tidy go.mod"
    exit 1
fi
echo "✅ go.mod tidied"

# Run codegen stubs (placeholder)
echo "Running codegen stubs..."
echo "TODO: Add protobuf generation"
echo "TODO: Add JSON schema generation"
echo "✅ Codegen stubs completed (placeholder)"

# Create example config directory
echo "Creating example configuration..."
mkdir -p config
cat > config/example.json << 'EOF'
{
  "node": {
    "role": "sequencer",
    "listen_addr": "0.0.0.0:8080"
  },
  "wasm": {
    "runtime": "wazero",
    "max_memory": "128MB"
  },
  "vdk": {
    "bridge_enabled": true,
    "accumulate_endpoint": "https://mainnet.accumulatenetwork.io"
  }
}
EOF
echo "✅ Created config/example.json"

# Test build
echo "Testing build..."
if ! make build; then
    echo "❌ Build failed"
    exit 1
fi
echo "✅ Build successful"

echo "🎉 Bootstrap completed successfully!"
echo ""
echo "Next steps:"
echo "  make run-sequencer    # Run as sequencer"
echo "  make test            # Run tests"
echo "  make -C ops help     # See all available targets"