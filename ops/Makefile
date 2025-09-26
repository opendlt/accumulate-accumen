.PHONY: build test run-sequencer proto json clean run-sim snapshot-export snapshot-import snapshot-list

# Go build settings
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
BINARY_NAME=accumen
BINARY_PATH=./bin/$(BINARY_NAME)

# Detect OS for cross-platform commands
ifeq ($(OS),Windows_NT)
    DETECTED_OS := Windows
    RUN_ACCUMEN := powershell -ExecutionPolicy Bypass -File ops/run-accumen.ps1
    RUN_SIM := powershell -ExecutionPolicy Bypass -File ops/run-sim.ps1
else
    DETECTED_OS := $(shell uname -s)
    RUN_ACCUMEN := $(GOCMD) run ./cmd/accumen --role=sequencer --config=./config/local.yaml --rpc=:8666
    RUN_SIM := @echo "Simulator runner not yet implemented for POSIX. Use: cd test/simulator && go test -v -run TestSimulator"
endif

# Default target
all: build

# Build the binary
build:
	mkdir -p bin
	$(GOBUILD) -o $(BINARY_PATH) ./cmd/accumen

# Run tests
test:
	$(GOTEST) -v ./...

# Run as sequencer with local config (OS-aware)
run-sequencer:
	@echo "Starting Accumen sequencer on $(DETECTED_OS)..."
	$(RUN_ACCUMEN)

# Run Accumulate simulator for testing
run-sim:
	@echo "Accumulate simulator setup for $(DETECTED_OS)..."
	$(RUN_SIM)

# Generate protobuf files
proto:
	@echo "Generating protobuf files..."
	./tools/gen-proto.sh --proto-dir types/proto --output-dir types/generated/proto

# Generate JSON schemas
json:
	@echo "Generating JSON schemas..."
	./tools/gen-json.sh --schema-dir types/json --output-dir types/generated/json

# Generate both proto and json
generate: proto json

# Export snapshot
snapshot-export:
	@echo "Exporting snapshot..."
	$(GOBUILD) -o $(BINARY_PATH) ./cmd/accucli
	./bin/accumen snapshot export --out ./snapshots/manual-export-$(shell powershell -Command "Get-Date -Format 'yyyy-MM-dd-HHmmss'").snap

# Import snapshot
snapshot-import:
	@echo "Importing snapshot..."
	$(GOBUILD) -o $(BINARY_PATH) ./cmd/accucli
	@if [ -z "$(FILE)" ]; then echo "Usage: make snapshot-import FILE=path/to/snapshot.snap"; exit 1; fi
	./bin/accumen snapshot import --in $(FILE)

# List snapshots
snapshot-list:
	@echo "Listing snapshots..."
	$(GOBUILD) -o $(BINARY_PATH) ./cmd/accucli
	./bin/accumen snapshot list

# Integration tests
test-integration:
	$(GOTEST) -v ./tests/harness/...

# End-to-end tests
test-e2e:
	$(GOTEST) -v ./tests/e2e/...

# Build for release
build-release:
	mkdir -p bin
	CGO_ENABLED=0 $(GOBUILD) -ldflags="-w -s" -o $(BINARY_PATH) ./cmd/accumen

# Clean build artifacts
clean:
	rm -rf bin/
	$(GOMOD) tidy

# Download dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

# Format code
fmt:
	$(GOCMD) fmt ./...

# Run go vet
vet:
	$(GOCMD) vet ./...

# Run linter (requires golangci-lint)
lint:
	golangci-lint run

# Install tools
install-tools:
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Build with formatting and vetting
build-all: fmt vet build

# Quick development targets
dev: build run-sequencer

# Show available targets
help:
	@echo "Available targets:"
	@echo "  build              Build the binary"
	@echo "  build-all          Format, vet, and build"
	@echo "  test               Run tests"
	@echo "  run-sequencer      Run Accumen sequencer locally"
	@echo "  run-sim            Setup/run Accumulate simulator"
	@echo "  proto              Generate protobuf files"
	@echo "  json               Generate JSON schemas"
	@echo "  generate           Generate both proto and json"
	@echo "  test-integration   Run integration tests"
	@echo "  test-e2e           Run end-to-end tests"
	@echo "  build-release      Build optimized release binary"
	@echo "  clean              Clean build artifacts"
	@echo "  deps               Download and tidy dependencies"
	@echo "  fmt                Format code"
	@echo "  vet                Run go vet"
	@echo "  lint               Run linter"
	@echo "  install-tools      Install development tools"
	@echo "  dev                Build and run sequencer"
	@echo "  snapshot-export    Export current state to snapshot"
	@echo "  snapshot-import    Import state from snapshot (FILE=path required)"
	@echo "  snapshot-list      List available snapshots"
	@echo "  help               Show this help message"