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
- Make (for build automation)

### Bootstrap Development Environment

**Unix/Linux/macOS:**
```bash
chmod +x ops/bootstrap.sh
./ops/bootstrap.sh
```

**Windows (PowerShell):**
```powershell
Set-ExecutionPolicy -ExecutionPolicy RemoteSigned -Scope CurrentUser
./ops/bootstrap.ps1
```

### Build and Run

```bash
# Build the binary
make build

# Run as sequencer
make run-sequencer

# Run as follower
./bin/accumen --role=follower --config=./config/example.json

# Run tests
make test
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