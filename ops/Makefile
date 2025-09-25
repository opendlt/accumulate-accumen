.PHONY: build test run-sequencer proto json clean

# Go build settings
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOMOD=$(GOCMD) mod
BINARY_NAME=accumen
BINARY_PATH=./bin/$(BINARY_NAME)

# Default target
all: build

# Build the binary
build:
	mkdir -p bin
	$(GOBUILD) -o $(BINARY_PATH) ./cmd/accumen

# Run tests
test:
	$(GOTEST) -v ./...

# Run as sequencer with example config
run-sequencer: build
	$(BINARY_PATH) --role=sequencer --config=./config/example.json

# Generate protobuf files (placeholder)
proto:
	@echo "Generating protobuf files..."
	@echo "TODO: Add protoc generation commands"

# Generate JSON schemas (placeholder)
json:
	@echo "Generating JSON schemas..."
	@echo "TODO: Add JSON schema generation commands"

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

# Run linter (requires golangci-lint)
lint:
	golangci-lint run

# Install tools
install-tools:
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest