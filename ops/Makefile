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

# Run as sequencer with local config
run-sequencer: build
	$(BINARY_PATH) --role=sequencer --config=./config/local.yaml

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

# Run linter (requires golangci-lint)
lint:
	golangci-lint run

# Install tools
install-tools:
	$(GOCMD) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest