#!/bin/bash

# Accumen JSON Schema Generation Tool
# Generates Go types from JSON schemas using various code generation tools

set -euo pipefail

# Default values
SCHEMA_DIR="types/json"
OUTPUT_DIR="types/generated/json"
PACKAGE="jsontypes"
VALIDATE=false
CLEAN=false
VERBOSE=false

# Parse command line arguments
show_help() {
    cat << 'EOF'
Accumen JSON Schema Generation Tool

USAGE:
    ./tools/gen-json.sh [OPTIONS]

OPTIONS:
    --schema-dir <path>     Directory containing JSON schema files (default: types/json)
    --output-dir <path>     Output directory for generated Go files (default: types/generated/json)
    --package <name>        Go package name for generated files (default: jsontypes)
    --validate              Validate schemas before generation
    --clean                 Clean output directory before generation
    --verbose               Enable verbose output
    --help                  Show this help message

EXAMPLES:
    ./tools/gen-json.sh                                         # Generate with defaults
    ./tools/gen-json.sh --validate --clean --verbose           # Full generation with validation
    ./tools/gen-json.sh --schema-dir custom/schemas            # Use custom schema directory

REQUIREMENTS:
    - Go 1.22+ (for code generation and formatting)
    - jq (for JSON validation and processing)
EOF
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --schema-dir)
            SCHEMA_DIR="$2"
            shift 2
            ;;
        --output-dir)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --package)
            PACKAGE="$2"
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

echo "🔧 Accumen JSON Schema Generation Tool"
echo "Schema Directory: $SCHEMA_DIR"
echo "Output Directory: $OUTPUT_DIR"
echo "Package Name: $PACKAGE"

# Check if schema directory exists
if [[ ! -d "$SCHEMA_DIR" ]]; then
    echo "❌ Schema directory not found: $SCHEMA_DIR"
    exit 1
fi

# Clean output directory if requested
if [[ "$CLEAN" == "true" && -d "$OUTPUT_DIR" ]]; then
    echo "🧹 Cleaning output directory..."
    rm -rf "$OUTPUT_DIR"
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Find all JSON schema files
mapfile -t schema_files < <(find "$SCHEMA_DIR" -name "*.schema.json" -type f)

if [[ ${#schema_files[@]} -eq 0 ]]; then
    echo "⚠️ No JSON schema files found in $SCHEMA_DIR"
    exit 0
fi

echo "📋 Found ${#schema_files[@]} schema file(s)"

# Check for required tools
required_tools=("go" "jq")
for tool in "${required_tools[@]}"; do
    if ! command -v "$tool" &> /dev/null; then
        echo "❌ Required tool not found: $tool"
        case $tool in
            go)
                echo "Please install Go from https://golang.org/dl/"
                ;;
            jq)
                echo "Please install jq: sudo apt-get install jq (Ubuntu/Debian) or brew install jq (macOS)"
                ;;
        esac
        exit 1
    fi
done

# Validate schemas if requested
if [[ "$VALIDATE" == "true" ]]; then
    echo "✅ Validating JSON schemas..."

    for file in "${schema_files[@]}"; do
        filename=$(basename "$file")
        echo "  Validating: $filename"

        # Validate JSON syntax
        if ! jq empty "$file" 2>/dev/null; then
            echo "❌ Invalid JSON in $filename"
            exit 1
        fi

        # Basic schema validation
        schema_prop=$(jq -r '."$schema" // empty' "$file")
        title_prop=$(jq -r '.title // empty' "$file")
        type_prop=$(jq -r '.type // empty' "$file")

        if [[ -z "$schema_prop" ]]; then
            echo "⚠️ Schema $filename missing '\$schema' property"
        fi

        if [[ -z "$title_prop" ]]; then
            echo "⚠️ Schema $filename missing 'title' property"
        fi

        if [[ -z "$type_prop" ]]; then
            echo "⚠️ Schema $filename missing 'type' property"
        fi

        echo "  ✓ $filename"
    done
fi

# Generate Go types from schemas
echo "🏗️ Generating Go types from JSON schemas..."

for file in "${schema_files[@]}"; do
    filename=$(basename "$file")
    base_name=$(basename "$file" .schema.json)
    output_file="$OUTPUT_DIR/$base_name.go"

    echo "  Processing: $filename -> $base_name.go"

    # Parse schema metadata
    title=$(jq -r '.title // "Unknown"' "$file")
    description=$(jq -r '.description // ""' "$file")

    # Generate struct name from title
    struct_name=$(echo "$title" | sed 's/[^A-Za-z0-9]//g')Metadata

    # Start generating Go file
    cat > "$output_file" << EOF
// Code generated from JSON schema. DO NOT EDIT.
// Source: $filename

package $PACKAGE

import (
    "encoding/json"
    "time"
)

// $struct_name represents the $title
EOF

    if [[ -n "$description" ]]; then
        echo "// $description" >> "$output_file"
    fi

    echo "type $struct_name struct {" >> "$output_file"

    # Generate struct fields from schema properties
    if jq -e '.properties' "$file" > /dev/null 2>&1; then
        jq -r '.properties | to_entries[] | @json' "$file" | while IFS= read -r prop_json; do
            prop_name=$(echo "$prop_json" | jq -r '.key')
            prop_data=$(echo "$prop_json" | jq -r '.value')
            prop_type=$(echo "$prop_data" | jq -r '.type // "string"')
            prop_format=$(echo "$prop_data" | jq -r '.format // empty')
            prop_desc=$(echo "$prop_data" | jq -r '.description // empty')

            # Convert prop_name to Go field name (camelCase)
            field_name=$(echo "$prop_name" | sed 's/_\(.\)/\U\1/g' | sed 's/^\(.\)/\U\1/')

            # Determine Go type
            go_type="string"
            case $prop_type in
                integer|number)
                    go_type="uint64"
                    ;;
                boolean)
                    go_type="bool"
                    ;;
                array)
                    go_type="[]interface{}"
                    ;;
                object)
                    go_type="map[string]interface{}"
                    ;;
                string)
                    if [[ "$prop_format" == "date-time" ]]; then
                        go_type="time.Time"
                    fi
                    ;;
            esac

            # Add field with description and JSON tag
            if [[ -n "$prop_desc" ]]; then
                echo "    // $prop_desc" >> "$output_file"
            fi
            echo "    $field_name $go_type \`json:\"$prop_name\"\`" >> "$output_file"
        done
    fi

    # Close struct and add methods
    cat >> "$output_file" << EOF
}

// Validate validates the $struct_name
func (m *$struct_name) Validate() error {
    // TODO: Add validation logic based on schema constraints
    return nil
}

// ToJSON converts the $struct_name to JSON
func (m *$struct_name) ToJSON() ([]byte, error) {
    return json.Marshal(m)
}

// FromJSON creates a $struct_name from JSON
func (m *$struct_name) FromJSON(data []byte) error {
    return json.Unmarshal(data, m)
}
EOF

    echo "  ✓ Generated $base_name.go"
done

# Generate package-level files
package_file="$OUTPUT_DIR/doc.go"
cat > "$package_file" << EOF
// Package $PACKAGE contains generated Go types from JSON schemas.
//
// This package is auto-generated from JSON schema files in the types/json directory.
// Do not edit these files directly. Instead, modify the source schemas and regenerate.
//
// Generated by: tools/gen-json.sh
// Generated at: $(date -u '+%Y-%m-%d %H:%M:%S UTC')
package $PACKAGE
EOF

echo "  ✓ Generated package documentation"

# Run go fmt on generated files
echo "🎨 Formatting generated Go code..."
if (cd "$OUTPUT_DIR" && go fmt ./...); then
    echo "  ✓ Code formatted successfully"
else
    echo "⚠️ Failed to format Go code"
fi

# Generate build metadata
build_file="$OUTPUT_DIR/build.go"
cat > "$build_file" << EOF
//go:build ignore
// +build ignore

package main

// Build information for generated JSON types
const (
    GeneratedAt = "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
    GeneratedBy = "tools/gen-json.sh"
    SchemaCount = ${#schema_files[@]}
)
EOF

echo "🎉 JSON schema generation completed successfully!"
echo ""
echo "Generated files:"
find "$OUTPUT_DIR" -name "*.go" -type f | while read -r file; do
    echo "  $(basename "$file")"
done

echo ""
echo "Next steps:"
echo "  go mod tidy              # Update dependencies"
echo "  go test ./$OUTPUT_DIR    # Test generated types"
echo "  go build ./$OUTPUT_DIR   # Build generated package"