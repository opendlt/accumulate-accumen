#!/bin/bash

# Accumen Protobuf Code Generation Tool
# Generates Go code from protobuf definitions using protoc and protoc-gen-go

set -euo pipefail

# Default values
PROTO_DIR="types/proto"
OUTPUT_DIR="types/generated/proto"
VALIDATE=false
CLEAN=false
VERBOSE=false

# Parse command line arguments
show_help() {
    cat << 'EOF'
Accumen Protobuf Code Generation Tool

USAGE:
    ./tools/gen-proto.sh [OPTIONS]

OPTIONS:
    --proto-dir <path>      Directory containing .proto files (default: types/proto)
    --output-dir <path>     Output directory for generated Go files (default: types/generated/proto)
    --validate              Validate protobuf files before generation
    --clean                 Clean output directory before generation
    --verbose               Enable verbose output
    --help                  Show this help message

EXAMPLES:
    ./tools/gen-proto.sh                                         # Generate with defaults
    ./tools/gen-proto.sh --validate --clean --verbose           # Full generation with validation
    ./tools/gen-proto.sh --proto-dir custom/proto              # Use custom proto directory

REQUIREMENTS:
    - protoc (Protocol Buffer compiler)
    - protoc-gen-go (Go plugin for protoc)
    - Go 1.22+ (for code generation and formatting)
EOF
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --proto-dir)
            PROTO_DIR="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --validate)
            VALIDATE=true
            shift
            ;;
        --clean)
            CLEAN=true
            shift
            ;;
        --verbose)
            VERBOSE=true
            shift
            ;;
        --help)
            show_help
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            show_help
            exit 1
            ;;
    esac
done

if [[ "$VERBOSE" == "true" ]]; then
    set -x
fi

echo "🔧 Accumen Protobuf Code Generation Tool"
echo "Proto Directory: $PROTO_DIR"
echo "Output Directory: $OUTPUT_DIR"

# Check if proto directory exists
if [[ ! -d "$PROTO_DIR" ]]; then
    echo "❌ Proto directory not found: $PROTO_DIR"
    exit 1
fi

# Clean output directory if requested
if [[ "$CLEAN" == "true" && -d "$OUTPUT_DIR" ]]; then
    echo "🧹 Cleaning output directory..."
    rm -rf "$OUTPUT_DIR"
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Find all .proto files
mapfile -t proto_files < <(find "$PROTO_DIR" -name "*.proto" -type f)

if [[ ${#proto_files[@]} -eq 0 ]]; then
    echo "⚠️ No .proto files found in $PROTO_DIR"
    exit 0
fi

echo "📋 Found ${#proto_files[@]} proto file(s)"

# Check for required tools
required_tools=("protoc" "protoc-gen-go")
for tool in "${required_tools[@]}"; do
    if ! command -v "$tool" &> /dev/null; then
        echo "❌ Required tool not found: $tool"
        case $tool in
            protoc)
                echo "Please install protoc from https://grpc.io/docs/protoc-installation/"
                ;;
            protoc-gen-go)
                echo "Please install protoc-gen-go: go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
                ;;
        esac
        exit 1
    fi
done

# Validate proto files if requested
if [[ "$VALIDATE" == "true" ]]; then
    echo "✅ Validating protobuf files..."

    for file in "${proto_files[@]}"; do
        filename=$(basename "$file")
        echo "  Validating: $filename"

        # Create temporary descriptor file for validation
        temp_descriptor=$(mktemp)
        trap "rm -f $temp_descriptor" EXIT

        # Run protoc with --descriptor_set_out to validate syntax
        if ! protoc \
            --descriptor_set_out="$temp_descriptor" \
            --proto_path="$PROTO_DIR" \
            "$file" 2>/dev/null; then
            echo "❌ Validation failed for $filename"
            exit 1
        fi

        echo "  ✓ $filename"
    done
fi

# Get the module path from go.mod
module_name=""
if [[ -f "go.mod" ]]; then
    module_name=$(grep "^module " go.mod | head -1 | awk '{print $2}')
fi

if [[ -z "$module_name" ]]; then
    echo "⚠️ Could not determine module name from go.mod, using default"
    module_name="github.com/opendlt/accumulate-accumen"
fi

# Generate Go code from proto files
echo "🏗️ Generating Go code from protobuf files..."

for file in "${proto_files[@]}"; do
    filename=$(basename "$file")
    echo "  Processing: $filename"

    # Calculate relative path from proto directory
    file_dir=$(dirname "$file")
    rel_path=$(realpath --relative-to="$PROTO_DIR" "$file_dir")
    output_subdir="$OUTPUT_DIR/$rel_path"

    # Create subdirectory if needed
    mkdir -p "$output_subdir"

    # Generate protoc arguments
    protoc_args=(
        "--go_out=$OUTPUT_DIR"
        "--go_opt=paths=source_relative"
        "--go_opt=module=$module_name/${OUTPUT_DIR//\\/\/}"
        "--proto_path=$PROTO_DIR"
    )

    # Add common proto paths for well-known types
    if [[ -n "${GOPATH:-}" ]]; then
        protoc_args+=("--proto_path=$GOPATH/src")
    fi
    if [[ -n "${GOROOT:-}" ]]; then
        protoc_args+=("--proto_path=$GOROOT/src")
    fi

    protoc_args+=("$file")

    if ! protoc "${protoc_args[@]}"; then
        echo "❌ Failed to generate code for $filename"
        exit 1
    fi

    echo "  ✓ Generated code for $filename"
done

# Run go fmt on generated files
echo "🎨 Formatting generated Go code..."
if (cd "$OUTPUT_DIR" && go fmt ./...); then
    echo "  ✓ Code formatted successfully"
else
    echo "⚠️ Failed to format some Go files"
fi

# Generate go.mod for the generated package if it doesn't exist
generated_go_mod="$OUTPUT_DIR/go.mod"
if [[ ! -f "$generated_go_mod" ]]; then
    generated_module_name="$module_name/${OUTPUT_DIR//\\/\/}"
    cat > "$generated_go_mod" << EOF
module $generated_module_name

go 1.22

require (
    google.golang.org/protobuf v1.31.0
)
EOF
    echo "  ✓ Generated go.mod for protobuf package"
fi

# Generate package documentation
doc_file="$OUTPUT_DIR/doc.go"
cat > "$doc_file" << EOF
// Package proto contains generated Go types from protobuf definitions.
//
// This package is auto-generated from .proto files in the types/proto directory.
// Do not edit these files directly. Instead, modify the source .proto files and regenerate.
//
// Generated by: tools/gen-proto.sh
// Generated at: $(date -u '+%Y-%m-%d %H:%M:%S UTC')
package proto
EOF

echo "  ✓ Generated package documentation"

# Generate build metadata
build_file="$OUTPUT_DIR/build.go"
cat > "$build_file" << EOF
//go:build ignore
// +build ignore

package main

// Build information for generated protobuf types
const (
    GeneratedAt = "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    GeneratedBy = "tools/gen-proto.sh"
    ProtoCount = ${#proto_files[@]}
)
EOF

echo "🎉 Protobuf code generation completed successfully!"
echo ""
echo "Generated files:"
find "$OUTPUT_DIR" -name "*.go" -type f | while read -r file; do
    rel_file=$(realpath --relative-to="$OUTPUT_DIR" "$file")
    echo "  $rel_file"
done

echo ""
echo "Next steps:"
echo "  go mod tidy              # Update dependencies"
echo "  go test ./$OUTPUT_DIR    # Test generated types"
echo "  go build ./$OUTPUT_DIR   # Build generated package"