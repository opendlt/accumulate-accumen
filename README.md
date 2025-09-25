# Accumen

Accumen is a high-performance L1 sequencer with WASM runtime capabilities and VDK bridge integration to the Accumulate L0 network.

## Architecture

- **L1 Sequencer**: High-throughput transaction sequencing and ordering
- **WASM Runtime**: Powered by wazero for secure and efficient smart contract execution
- **VDK Bridge**: Seamless integration with Accumulate L0 network infrastructure
- **Dual Modes**: Supports both sequencer and follower node operations

## Quick Start

### Prerequisites

- Go 1.22 or later
- PowerShell (Windows) or Bash (POSIX)
- Port 8666 available for RPC server

### End-to-End Smoke Test

Run the complete smoke test that exercises the full stack from WASM contract deployment through L0 metadata writing:

```bash
# Run smoke test with embedded Accumulate simulator
go test ./tests/e2e -run TestAccumenSmoke -v
```

### Manual Development Workflow

#### Windows:
```powershell
# 1. Start Accumen sequencer
powershell -ExecutionPolicy Bypass -File ops/run-accumen.ps1

# 2. Build CLI tool (in another terminal)
go build -o bin/accucli ./cmd/accucli

# 3. Check status
./bin/accucli status --rpc=http://127.0.0.1:8666

# 4. Submit transactions
./bin/accucli submit --contract=acc://counter.acme --entry=increment --arg=amount:1

# 5. Query state
./bin/accucli query --contract=acc://counter.acme --key=value
```

#### POSIX (Linux/macOS):
```bash
# 1. Start Accumen sequencer
go run ./cmd/accumen --role=sequencer --config=./config/local.yaml --rpc=:8666

# 2. Build CLI tool (in another terminal)
go build -o bin/accucli ./cmd/accucli

# 3. Check status
./bin/accucli status --rpc=http://127.0.0.1:8666

# 4. Submit transactions
./bin/accucli submit --contract=acc://counter.acme --entry=increment --arg=amount:1

# 5. Query state
./bin/accucli query --contract=acc://counter.acme --key=value
```

### Build and Test

```bash
# Build the binary
make build  # or: go build -o bin/accumen ./cmd/accumen

# Run unit tests
go test ./...

# Run integration tests
go test ./tests/harness/...

# Run end-to-end tests
go test ./tests/e2e/...
```

### Configuration

Create a configuration file (see `config/example.json` for reference):

```json
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
```

## Development

### Available Make Targets

- `make build` - Build the accumen binary
- `make test` - Run all tests
- `make run-sequencer` - Run node in sequencer mode
- `make proto` - Generate protobuf files (TODO)
- `make json` - Generate JSON schemas (TODO)
- `make clean` - Clean build artifacts
- `make fmt` - Format Go code
- `make lint` - Run linter (requires golangci-lint)

### Project Structure

```
accumulate-accumen/
├── cmd/accumen/          # Main application entry point
├── ops/                  # Build and deployment scripts
│   ├── Makefile         # Build automation
│   ├── bootstrap.sh     # Unix bootstrap script
│   └── bootstrap.ps1    # Windows bootstrap script
├── config/              # Configuration files
└── README.md           # This file
```

## Documentation

- [Architecture Overview](docs/ARCHITECTURE.md) - System design and components
- [Operations Runbook](docs/RUNBOOK.md) - Deployment and troubleshooting guide
- [Claude Code Notes](CLAUDE.md) - Development workflow and build commands

## SDK and Examples

### Rust SDK
- [Accumen ABI](sdk/rust/accumen-abi/) - Rust bindings for WASM contracts
- [Counter Example](sdk/rust/examples/counter/) - Simple counter smart contract

### Building Contracts
```bash
# Build Rust contract to WASM
cd sdk/rust/examples/counter
cargo build --target wasm32-unknown-unknown --release
```

## Testing

- Unit tests: `make test`
- Integration tests: `make test-integration`
- End-to-end tests: `make test-e2e`

## Contributing

This is an MVP implementation. Future development will include:

- Complete WASM runtime integration
- VDK bridge implementation
- Advanced sequencing algorithms
- Comprehensive test coverage
- Production deployment configurations

## License

TBD