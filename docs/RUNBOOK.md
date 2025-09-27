# Accumen Operations Runbook

This runbook provides operational procedures for running, monitoring, and troubleshooting Accumen in production environments.

## End-to-End Smoke Test (Manual)

This section describes how to manually run the complete end-to-end smoke test that exercises the full Accumen stack from WASM contract deployment through L0 metadata writing.

### 1. Start Accumulate Simulator

The smoke test includes an embedded Accumulate simulator that provides the L0 network endpoints. Run the smoke test to start the simulator:

```bash
# Run the smoke test which includes the simulator
go test ./tests/e2e -run TestAccumenSmoke -v
```

This will:
- Start a mock Accumulate simulator (1 DN / 1 BVN)
- Initialize registry and DN placeholder accounts
- Deploy a counter WASM contract
- Invoke increment operations 3 times
- Verify metadata writes to DN
- Assert all operations completed successfully

**Expected Output:**
```
=== RUN   TestAccumenSmoke
    accumen_e2e_test.go:20: 🚀 Starting Accumen E2E Smoke Test
    integration.go:xxx: 🎯 Starting Accumen Smoke Test
    integration.go:xxx: ✅ Simulator started on port 26660
    integration.go:xxx: ✅ Sequencer started successfully
    integration.go:xxx: ✅ Counter deployed at: acc://counter.acme
    integration.go:xxx: ✅ Counter invoked 3 times
    integration.go:xxx: ✅ All transactions processed
    integration.go:xxx: ✅ Metadata written to DN (3 calls)
    accumen_e2e_test.go:22: ✅ Accumen E2E Smoke Test completed successfully
--- PASS: TestAccumenSmoke (5.23s)
```

### 2. VDK Runner

Accumen supports running with VDK (Validator Development Kit) for enhanced node lifecycle management and logging integration.

#### Building with VDK Support

Build the VDK-enabled binary with the appropriate build tag:

```bash
# Build VDK-enabled runner
go build -tags=withvdk -o bin/accumen-vdk ./cmd/accumen-vdk
```

#### Running with VDK

```bash
# Start sequencer with VDK lifecycle management
./bin/accumen-vdk \
  --config=config/local.yaml \
  --rpc=:8666 \
  --metrics=:8667 \
  --log-level=info
```

#### VDK Features

- **Lifecycle Management**: VDK handles graceful startup and shutdown
- **Logging Integration**: Automatic integration with VDK's structured logging
- **Service Discovery**: VDK node registration and health reporting
- **Monitoring**: Enhanced observability through VDK's monitoring framework

#### Building without VDK

If built without the `withvdk` tag, the binary will display a helpful error message:

```bash
# Standard build (no VDK)
go build -o bin/accumen-vdk ./cmd/accumen-vdk

# Running will show error
./bin/accumen-vdk --config=config/local.yaml
# Error: VDK support not available - this binary was built without -tags=withvdk
```

### 3. Run Accumen L1 (Manual Mode)

For manual testing and development, start the Accumen sequencer separately:

#### Windows:
```powershell
# Start the sequencer with local configuration
powershell -ExecutionPolicy Bypass -File ops/run-accumen.ps1
```

#### POSIX (Linux/macOS):
```bash
# Start the sequencer directly
go run ./cmd/accumen --role=sequencer --config=./config/local.yaml --rpc=:8666
```

**Expected Output:**
```
🚀 Starting Accumen Node...
   Role: sequencer
   Config: config/local.yaml
   RPC: :8666
   Log Level: info

✅ Go detected: go1.22.0
📋 Configuration Summary:
   Config file: config/local.yaml

🔧 Build and run command:
   go run ./cmd/accumen --role=sequencer --config=config/local.yaml --rpc=:8666 --log-level=info

▶️  Starting Accumen...

INFO[2024-01-15T10:30:00Z] Starting Accumen sequencer
INFO[2024-01-15T10:30:00Z] RPC server listening on :8666
INFO[2024-01-15T10:30:00Z] Sequencer running with 1s block time
```

### 3. Use accucli to Submit Increment

Once the sequencer is running, use the CLI tool to interact with it:

#### Build and Install CLI:
```bash
# Build the CLI tool
go build -o bin/accucli ./cmd/accucli

# Or run directly
go run ./cmd/accucli --help
```

#### Check Sequencer Status:
```bash
# Check if sequencer is running
./bin/accucli status --rpc=http://127.0.0.1:8666
```

**Expected Output:**
```
Chain ID: accumen-local
Height: 15
Running: true
Last Anchor: 2024-01-15T10:35:00Z
```

#### Deploy Counter Contract:
First, you need a deployed counter contract. The smoke test deploys one automatically, but for manual testing:

```bash
# Submit a deploy transaction (example)
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://registry.acme \
  --entry=deploy \
  --arg=name:counter \
  --arg=code:./examples/counter.wasm
```

#### Invoke Counter Increment:
```bash
# Submit increment transaction to deployed counter
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --entry=increment \
  --arg=amount:1
```

**Expected Output:**
```
Transaction Hash: 0x1234567890abcdef...
Status: pending
Block Height: 16
```

#### Query Counter State:
```bash
# Check the current counter value
./bin/accucli query \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --key=value
```

**Expected Output:**
```
Contract: acc://counter.acme
Key: value
Exists: true
Value: 3
Type: uint64
```

### 4. Where to See Metadata on DN

Accumen writes L1 transaction metadata to the Accumulate L0 network via WriteData operations. The metadata locations are configured in `config/local.yaml`:

#### Configuration:
```yaml
# L0 Directory Network paths for metadata storage
dnPaths:
  metadata: "acc://accumen-metadata.acme"
  anchors: "acc://accumen-anchors.acme"
```

#### Metadata Structure:
Each L1 transaction generates metadata written to the DN with the following URL pattern:
```
acc://accumen-metadata.acme/YYYY/MM/DD/block-{height}-tx-{index}-{random}.json
```

Example metadata URLs:
- `acc://accumen-metadata.acme/2024/01/15/block-16-tx-0-a1b2c3d4.json`
- `acc://accumen-metadata.acme/2024/01/15/block-16-tx-1-e5f6g7h8.json`

#### Metadata Content:
```json
{
  "version": "1.0",
  "chainId": "accumen-local",
  "blockHeight": 16,
  "blockTime": "2024-01-15T10:35:15Z",
  "txIndex": 0,
  "txHash": "0x1234567890abcdef...",
  "contract": "acc://counter.acme",
  "entry": "increment",
  "args": {"amount": "1"},
  "events": [
    {"type": "counter.incremented", "data": {"old": 2, "new": 3}}
  ],
  "stagedOps": [],
  "gasUsed": 1000,
  "signature": "0xabcdef1234567890..."
}
```

#### Viewing Metadata:
To view the metadata written to the DN:

1. **Via Accumulate CLI** (if available):
   ```bash
   accumulate account get acc://accumen-metadata.acme/2024/01/15/block-16-tx-0-a1b2c3d4.json
   ```

2. **Via L0 API** (direct query):
   ```bash
   curl -X POST http://localhost:26660/v3 \
     -H "Content-Type: application/json" \
     -d '{
       "id": 1,
       "method": "query",
       "params": {
         "url": "acc://accumen-metadata.acme/2024/01/15/block-16-tx-0-a1b2c3d4.json"
       }
     }'
   ```

3. **In Test Output**: The smoke test logs DN writer calls:
   ```
   INFO[2024-01-15T10:35:15Z] Writing metadata to DN                       url=acc://accumen-metadata.acme/2024/01/15/block-16-tx-0-a1b2c3d4.json
   INFO[2024-01-15T10:35:15Z] Metadata written successfully               calls=3 total_bytes=2048
   ```

## Run a Real Devnet

For development and testing purposes, you can run a real Accumulate devnet locally. This provides a more realistic environment compared to the embedded simulator.

### Prerequisites

- Go 1.21 or later installed
- Network connectivity to download Accumulate dependencies
- At least 4GB of free disk space
- Ports 26660, 26670, 26680, 26690 available

### Initialize Devnet

Use the provided scripts to initialize the devnet configuration:

#### Windows (PowerShell)
```powershell
# Initialize devnet in a temporary directory
.\ops\devnet-init.ps1

# Or specify a custom directory
.\ops\devnet-init.ps1 -WorkDir "C:\accumulate-devnet"
```

#### POSIX (Linux/macOS)
```bash
# Initialize devnet in a temporary directory
./ops/devnet-init.sh

# Or specify a custom directory
./ops/devnet-init.sh --work-dir /tmp/accumulate-devnet
```

**Expected Output:**
```
=== Accumulate Devnet Initialization ===
Work directory: /tmp/accumulate-devnet-20240115-143022

Initializing Accumulate devnet...
Devnet initialized successfully!

=== RPC Endpoints ===
Configuration file: /tmp/accumulate-devnet-20240115-143022/.accumulate/config.toml
RPC Endpoint: http://localhost:26660
RPC Endpoint: http://localhost:26670
RPC Endpoint: http://localhost:26680
RPC Endpoint: http://localhost:26690

=== Configuration for Accumen ===
Add these endpoints to your Accumen config:

l0:
  source: static
  static:
    endpoints:
      - http://localhost:26660  # Directory Network
      - http://localhost:26670  # Block Validator 0
      - http://localhost:26680  # Block Validator 1
      - http://localhost:26690  # Block Validator 2
```

### Run Devnet

Start the initialized devnet:

#### Windows (PowerShell)
```powershell
# Run devnet (using work directory from init script)
.\ops\devnet-run.ps1 -WorkDir "C:\temp\accumulate-devnet-20240115-143022"
```

#### POSIX (Linux/macOS)
```bash
# Run devnet (using work directory from init script)
./ops/devnet-run.sh --work-dir /tmp/accumulate-devnet-20240115-143022
```

**Expected Output:**
```
=== Accumulate Devnet Runner ===
Work directory: /tmp/accumulate-devnet-20240115-143022
Log file: logs/devnet-20240115-143045.log

Starting Accumulate devnet...
Devnet process started with PID: 12345
Waiting for RPC endpoints to become available...
..........
Directory Network RPC is ready: http://localhost:26660

=== Devnet Status ===
✓ http://localhost:26660 - Ready
✓ http://localhost:26670 - Ready
✓ http://localhost:26680 - Ready
✓ http://localhost:26690 - Ready

=== Devnet Running Successfully! ===
Process ID: 12345
Log file: logs/devnet-20240115-143045.log

Primary endpoints:
  Directory Network: http://localhost:26660
  Block Validator 0: http://localhost:26670
  Block Validator 1: http://localhost:26680
  Block Validator 2: http://localhost:26690

The devnet is now ready for use with Accumen!
Press Ctrl+C to stop the devnet
```

### Configure Accumen for Devnet

Update your Accumen configuration to use the devnet endpoints:

```yaml
# config/devnet.yaml
l0:
  source: static
  static:
    endpoints:
      - http://localhost:26660  # Directory Network
      - http://localhost:26670  # Block Validator 0
      - http://localhost:26680  # Block Validator 1
      - http://localhost:26690  # Block Validator 2

# Enable events monitoring for confirmations
events:
  enable: true
  reconnectMin: "1s"
  reconnectMax: "30s"

# Bridge configuration for devnet
bridge:
  enableBridge: true
  client:
    serverURL: "http://localhost:26660"
    sequencerKey: "your-key-here"

# Faster block times for development
blockTime: "2s"
maxTransactions: 100
```

### Start Accumen with Devnet

```bash
# Start Accumen sequencer with devnet configuration
go run ./cmd/accumen \
  --role=sequencer \
  --config=config/devnet.yaml \
  --rpc=:8666 \
  --log-level=debug
```

### Verify Integration

Test the integration between Accumen and the devnet:

```bash
# 1. Deploy a test contract
./bin/accucli deploy \
  --addr=acc://test-counter.acme \
  --wasm=./examples/counter.wasm \
  --rpc=http://localhost:8666

# 2. Invoke the contract
./bin/accucli submit \
  --contract=acc://test-counter.acme \
  --entry=increment \
  --arg=amount:1 \
  --rpc=http://localhost:8666

# 3. Check Accumulate DN for metadata
curl -X POST http://localhost:26660/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "query",
    "params": {
      "url": "acc://accumen.acme"
    }
  }'
```

### Script Options

#### devnet-init Options

**PowerShell:**
```powershell
.\ops\devnet-init.ps1 [-WorkDir <path>] [-Help]
```

**Bash:**
```bash
./ops/devnet-init.sh [--work-dir <path>] [--help]
```

#### devnet-run Options

**PowerShell:**
```powershell
.\ops\devnet-run.ps1 -WorkDir <path> [-LogDir <path>] [-TimeoutSeconds <int>]
```

**Bash:**
```bash
./ops/devnet-run.sh --work-dir <path> [--log-dir <path>] [--timeout <seconds>]
```

### Troubleshooting Devnet

#### Common Issues

1. **Port Already in Use:**
   ```bash
   # Check what's using the ports
   netstat -tlnp | grep -E '2666[0-9]'

   # Kill conflicting processes
   sudo fuser -k 26660/tcp
   ```

2. **RPC Not Ready:**
   - Wait longer for initialization (can take 30-60 seconds)
   - Check log file for errors
   - Verify Go version compatibility

3. **Connection Refused:**
   ```bash
   # Test direct connection
   curl http://localhost:26660/status

   # Check firewall settings
   sudo ufw status
   ```

#### Log Analysis

```bash
# Monitor devnet logs
tail -f logs/devnet-*.log

# Search for errors
grep -i error logs/devnet-*.log

# Check for specific components
grep -E "(DN|BVN)" logs/devnet-*.log
```

### Cleanup

To clean up the devnet:

1. **Stop the devnet**: Press `Ctrl+C` in the terminal running devnet-run
2. **Remove work directory**: `rm -rf /path/to/workdir` (if desired)
3. **Clean logs**: `rm -f logs/devnet-*.log` (optional)

### Benefits of Real Devnet

Compared to the embedded simulator, a real devnet provides:

- **Multi-node consensus**: Full BFT consensus with multiple validators
- **Network persistence**: State persists across restarts
- **Production-like behavior**: More realistic transaction processing
- **Full L0 API**: Complete Accumulate API compatibility
- **Event monitoring**: Real WebSocket events for confirmations
- **DN integration**: Actual Directory Network for metadata storage

This makes it ideal for integration testing and development workflows that require a production-like environment.

## Run Devnet Smoke Test

For comprehensive end-to-end testing against a real Accumulate devnet, use the devnet smoke test. This test validates the entire Accumen stack including contract deployment, transaction submission, state queries, and DN metadata writes.

### Prerequisites

- Real Accumulate devnet running (see "Run a Real Devnet" section above)
- Accumen sequencer running with proper configuration
- Signer configured in `config/local.yaml`
- L0 endpoints configured for devnet access

### Configuration Requirements

Ensure your `config/local.yaml` has the following configured:

```yaml
# L0 endpoints for devnet
l0:
  source: static
  static:
    endpoints:
      - http://localhost:26660  # Directory Network
      - http://localhost:26670  # Block Validator 0
      - http://localhost:26680  # Block Validator 1
      - http://localhost:26690  # Block Validator 2

# Signer configuration (required)
signer:
  type: "file"
  key: "/path/to/your/private-key.hex"

# Events monitoring (optional but recommended)
events:
  enable: true
  reconnectMin: "1s"
  reconnectMax: "30s"

# Bridge configuration for L0 integration
bridge:
  enableBridge: true
  client:
    serverURL: "http://localhost:26660"

# DN paths for metadata verification
dnPaths:
  anchorsBase: "acc://dn.acme/anchors"
  txBase: "acc://dn.acme/tx-metadata"
```

### Running the Smoke Test

#### Windows (PowerShell)

```powershell
# Set environment variable and run test
$env:ACC_DEVNET = "1"
go test ./tests/e2e -run TestAccumenDevnetSmoke -v -timeout 10m
```

#### POSIX (Linux/macOS)

```bash
# Set environment variable and run test
ACC_DEVNET=1 go test ./tests/e2e -run TestAccumenDevnetSmoke -v -timeout 10m
```

#### From Specific Directory

```bash
# Run from project root
ACC_DEVNET=1 go test -C ./tests/e2e -run TestAccumenDevnetSmoke -v -timeout 10m

# Or change to tests directory first
cd tests/e2e
ACC_DEVNET=1 go test -run TestAccumenDevnetSmoke -v -timeout 10m
```

### Expected Test Flow

The smoke test performs the following operations in sequence:

1. **Sequencer Status Check**
   - Verifies Accumen sequencer is running and responsive
   - Checks RPC endpoint connectivity

2. **Contract Deployment**
   - Deploys a minimal counter contract
   - Verifies successful deployment with transaction hash

3. **Event Monitoring Setup**
   - Sets up polling for transaction status
   - Prepares to monitor blockchain progression

4. **Transaction Submissions**
   - Submits 2 increment transactions to the counter
   - Records transaction hashes for verification

5. **Execution Monitoring**
   - Waits for transactions to be processed
   - Monitors block height progression

6. **State Verification**
   - Queries contract state to verify counter value
   - Expects counter to equal 2 (from 2 increments)

7. **DN Metadata Verification**
   - Checks for metadata writes to the Directory Network
   - Verifies L0 integration is working

8. **Final Status Check**
   - Confirms sequencer is still healthy
   - Verifies blockchain progression occurred

### Expected Output

```
=== RUN   TestAccumenDevnetSmoke
    devnet_smoke_test.go:29: 🚀 Starting Accumen Devnet E2E Smoke Test
    devnet_smoke_test.go:55: Using L0 endpoints: [http://localhost:26660 http://localhost:26670 http://localhost:26680 http://localhost:26690]
    devnet_smoke_test.go:56: Signer type: file
    devnet_smoke_test.go:57: DN paths - TxMeta: acc://dn.acme/tx-metadata, Anchors: acc://dn.acme/anchors
=== RUN   TestAccumenDevnetSmoke/SequencerStatus
    devnet_smoke_test.go:79: Sequencer status - Chain: accumen-local, Height: 15, Running: true
=== RUN   TestAccumenDevnetSmoke/DeployCounter
    devnet_smoke_test.go:92: Counter deployed at acc://counter-devnet-test.acme with tx hash: a1b2c3d4...
=== RUN   TestAccumenDevnetSmoke/SetupEventMonitoring
    devnet_smoke_test.go:100: Event monitoring setup - will poll for transaction status
=== RUN   TestAccumenDevnetSmoke/SubmitIncrements
    devnet_smoke_test.go:119: First increment submitted: b2c3d4e5...
    devnet_smoke_test.go:128: Second increment submitted: c3d4e5f6...
=== RUN   TestAccumenDevnetSmoke/WaitForExecution
    devnet_smoke_test.go:146: Current block height: 18
    devnet_smoke_test.go:156: Detected blockchain progression (height: 18)
=== RUN   TestAccumenDevnetSmoke/QueryState
    devnet_smoke_test.go:168: Query result - Contract: acc://counter-devnet-test.acme, Key: counter, Exists: true
    devnet_smoke_test.go:171: Counter value: 2 (type: uint64)
    devnet_smoke_test.go:174: ✅ Counter state is correct: 2
=== RUN   TestAccumenDevnetSmoke/VerifyDNMetadata
    devnet_smoke_test.go:194: ✅ Found 3 DN metadata entries
=== RUN   TestAccumenDevnetSmoke/FinalVerification
    devnet_smoke_test.go:205: Final sequencer status - Height: 19, Running: true
    devnet_smoke_test.go:215: ✅ Devnet smoke test completed successfully
    devnet_smoke_test.go:254: 🎉 Accumen Devnet E2E Smoke Test completed
--- PASS: TestAccumenDevnetSmoke (45.67s)
```

### Troubleshooting

#### Test Skipped

```
=== RUN   TestAccumenDevnetSmoke
    devnet_smoke_test.go:27: Skipping devnet smoke test - set ACC_DEVNET=1 to enable
--- SKIP: TestAccumenDevnetSmoke (0.00s)
```

**Solution**: Set the `ACC_DEVNET=1` environment variable.

#### Sequencer Connection Failed

```
Failed to get sequencer status: HTTP request failed: dial tcp 127.0.0.1:8666: connect: connection refused
```

**Solutions**:
- Ensure Accumen sequencer is running on port 8666
- Check sequencer configuration and startup logs
- Verify firewall settings

#### Signer Configuration Missing

```
Signer must be configured in config/local.yaml for devnet testing
```

**Solutions**:
- Generate keys using `./bin/accucli keys gen`
- Save key with `./bin/accucli keys save --file ~/.accumen/dev-key.hex --priv <hex>`
- Configure signer in `config/local.yaml`

#### L0 Endpoint Connection Failed

```
No L0 URLs configured for DN metadata verification
```

**Solutions**:
- Ensure devnet is running (see devnet-init.sh and devnet-run.sh)
- Configure L0 endpoints in `config/local.yaml`
- Check devnet RPC endpoint availability

#### Transaction Execution Timeout

```
Warning: No confirmed transactions detected via status polling
```

**Solutions**:
- Check sequencer logs for execution errors
- Verify block production is working (block height increasing)
- Ensure sufficient time for transaction processing
- Check mempool size and configuration

#### State Query Failed

```
⚠️  Counter state not found (may be execution timing or implementation)
```

**Solutions**:
- This is often a timing issue - transactions may still be processing
- Check sequencer execution logs
- Verify contract deployment was successful
- Ensure proper WASM contract implementation

#### DN Metadata Not Found

```
⚠️  No DN metadata entries found (may be bridge disabled or timing)
```

**Solutions**:
- Check if bridge is enabled in configuration
- Verify L0 devnet connectivity
- Check sequencer bridge logs
- This may be expected if bridge is intentionally disabled

### Integration with CI/CD

The devnet smoke test can be integrated into CI/CD pipelines:

```yaml
# Example GitHub Actions
- name: Setup Devnet
  run: |
    ./ops/devnet-init.sh --work-dir /tmp/devnet-ci
    ./ops/devnet-run.sh --work-dir /tmp/devnet-ci &
    sleep 30  # Wait for devnet to start

- name: Start Accumen
  run: |
    go run ./cmd/accumen --role=sequencer --config=config/devnet-ci.yaml &
    sleep 10  # Wait for sequencer to start

- name: Run Devnet Smoke Test
  run: |
    ACC_DEVNET=1 go test ./tests/e2e -run TestAccumenDevnetSmoke -v -timeout 10m
```

### Test Customization

The test can be customized by modifying:

- **Contract Address**: Change `contractAddr` variable
- **Test Timeout**: Adjust context timeout (default: 5 minutes)
- **RPC Endpoint**: Modify `client.rpcURL` for different ports
- **L0 Endpoints**: Configure different devnet endpoints
- **Transaction Count**: Add more increment operations
- **Polling Intervals**: Adjust wait times for different environments

This comprehensive smoke test validates that Accumen works correctly with a real Accumulate devnet, providing confidence in the full integration stack.

## Run with Docker (devnet + accumen + follower)

For the fastest way to get a complete Accumen stack running, use the Docker Compose setup that includes an Accumulate devnet, Accumen sequencer, and follower.

### Prerequisites

- Docker and Docker Compose installed
- At least 4GB available RAM
- Ports 8080, 8666, 8667, 26656, 26657 available

### Quick Start

```bash
# Build Docker images
make docker-build

# Start the complete stack
make docker-devnet-up

# Check status
curl http://localhost:8666/health  # Accumen sequencer
curl http://localhost:8667/health  # Accumen follower
curl http://localhost:8080/status  # Accumulate devnet
```

### What's Included

The Docker Compose stack provides:

1. **Accumulate Devnet**: Complete 3-BVN Accumulate network with Directory Network
2. **Accumen Sequencer**: L1 blockchain producing blocks and executing contracts
3. **Accumen Follower**: Read-only replica for queries and indexing

#### Service Details

- **accumulate-devnet**: Runs on ports 8080 (API), 26656 (P2P), 26657 (Tendermint RPC)
- **accumen**: Sequencer on port 8666
- **follower**: Follower on port 8667

### Configuration

The stack uses dedicated Docker configuration:

```yaml
# docker/config/docker.yaml (mounted in containers)
l0:
  source: static
  static:
    endpoints:
      - http://accumulate-devnet:8080

bridge:
  enableBridge: true
  client:
    serverURL: "http://accumulate-devnet:8080"

blockTime: "2s"
maxTransactions: 100
```

### Operations

#### Starting the Stack

```bash
# Start all services
make docker-devnet-up

# View logs
docker-compose -f docker/docker-compose.devnet.yaml logs -f

# View specific service logs
docker logs accumen-sequencer -f
docker logs accumen-follower -f
docker logs accumulate-devnet -f
```

#### Stopping the Stack

```bash
# Stop and remove all containers and volumes
make docker-devnet-down

# Stop without removing volumes (preserve data)
cd docker && docker-compose -f docker-compose.devnet.yaml stop
```

#### Building Images

```bash
# Build images for both accumen and follower
make docker-build

# Build and push to registry
make docker-push

# Build and push in one command
make docker-all
```

#### Custom Registry

```bash
# Use custom registry
make docker-build DOCKER_REGISTRY=my-registry.com DOCKER_TAG=v1.0.0
make docker-push DOCKER_REGISTRY=my-registry.com DOCKER_TAG=v1.0.0
```

### Testing the Stack

Once running, test the complete integration:

```bash
# 1. Check all services are healthy
curl http://localhost:8080/status   # Accumulate devnet
curl http://localhost:8666/health   # Accumen sequencer
curl http://localhost:8667/health   # Accumen follower

# 2. Deploy a test contract (requires accucli)
go build -o bin/accucli ./cmd/accucli
./bin/accucli deploy \
  --addr=acc://test-counter.acme \
  --wasm=./examples/counter.wasm \
  --rpc=http://localhost:8666

# 3. Execute contract method
./bin/accucli submit \
  --contract=acc://test-counter.acme \
  --entry=increment \
  --arg=amount:1 \
  --rpc=http://localhost:8666

# 4. Query contract state via follower
./bin/accucli query \
  --contract=acc://test-counter.acme \
  --key=counter \
  --rpc=http://localhost:8667

# 5. Verify metadata written to L0
curl -X POST http://localhost:8080/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "query",
    "params": {
      "url": "acc://accumen-metadata.acme"
    }
  }'
```

### Container Images

The Docker setup uses distroless images for security and minimal attack surface:

#### Base Images
- **Builder stage**: `golang:1.21-alpine` for compilation
- **Runtime stage**: `gcr.io/distroless/static:nonroot` for execution

#### Image Features
- CGO disabled for static binaries
- Optimized with `-ldflags="-w -s"` for smaller size
- Runs as non-root user for security
- Includes default configuration

#### Image Sizes
- accumen image: ~20MB
- follower image: ~20MB (same binary, different configuration)

### Networking

The stack uses a dedicated Docker network (`accumulate-devnet`) with service discovery:

```yaml
networks:
  accumulate-devnet:
    driver: bridge
```

Services communicate using container names:
- `accumulate-devnet` → Accumulate API
- `accumen` → Accumen sequencer
- `follower` → Accumen follower

### Volumes

Persistent volumes store blockchain data:

```yaml
volumes:
  accumulate-data:    # Accumulate blockchain state
  accumen-data:       # Accumen sequencer state
  follower-data:      # Follower indexed data
```

### Health Checks

The Accumulate devnet includes health checks:

```yaml
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/status"]
  interval: 30s
  timeout: 10s
  retries: 5
  start_period: 60s
```

Accumen services start only after devnet is healthy.

### Development Workflow

The Docker stack enables rapid development:

```bash
# 1. Start stack
make docker-devnet-up

# 2. Make code changes
vim cmd/accumen/main.go

# 3. Rebuild and restart
make docker-build
make docker-devnet-down
make docker-devnet-up

# 4. Test changes
curl http://localhost:8666/health
```

### Production Considerations

For production use:

1. **Configuration**: Use production-specific config files mounted as volumes
2. **Secrets**: Use Docker secrets or external secret management
3. **Monitoring**: Add monitoring containers (Prometheus, Grafana)
4. **Load Balancing**: Use multiple replicas with load balancer
5. **Backup**: Configure volume backup strategies

### Troubleshooting

#### Container Won't Start

```bash
# Check container logs
docker logs accumen-sequencer

# Check resource usage
docker stats

# Verify port availability
netstat -tlnp | grep -E '(8666|8667|8080)'
```

#### Service Connectivity Issues

```bash
# Test network connectivity between containers
docker exec accumen-sequencer ping accumulate-devnet

# Check container IP addresses
docker network inspect accumulate-devnet
```

#### Data Persistence Issues

```bash
# Check volume mounts
docker inspect accumen-sequencer | jq '.[0].Mounts'

# Verify volume data
docker volume ls
docker volume inspect accumen-data
```

The Docker Compose setup provides the fastest way to get a complete Accumen development environment running with full L0 integration.

## Deploying a Contract via RPC

Accumen supports deploying WASM smart contracts through the JSON-RPC API. Contracts are stored persistently and referenced by their Accumulate-style addresses.

### Prerequisites

- Accumen sequencer running with RPC server enabled
- WASM contract bytecode (compiled from Rust, AssemblyScript, etc.)
- Contract must be between 1 KB and 2 MB in size
- Valid WASM module with magic bytes `\x00asm`

### Deployment Process

#### 1. Prepare Contract Bytecode

Convert your WASM contract to base64 format:

```bash
# Example: Convert counter.wasm to base64
base64 -i counter.wasm -o counter.wasm.b64

# Or use command line tools
cat counter.wasm | base64 > counter.wasm.b64
```

#### 2. Deploy via JSON-RPC

Use the `accumen.deployContract` method:

**Request:**
```bash
curl -X POST http://127.0.0.1:8666 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "accumen.deployContract",
    "params": {
      "addr": "acc://counter.acme",
      "wasm_b64": "AGFzbQEAAAAB..."
    }
  }'
```

**Response:**
```json
{
  "id": 1,
  "result": {
    "wasm_hash": "a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456"
  }
}
```

#### 3. Using accucli

Alternatively, you can use the CLI tool for easier deployment:

```bash
# Deploy contract using CLI (future enhancement)
./bin/accucli deploy \
  --addr=acc://counter.acme \
  --wasm=./counter.wasm \
  --rpc=http://127.0.0.1:8666
```

### Contract Storage

Deployed contracts are stored differently based on the storage backend:

#### Memory Backend (`storage.backend: "memory"`)
- WASM bytecode stored directly in the key-value store
- Metadata and bytecode kept in RAM
- Faster execution but not persistent across restarts

#### Badger Backend (`storage.backend: "badger"`)
- WASM bytecode stored as files in `data/wasm/<hash>.wasm`
- Metadata stored in Badger database
- Persistent across restarts
- Optimized for disk storage

### Contract Execution

Once deployed, contracts can be invoked via transactions:

```bash
# Submit transaction to deployed contract
./bin/accucli submit \
  --contract=acc://counter.acme \
  --entry=increment \
  --arg=amount:1 \
  --rpc=http://127.0.0.1:8666
```

### Error Handling

Common deployment errors:

1. **Invalid Address Format:**
   ```json
   {"error": {"code": -32602, "message": "Invalid address format, must start with 'acc://'"}}
   ```

2. **Contract Already Exists:**
   ```json
   {"error": {"code": -32602, "message": "Contract already deployed at address: acc://counter.acme"}}
   ```

3. **Invalid WASM:**
   ```json
   {"error": {"code": -32602, "message": "Invalid WASM module: missing magic bytes"}}
   ```

4. **Size Limits:**
   ```json
   {"error": {"code": -32602, "message": "WASM module too large: 3145728 bytes (max: 2097152)"}}
   ```

### Contract Lifecycle

1. **Deployment:** WASM module uploaded and validated
2. **Storage:** Bytecode and metadata persisted
3. **Execution:** Runtime loads module for transaction processing
4. **State:** Contract state managed through key-value operations
5. **Upgrades:** Deploy new version at different address (no in-place upgrades)

### Development Workflow

For contract development:

```bash
# 1. Develop contract (Rust example)
cd my-contract/
cargo build --target wasm32-unknown-unknown --release

# 2. Deploy to local Accumen
base64 -i target/wasm32-unknown-unknown/release/my_contract.wasm | \
curl -X POST http://127.0.0.1:8666 \
  -H "Content-Type: application/json" \
  -d @- << 'EOF'
{
  "id": 1,
  "method": "accumen.deployContract",
  "params": {
    "addr": "acc://my-contract.acme",
    "wasm_b64": "$(cat)"
  }
}
EOF

# 3. Test contract
./bin/accucli submit --contract=acc://my-contract.acme --entry=test

# 4. Query contract state
./bin/accucli query --contract=acc://my-contract.acme --key=state
```

This workflow enables rapid contract development and testing on a local Accumen network.

## Testing

### End-to-End Tests

Accumen includes comprehensive end-to-end tests that exercise the complete authority binding and contract execution flow using the Accumulate simulator.

#### Authority Sandbox Test

The `AuthoritySandbox` test provides a complete integration test that covers:

- **Accumulate Setup**: Creates ADI, key books, key pages, and token accounts
- **Authority Binding**: Binds contract signers to L0 key books
- **Contract Deployment**: Deploys and executes WASM contracts
- **L1 Execution**: Calls contract methods with authority verification
- **State Verification**: Validates both DN metadata and follower state synchronization

**Running the Test:**

```bash
# Windows PowerShell
$env:ACC_SIM=1; go test ./tests/e2e -run AuthoritySandbox -v

# Windows Command Prompt
set ACC_SIM=1 && go test ./tests/e2e -run AuthoritySandbox -v

# Linux/macOS
ACC_SIM=1 go test ./tests/e2e -run AuthoritySandbox -v
```

**Test Requirements:**
- Set `ACC_SIM=1` environment variable to enable simulator tests
- Tests are skipped by default to avoid simulator dependency in CI/CD

**What the Test Verifies:**
1. **Authority Chain**: ADI → Key Book → Key Page → Contract binding
2. **L0 Integration**: Credit management and token operations
3. **Contract Execution**: WASM deployment and method calls
4. **Authority Verification**: `VerifyAuthority` and `VerifyAll` pass successfully
5. **State Consistency**: Counter reaches expected value (2 after two increments)
6. **DN Metadata**: Transaction metadata written to DN registry
7. **Follower Sync**: Follower indexer can query and verify transaction state

**Example Output:**
```
=== RUN   TestAuthoritySandbox
    authority_sandbox_test.go:31: Starting Authority Sandbox E2E test...
    authority_sandbox_test.go:45: Step 1: Creating ADI...
    authority_sandbox_test.go:53: ADI created: acc://test-adi.acme (txid: a1b2c3...)
    authority_sandbox_test.go:56: Step 2: Creating Key Book and Key Page...
    authority_sandbox_test.go:66: Key book created: acc://test-adi.acme/book (txid: d4e5f6...)
    authority_sandbox_test.go:75: Key page created: acc://test-adi.acme/book/1 (txid: g7h8i9...)
    authority_sandbox_test.go:95: Step 4: Adding credits to key page...
    authority_sandbox_test.go:134: Step 7: Deploying counter contract...
    authority_sandbox_test.go:156: Step 9: Executing contract increment calls...
    authority_sandbox_test.go:170: First increment successful (txid: tx1_increment, gas: 5000)
    authority_sandbox_test.go:184: Second increment successful (txid: tx2_increment, gas: 5000)
    authority_sandbox_test.go:189: Counter value verified: 2
    authority_sandbox_test.go:258: ✅ Authority Sandbox E2E Test PASSED
--- PASS: TestAuthoritySandbox (2.34s)
PASS
```

This test ensures that the complete Accumen authority and execution pipeline works correctly from L0 identity management through L1 contract execution and state synchronization.

## Quick Start

### Local Development
```bash
# Clone and setup
git clone <repository>
cd accumulate-accumen

# Build
make build

# Run with local config
make run-sequencer

# Check health
curl http://127.0.0.1:8082/health
```

### Production Deployment
```bash
# Build optimized binary
make build-release

# Create production config
cp config/local.yaml config/production.yaml
# Edit config/production.yaml for your environment

# Run sequencer
./bin/accumen --role=sequencer --config=config/production.yaml

# Run follower (optional)
./bin/accumen --role=follower --config=config/production.yaml
```

## Configuration

### Environment-Specific Configs

#### Development (`config/local.yaml`)
- Fast block times (1s)
- Debug logging enabled
- Bridge disabled
- In-memory storage

#### Staging (`config/staging.yaml`)
- Moderate block times (5s)
- Info logging
- Bridge enabled with testnet
- Persistent storage

#### Production (`config/production.yaml`)
- Optimal block times (10-15s)
- Warn/Error logging
- Bridge enabled with mainnet
- High availability setup

### L0 Endpoint Configuration

Accumen supports two methods for discovering Accumulate L0 API endpoints: proxy-based discovery and static configuration.

#### Static Configuration (Default)

Use a predefined list of endpoints:

```yaml
l0:
  source: "static"
  apiV3Endpoints:
    - "https://mainnet.accumulatenetwork.io/v3"
    - "https://api.accumulatenetwork.io/v3"
  wsPath: "/ws"
```

- **Pros**: Simple, reliable, no external dependencies
- **Cons**: Manual updates required when endpoints change
- **Use cases**: Production environments, development, testing

#### Proxy-Based Discovery

Discover endpoints dynamically from a registry/proxy service:

```yaml
l0:
  source: "proxy"
  proxy: "https://registry.accumulatenetwork.io"
  wsPath: "/ws"
```

- **Pros**: Automatic endpoint discovery, load distribution
- **Cons**: Dependency on proxy service availability
- **Use cases**: Dynamic environments, multi-region deployments

#### Health Monitoring & Failover

Both modes support automatic health checking and failover:

- **Health checks**: Every 30 seconds via `/health` endpoint
- **Failure handling**: 3 strikes before 5-minute backoff
- **Round-robin**: Distributes load across healthy endpoints
- **Logging**: Endpoint selection and failover events logged

#### WebSocket Support

WebSocket URLs are automatically generated by converting HTTP(S) endpoints:
- `https://api.example.com/v3` → `wss://api.example.com/v3/ws`
- `http://localhost:26660/v3` → `ws://localhost:26660/v3/ws`

### Key Configuration Parameters

```yaml
# L0 Endpoint Discovery
l0:
  source: "static"                    # "proxy" | "static"
  proxy: "https://proxy.acme.com"     # Proxy URL for discovery (proxy mode only)
  apiV3Endpoints:                     # Static endpoint list (static mode only)
    - "https://mainnet.accumulatenetwork.io/v3"
    - "https://api.accumulatenetwork.io/v3"
  wsPath: "/ws"                       # WebSocket path for real-time updates

# Performance tuning
block_time: "10s"              # Block production interval
max_transactions: 1000         # Max txs per block
execution.workers: 8           # Worker threads
mempool.max_size: 50000       # Max pending transactions

# Resource limits
execution.gas_limit: 50000000  # Gas limit per transaction
wasm.max_memory_pages: 64     # WASM memory limit
execution.memory_limit: 128    # Engine memory limit

# Bridge configuration
bridge.enable_bridge: true     # Enable L0 integration
bridge.submitter.workers: 4    # Submission workers
bridge.stager.batch_size: 200  # Staging batch size

# Monitoring
metrics_addr: ":8081"          # Metrics endpoint
logging.level: "info"          # Log level
```

## Operations

### Starting Services

#### Sequencer Node
```bash
# Start sequencer (block producer)
./bin/accumen \
  --role=sequencer \
  --config=config/production.yaml \
  2>&1 | tee logs/sequencer.log &

# Verify startup
tail -f logs/sequencer.log
curl http://localhost:8082/health
```

#### Follower Node
```bash
# Start follower (block consumer)
./bin/accumen \
  --role=follower \
  --config=config/production.yaml \
  2>&1 | tee logs/follower.log &

# Monitor sync status
curl http://localhost:8082/stats
```

### Stopping Services

#### Graceful Shutdown
```bash
# Send SIGTERM for graceful shutdown
kill -TERM <pid>

# Wait for shutdown (up to 30s)
# Process should exit cleanly
```

#### Force Stop
```bash
# If graceful shutdown fails
kill -KILL <pid>

# Check for orphaned processes
ps aux | grep accumen
```

### Health Checks

#### Basic Health
```bash
# Health endpoint
curl http://localhost:8082/health

# Expected response
{
  "status": "healthy",
  "timestamp": "2024-01-01T12:00:00Z",
  "uptime": "24h30m15s",
  "version": "v1.0.0"
}
```

#### Detailed Status
```bash
# Statistics endpoint
curl http://localhost:8080/stats

# Expected response
{
  "running": true,
  "block_height": 12345,
  "blocks_produced": 12345,
  "txs_processed": 567890,
  "uptime": "24h30m15s",
  "mempool_stats": {
    "size": 150,
    "accounts": 75
  }
}
```

#### Component Health
```bash
# Mempool status
curl http://localhost:8080/mempool/stats

# Execution engine status
curl http://localhost:8080/execution/stats

# Bridge status (if enabled)
curl http://localhost:8080/bridge/stats
```

## Monitoring

### Key Metrics

#### Performance Metrics
- `accumen_blocks_produced_total` - Total blocks produced
- `accumen_transactions_processed_total` - Total transactions processed
- `accumen_block_production_duration_seconds` - Block production time
- `accumen_transaction_execution_duration_seconds` - Transaction execution time
- `accumen_gas_used_total` - Total gas consumed

#### Resource Metrics
- `accumen_mempool_size` - Current mempool size
- `accumen_mempool_accounts` - Unique accounts in mempool
- `accumen_execution_queue_depth` - Execution queue backlog
- `accumen_memory_usage_bytes` - Memory usage
- `accumen_goroutines` - Active goroutines

#### Error Metrics
- `accumen_transaction_failures_total` - Failed transactions
- `accumen_execution_errors_total` - Execution errors
- `accumen_bridge_errors_total` - Bridge submission errors
- `accumen_api_errors_total` - API errors

### Alerting Rules

#### Critical Alerts
```yaml
# Block production stopped
- alert: AccumenBlockProductionStopped
  expr: increase(accumen_blocks_produced_total[5m]) == 0
  for: 2m
  labels:
    severity: critical
  annotations:
    summary: "Block production has stopped"

# High error rate
- alert: AccumenHighErrorRate
  expr: rate(accumen_transaction_failures_total[5m]) > 0.1
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "High transaction failure rate"
```

#### Warning Alerts
```yaml
# Mempool filling up
- alert: AccumenMempoolFull
  expr: accumen_mempool_size > 40000
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Mempool is nearly full"

# Slow block production
- alert: AccumenSlowBlocks
  expr: accumen_block_production_duration_seconds > 30
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "Block production is slow"
```

### Dashboards

#### Key Performance Indicators
1. **Throughput**: Transactions per second
2. **Latency**: Block production time
3. **Error Rate**: Failed transaction percentage
4. **Resource Usage**: CPU, memory, disk
5. **Queue Depths**: Mempool, execution queue

#### Operational Metrics
1. **Block Height**: Current and target height
2. **Sync Status**: Follower sync progress
3. **Bridge Health**: L0 connection status
4. **API Response Times**: Endpoint latencies

## Troubleshooting

### Common Issues

#### 1. Block Production Stopped

**Symptoms:**
- No new blocks being produced
- Block height not increasing
- `accumen_blocks_produced_total` metric flatlined

**Diagnosis:**
```bash
# Check sequencer logs
tail -100 logs/sequencer.log | grep ERROR

# Check mempool
curl http://localhost:8080/mempool/stats

# Check execution engine
curl http://localhost:8080/execution/stats
```

**Resolution:**
```bash
# If mempool is empty, check transaction submission
# If execution engine is stuck, restart service
# If bridge is failing, check L0 connectivity

# Restart if necessary
kill -TERM <sequencer_pid>
./bin/accumen --role=sequencer --config=config/production.yaml &
```

#### 2. High Memory Usage

**Symptoms:**
- Memory usage continuously increasing
- Out of memory errors
- System slowdown

**Diagnosis:**
```bash
# Check current memory usage
ps aux | grep accumen

# Check Go runtime metrics
curl http://localhost:8081/metrics | grep go_memstats

# Check component-specific usage
curl http://localhost:8080/execution/stats
curl http://localhost:8080/mempool/stats
```

**Resolution:**
```bash
# Tune mempool size
# Reduce worker count
# Increase memory limits in config
# Restart with updated configuration
```

#### 3. Bridge Failures

**Symptoms:**
- Bridge submission errors
- `accumen_bridge_errors_total` increasing
- L0 anchoring failures

**Diagnosis:**
```bash
# Check bridge status
curl http://localhost:8080/bridge/stats

# Check L0 connectivity
curl https://mainnet.accumulatenetwork.io/v3/status

# Check bridge logs
grep "bridge" logs/sequencer.log | tail -20
```

**Resolution:**
```bash
# Verify L0 endpoint configuration
# Check network connectivity
# Verify credentials/authentication
# Restart bridge components if needed
```

#### 4. API Timeouts

**Symptoms:**
- API requests timing out
- High API response times
- Client connection errors

**Diagnosis:**
```bash
# Check API metrics
curl http://localhost:8081/metrics | grep http

# Check active connections
netstat -an | grep :8080

# Check system load
top
iostat -x 1
```

**Resolution:**
```bash
# Increase timeout configurations
# Scale API workers
# Add load balancer
# Optimize database queries
```

### Performance Tuning

#### High Throughput Configuration
```yaml
# Optimize for throughput
block_time: "1s"
max_transactions: 5000
execution:
  workers: 16
  queue_size: 10000
mempool:
  max_size: 100000
  account_limit: 5000
```

#### Low Latency Configuration
```yaml
# Optimize for latency
block_time: "500ms"
max_transactions: 100
execution:
  workers: 4
  timeout: "5s"
bridge:
  stager:
    flush_interval: "100ms"
```

#### Resource Constrained Configuration
```yaml
# Optimize for limited resources
execution:
  workers: 2
  memory_limit: 16
wasm:
  max_memory_pages: 8
mempool:
  max_size: 5000
```

## Backup and Recovery

### State Backup
```bash
# Backup current state
tar -czf accumen-state-$(date +%Y%m%d).tar.gz data/

# Backup configuration
cp -r config/ backup/config-$(date +%Y%m%d)/

# Backup logs
cp -r logs/ backup/logs-$(date +%Y%m%d)/
```

### Recovery Procedures

#### Complete System Recovery
```bash
# Stop services
kill -TERM <accumen_pid>

# Restore state
tar -xzf accumen-state-YYYYMMDD.tar.gz

# Restore configuration
cp -r backup/config-YYYYMMDD/* config/

# Start services
./bin/accumen --role=sequencer --config=config/production.yaml &
```

#### Partial Recovery
```bash
# Recover specific component
# Stop service
# Replace corrupted files
# Restart service
# Verify functionality
```

## Security

### Access Control
```bash
# Secure file permissions
chmod 600 config/*.yaml
chmod 700 data/
chmod 755 bin/accumen

# Network security
# Configure firewall rules
# Use TLS for external communication
# Implement rate limiting
```

### Monitoring Security
```bash
# Monitor failed authentication attempts
# Track unusual API usage patterns
# Alert on configuration changes
# Monitor resource exhaustion attacks
```

## Maintenance

### Regular Maintenance Tasks

#### Daily
- [ ] Check service health
- [ ] Review error logs
- [ ] Monitor resource usage
- [ ] Verify backup completion

#### Weekly
- [ ] Analyze performance trends
- [ ] Review configuration changes
- [ ] Update security patches
- [ ] Clean old logs

#### Monthly
- [ ] Capacity planning review
- [ ] Security audit
- [ ] Performance optimization
- [ ] Disaster recovery testing

### Upgrades

#### Pre-Upgrade Checklist
- [ ] Backup current state
- [ ] Review release notes
- [ ] Test in staging environment
- [ ] Plan rollback procedure
- [ ] Schedule maintenance window

#### Upgrade Procedure
```bash
# 1. Backup current version
cp bin/accumen bin/accumen.backup

# 2. Stop services gracefully
kill -TERM <pid>

# 3. Install new version
make build-release

# 4. Update configuration if needed
# Check for breaking changes

# 5. Start new version
./bin/accumen --role=sequencer --config=config/production.yaml &

# 6. Verify functionality
curl http://localhost:8082/health

# 7. Monitor for issues
tail -f logs/sequencer.log
```

#### Rollback Procedure
```bash
# If upgrade fails, rollback
kill -TERM <new_pid>
cp bin/accumen.backup bin/accumen
./bin/accumen --role=sequencer --config=config/production.yaml &
```

## Contact and Escalation

### Support Channels
- **Emergency**: Immediate response required
- **High Priority**: Response within 1 hour
- **Medium Priority**: Response within 4 hours
- **Low Priority**: Response within 24 hours

### Escalation Matrix
1. **Level 1**: On-call engineer
2. **Level 2**: Senior engineer
3. **Level 3**: Engineering manager
4. **Level 4**: CTO/VP Engineering

### Emergency Contacts
- On-call Engineer: [contact information]
- Engineering Manager: [contact information]
- Infrastructure Team: [contact information]

## Appendix

### Observability Endpoints

#### Health Check Endpoints

The Accumen nodes expose several health check endpoints for monitoring:

- **`/healthz`** - Basic health status returning JSON with `ok`, `height`, `last_anchor_time`
- **`/health/ready`** - Readiness check (returns 503 if not ready to serve traffic)
- **`/health/live`** - Liveness check (simple alive confirmation)
- **`/health/detailed`** - Detailed status with full metrics snapshot

#### Metrics Endpoints

- **`/debug/vars`** - Expvar metrics in JSON format including:
  - `blocks_produced` - Total blocks produced by sequencer
  - `txs_executed` - Total transactions executed
  - `l0_submitted` - Successful L0 submissions to Accumulate network
  - `l0_failed` - Failed L0 submissions
  - `anchors_written` - Anchors written to DN
  - `wasm_gas_used_total` - Total WASM gas consumed
  - `current_height` - Current block height
  - `follower_height` - Follower sync height
  - `indexer_entries_processed` - Entries processed by follower indexer

#### Default Ports

- **Sequencer**: Metrics server runs on `:8667` by default
- **Follower**: Metrics server runs on `:8668` by default

#### Usage Examples

```bash
# Check sequencer health
curl http://localhost:8667/healthz

# Get detailed metrics
curl http://localhost:8667/debug/vars

# Check follower readiness
curl http://localhost:8668/health/ready
```

For production monitoring, configure your monitoring system to:
1. Poll `/healthz` for basic health status
2. Scrape `/debug/vars` for detailed metrics
3. Use `/health/ready` for load balancer health checks

### Useful Commands
```bash
# Process management
pgrep -f accumen
pkill -f accumen
nohup ./bin/accumen ... &

# Log analysis
grep -i error logs/sequencer.log
tail -f logs/sequencer.log | grep -v INFO
journalctl -u accumen -f

# Network diagnostics
netstat -tlnp | grep accumen
ss -tlnp | grep accumen
curl -v http://localhost:8080/health

# System resources
htop
iotop
free -h
df -h
```

## Switching Networks

Accumen supports multiple network profiles to easily switch between different Accumulate networks (devnet, testnet, mainnet) without manually configuring endpoints.

### Network Profiles

The following network profiles are built into Accumen:

#### Devnet
- **Endpoints**: `http://127.0.0.1:26660` (local devnet)
- **Use case**: Local development and testing
- **WebSocket**: `/ws`

#### Testnet
- **Endpoints**: `https://testnet.accumulate.net/v3`
- **Use case**: Integration testing and staging
- **WebSocket**: `/ws`

#### Mainnet
- **Endpoints**: `https://mainnet.accumulate.net/v3`
- **Use case**: Production deployments
- **WebSocket**: `/ws`

### Switching Methods

There are multiple ways to specify which network to use, in order of precedence:

1. **Command Line Flag** (highest priority)
2. **Environment Variable**
3. **Configuration File**
4. **Default** (local development)

#### Method 1: Command Line Flag

```bash
# Use testnet
go run ./cmd/accumen --config=config/local.yaml --network=testnet --role=sequencer

# Use mainnet
go run ./cmd/accumen --config=config/local.yaml --network=mainnet --role=sequencer

# Use devnet (local)
go run ./cmd/accumen --config=config/local.yaml --network=devnet --role=sequencer
```

#### Method 2: Environment Variable

```bash
# Set environment variable
export ACCUMEN_NETWORK=testnet

# Run without --network flag
go run ./cmd/accumen --config=config/local.yaml --role=sequencer

# Or on Windows
set ACCUMEN_NETWORK=mainnet
go run ./cmd/accumen --config=config/local.yaml --role=sequencer
```

#### Method 3: Configuration File

Add the network configuration to your YAML config file:

```yaml
# config/testnet.yaml
network:
  name: testnet

# Other configuration...
blockTime: "5s"
anchorEvery: "30s"
```

#### Method 4: PowerShell Script

The `run-accumen.ps1` script supports the `-Network` parameter:

```powershell
# Use testnet
.\ops\run-accumen.ps1 -Network testnet

# Use mainnet
.\ops\run-accumen.ps1 -Network mainnet -Config config/production.yaml

# Use devnet (local development)
.\ops\run-accumen.ps1 -Network devnet
```

### Network Profile Behavior

When a network profile is specified:

1. **Automatic Configuration**: L0 endpoints and WebSocket paths are automatically configured
2. **Override Protection**: Manual L0 configuration in the config file takes precedence
3. **Logging**: The effective network and endpoints are logged on startup
4. **Validation**: Invalid network names are rejected with helpful error messages

### Example Startup Output

```
INFO[2024-01-15T10:30:00Z] Using network profile: testnet
INFO[2024-01-15T10:30:00Z] Applied network profile 'testnet': endpoints=[https://testnet.accumulate.net/v3], wsPath=/ws
INFO[2024-01-15T10:30:00Z] L0 endpoint manager started with source: static
INFO[2024-01-15T10:30:00Z] Using initial L0 endpoint: https://testnet.accumulate.net/v3
```

### Custom Network Configuration

For custom networks or advanced configurations, you can still manually configure L0 endpoints:

```yaml
# config/custom.yaml
l0:
  source: static
  static:
    - https://custom-node1.example.com/v3
    - https://custom-node2.example.com/v3
  wsPath: "/ws"

# Network profile will be ignored when l0.source is explicitly set
network:
  name: testnet  # This will be ignored
```

### Development Workflow

The network profiles enable smooth development workflows:

```bash
# 1. Local development (devnet)
.\ops\run-accumen.ps1 -Network devnet

# 2. Integration testing (testnet)
.\ops\run-accumen.ps1 -Network testnet -Config config/staging.yaml

# 3. Production deployment (mainnet)
.\ops\run-accumen.ps1 -Network mainnet -Config config/production.yaml
```

### Environment-Specific Examples

#### Development Environment
```yaml
# config/dev.yaml
network:
  name: devnet

blockTime: "1s"     # Fast blocks for development
anchorEvery: "5s"   # Frequent anchoring for testing

storage:
  backend: memory   # In-memory for development
```

#### Staging Environment
```yaml
# config/staging.yaml
network:
  name: testnet

blockTime: "5s"     # Moderate blocks for testing
anchorEvery: "30s"  # Regular anchoring

storage:
  backend: badger   # Persistent storage
  path: data/staging
```

#### Production Environment
```yaml
# config/production.yaml
network:
  name: mainnet

blockTime: "15s"    # Production block timing
anchorEvery: "60s"  # Conservative anchoring

storage:
  backend: badger
  path: /var/lib/accumen

# Override with specific endpoints for reliability
l0:
  source: static
  static:
    - https://mainnet-api1.accumulate.net/v3
    - https://mainnet-api2.accumulate.net/v3
    - https://mainnet-api3.accumulate.net/v3
```

### Troubleshooting Network Issues

#### Unknown Network Profile
```
WARN[2024-01-15T10:30:00Z] Unknown network profile 'typo-testnet', using default configuration
```
**Solution**: Check spelling of network name (devnet, testnet, mainnet)

#### Network Override
```
INFO[2024-01-15T10:30:00Z] Using network profile: local
INFO[2024-01-15T10:30:00Z] L0 configuration already specified, network profile ignored
```
**Solution**: Remove explicit `l0.source` config if you want to use network profiles

#### Endpoint Connectivity
```
ERROR[2024-01-15T10:30:00Z] Failed to connect to L0 endpoint: https://testnet.accumulate.net/v3
```
**Solution**: Check network connectivity and endpoint availability

The network profile system makes it easy to switch between different Accumulate networks while maintaining consistent configuration for other aspects of your Accumen deployment.

### Configuration Templates
See `config/` directory for environment-specific templates.

### Log Patterns
Common log patterns to watch for:
- `ERROR.*mempool.*full` - Mempool capacity issues
- `ERROR.*execution.*timeout` - Execution timeouts
- `ERROR.*bridge.*failed` - Bridge connectivity issues
- `WARN.*gas.*limit` - Gas limit warnings

## Production JSON-RPC Configuration

The Accumen JSON-RPC server includes comprehensive production features including authentication, rate limiting, CORS, TLS, and standardized error handling.

### Server Configuration

Configure the server section in your `config.yaml`:

```yaml
server:
  addr: ":8666"                    # Server bind address
  tls:
    cert: "/path/to/server.crt"     # TLS certificate file
    key: "/path/to/server.key"      # TLS private key file
  apiKeys:                          # API key allowlist
    - "prod-key-1234567890abcdef"
    - "staging-key-abcdef1234567890"
  cors:
    origins:                        # Allowed CORS origins
      - "https://app.example.com"
      - "https://dashboard.example.com"
  rate:
    rps: 100                        # Requests per second per IP
    burst: 200                      # Burst capacity per IP
```

### Authentication & Authorization

#### API Key Authentication

API keys are validated via the `X-API-Key` header:

```bash
curl -X POST https://api.example.com/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: prod-key-1234567890abcdef" \
  -d '{
    "id": 1,
    "method": "accumen.status",
    "params": {}
  }'
```

**Features:**
- Configurable allowlist of API keys
- Health endpoint (`/health`) bypasses API key checks
- Graceful handling when no API keys are configured (development mode)

#### Error Responses

Authentication failures return standardized error codes:

```json
{
  "error": {
    "code": -32401,
    "message": "Unauthorized: Missing X-API-Key header"
  }
}
```

### Rate Limiting

Per-IP token bucket rate limiting protects against abuse:

**Configuration:**
- `rps`: Sustained requests per second (default: 100)
- `burst`: Burst capacity for traffic spikes (default: 200)

**Rate Limit Response:**
```json
{
  "error": {
    "code": -32429,
    "message": "Rate limit exceeded"
  }
}
```

**Headers:**
- `Retry-After: 60` - Indicates retry delay in seconds

### CORS (Cross-Origin Resource Sharing)

Configure allowed origins for browser-based applications:

```yaml
server:
  cors:
    origins:
      - "https://app.example.com"
      - "https://dashboard.example.com"
      - "*"  # Allow all origins (not recommended for production)
```

**Headers Set:**
- `Access-Control-Allow-Origin`
- `Access-Control-Allow-Methods: POST, GET, OPTIONS`
- `Access-Control-Allow-Headers: Content-Type, X-API-Key, Authorization`
- `Access-Control-Max-Age: 86400`

### TLS/HTTPS Configuration

Enable TLS for production deployments:

#### Certificate Setup

```bash
# Generate self-signed certificate (development only)
openssl req -x509 -newkey rsa:4096 -keyout server.key -out server.crt -days 365 -nodes

# Production: Use certificates from CA (Let's Encrypt, etc.)
certbot certonly --standalone -d api.example.com
```

#### Configuration

```yaml
server:
  addr: ":8443"  # Standard HTTPS port
  tls:
    cert: "/etc/ssl/certs/api.example.com.crt"
    key: "/etc/ssl/private/api.example.com.key"
```

#### HTTPS Client Usage

```bash
curl -X POST https://api.example.com:8443/rpc \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"id":1,"method":"accumen.status","params":{}}'
```

### Standardized Error Codes

The JSON-RPC server uses standardized error codes for consistent client handling:

#### Standard JSON-RPC Errors
- `-32700`: Parse error (invalid JSON)
- `-32600`: Invalid Request object
- `-32601`: Method not found
- `-32602`: Invalid method parameters
- `-32603`: Internal JSON-RPC error

#### Authentication Errors
- `-32401`: Unauthorized (invalid API key)
- `-32403`: Forbidden (insufficient permissions)
- `-32429`: Rate limit exceeded

#### Accumen-Specific Errors
- `-33001`: Authority not permitted (L0 validation failed)
- `-33002`: Contract paused/inactive
- `-33003`: Determinism violation (WASM audit failed)
- `-33004`: Insufficient L0 credits
- `-33005`: Contract not found
- `-33006`: Invalid contract address format
- `-33007`: Transaction execution failed
- `-33008`: Gas limit exceeded
- `-33009`: Invalid or replay nonce
- `-33010`: Mempool full
- `-33011`: Transaction validation failed
- `-33012`: State operation failed
- `-33013`: Contract version not found
- `-33014`: Contract migration failed
- `-33015`: Contract upgrade not permitted
- `-33016`: Namespace reserved
- `-33017`: Service temporarily unavailable

### Security Headers

All responses include security headers:

```
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'none'; script-src 'none'; object-src 'none'
```

### Health Endpoint

The `/health` endpoint provides service health status:

```bash
curl http://localhost:8666/health
```

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2024-01-15T10:30:00Z",
  "version": "v1.0.0"
}
```

**Features:**
- Bypasses API key authentication
- Bypasses rate limiting
- Returns 200 OK when service is healthy

### Production Deployment Checklist

#### Security
- [ ] Configure TLS with valid certificates
- [ ] Set up API key authentication
- [ ] Configure CORS origins (avoid wildcard `*`)
- [ ] Enable rate limiting appropriate for your load
- [ ] Use HTTPS-only in production

#### Monitoring
- [ ] Monitor rate limit violations
- [ ] Track authentication failures
- [ ] Set up alerting for high error rates
- [ ] Monitor TLS certificate expiration

#### Configuration
- [ ] Use environment-specific config files
- [ ] Secure API key storage (env vars, secrets management)
- [ ] Configure appropriate timeout values
- [ ] Set proper log levels for production

#### Load Balancing
```yaml
server:
  rate:
    rps: 50    # Per instance (total = instances × rps)
    burst: 100
```

When load balancing, configure per-instance limits based on your total capacity.

### Example Production Configuration

```yaml
# production.yaml
server:
  addr: ":8443"
  tls:
    cert: "/etc/ssl/certs/api.example.com.crt"
    key: "/etc/ssl/private/api.example.com.key"
  apiKeys:
    - "${API_KEY_1}"
    - "${API_KEY_2}"
  cors:
    origins:
      - "https://app.example.com"
      - "https://dashboard.example.com"
  rate:
    rps: 200
    burst: 400

# Environment variables
export API_KEY_1="prod-key-1234567890abcdef"
export API_KEY_2="staging-key-abcdef1234567890"
```

This configuration provides enterprise-grade security and reliability for production Accumen deployments.

## Prometheus Metrics and Observability

Accumen provides rich observability through Prometheus metrics when built with the `prom` build tag. This enables detailed monitoring and alerting for production deployments.

### Building with Prometheus Support

To enable Prometheus metrics, build with the `prom` tag:

```bash
# Build sequencer with Prometheus support
go build -tags=prom -o bin/accumen ./cmd/accumen

# Build follower with Prometheus support
go build -tags=prom -o bin/accumen-follower ./cmd/accumen-follower

# Build both using Makefile
make build TAGS=prom
```

### Metrics Endpoints

When built with Prometheus support, the following endpoints are available:

- **`/metrics`** - Prometheus metrics endpoint (requires `-tags=prom`)
- **`/debug/vars`** - Expvar metrics in JSON format
- **`/healthz`** - Basic health check
- **`/health/ready`** - Readiness check
- **`/health/live`** - Liveness check

#### Without Prometheus Support

If built without the `prom` tag, the `/metrics` endpoint returns:

```
404 Not Found
Prometheus metrics not available - build with -tags=prom to enable
```

### Available Metrics

#### Counter Metrics

- **`accumen_blocks_produced_total`** - Total blocks produced by sequencer
- **`accumen_transactions_executed_total`** - Total transactions executed
- **`accumen_l0_submitted_total`** - Successful L0 submissions to Accumulate
- **`accumen_l0_failed_total`** - Failed L0 submissions
- **`accumen_anchors_written_total`** - Anchors written to DN
- **`accumen_wasm_gas_used_total`** - Total WASM gas consumed
- **`accumen_indexer_entries_processed_total`** - Entries processed by follower

#### Gauge Metrics

- **`accumen_current_height`** - Current block height
- **`accumen_follower_height`** - Follower sync height
- **`accumen_mempool_size`** - Current mempool transaction count
- **`accumen_mempool_accounts`** - Unique accounts in mempool

### Prometheus Configuration

Add Accumen to your `prometheus.yml`:

```yaml
global:
  scrape_interval: 15s

scrape_configs:
  - job_name: 'accumen-sequencer'
    static_configs:
      - targets: ['localhost:8667']
    metrics_path: /metrics
    scrape_interval: 10s

  - job_name: 'accumen-follower'
    static_configs:
      - targets: ['localhost:8668']
    metrics_path: /metrics
    scrape_interval: 10s
```

### Grafana Dashboard

Import the provided Grafana dashboard for comprehensive monitoring:

#### Manual Import

1. Open Grafana UI
2. Navigate to **Dashboards** → **Import**
3. Upload `ops/grafana/dashboard.json`
4. Configure data source as your Prometheus instance
5. Click **Import**

#### Dashboard Features

The dashboard includes:

- **Block Production Rate** - Blocks per second over time
- **Transaction Execution Rate** - Transactions per second over time
- **L0 Submission Rate** - Success/failure rates for L0 bridge
- **WASM Gas Usage Rate** - Gas consumption over time
- **Mempool Size** - Transaction and account counts
- **Block Height** - Current and follower heights
- **Summary Stats** - Total blocks, transactions, gas, failures

#### Dashboard JSON

The dashboard is located at `ops/grafana/dashboard.json` and includes:

```json
{
  "title": "Accumen Blockchain Dashboard",
  "tags": ["accumen", "blockchain", "metrics"],
  "panels": [
    {
      "title": "Block Production Rate",
      "targets": [
        {
          "expr": "rate(accumen_blocks_produced_total[5m])",
          "legendFormat": "Blocks per second"
        }
      ]
    }
  ]
}
```

### Example Queries

#### PromQL Examples

```promql
# Block production rate (blocks/sec)
rate(accumen_blocks_produced_total[5m])

# Transaction throughput (tx/sec)
rate(accumen_transactions_executed_total[5m])

# L0 bridge success rate
rate(accumen_l0_submitted_total[5m]) /
(rate(accumen_l0_submitted_total[5m]) + rate(accumen_l0_failed_total[5m]))

# Average gas per transaction
rate(accumen_wasm_gas_used_total[5m]) /
rate(accumen_transactions_executed_total[5m])

# Follower lag (blocks behind)
accumen_current_height - accumen_follower_height
```

### Alerting Rules

#### Critical Alerts

```yaml
groups:
  - name: accumen.critical
    rules:
      - alert: AccumenBlockProductionStopped
        expr: increase(accumen_blocks_produced_total[5m]) == 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "Accumen block production has stopped"
          description: "No blocks produced in last 5 minutes"

      - alert: AccumenHighL0FailureRate
        expr: |
          rate(accumen_l0_failed_total[5m]) /
          (rate(accumen_l0_submitted_total[5m]) + rate(accumen_l0_failed_total[5m])) > 0.1
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "High L0 submission failure rate"
          description: "L0 failure rate: {{ $value | humanizePercentage }}"
```

#### Warning Alerts

```yaml
  - name: accumen.warning
    rules:
      - alert: AccumenMempoolFull
        expr: accumen_mempool_size > 40000
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "Accumen mempool is nearly full"
          description: "Mempool size: {{ $value }} transactions"

      - alert: AccumenFollowerLagging
        expr: accumen_current_height - accumen_follower_height > 100
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Follower is lagging behind sequencer"
          description: "Follower is {{ $value }} blocks behind"
```

### Development Workflow

#### Local Development

```bash
# 1. Build with Prometheus support
go build -tags=prom -o bin/accumen ./cmd/accumen

# 2. Start Accumen
./bin/accumen --config=config/local.yaml --role=sequencer

# 3. Check metrics are available
curl http://localhost:8667/metrics

# 4. Run Prometheus locally
docker run -p 9090:9090 \
  -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml \
  prom/prometheus

# 5. Import Grafana dashboard
# Upload ops/grafana/dashboard.json to Grafana UI
```

#### Production Deployment

```bash
# 1. Build production binaries with Prometheus
make build-release TAGS=prom

# 2. Deploy with monitoring stack
docker-compose -f docker-compose.prod.yaml up -d

# 3. Verify metrics collection
curl https://metrics.example.com:8667/metrics

# 4. Import dashboard to production Grafana
# Configure alerting rules in production Prometheus
```

### Metrics Architecture

#### Metric Synchronization

The Prometheus metrics mirror the existing expvar metrics:

```go
// Synchronizes expvar counters with Prometheus counters
func updatePrometheusMetrics() {
    promBlocksProduced.Add(float64(blocksProduced.Value()) - getPromCounterValue(promBlocksProduced))
    promTxsExecuted.Add(float64(txsExecuted.Value()) - getPromCounterValue(promTxsExecuted))
    // ... other metrics
}
```

#### Build Tags

- **With `prom` tag**: Full Prometheus integration with `/metrics` endpoint
- **Without `prom` tag**: Expvar-only metrics, `/metrics` returns 404

#### Performance Impact

- Minimal overhead when built with `prom` tag
- Zero overhead when built without `prom` tag
- Metrics updated every 10 seconds by default

### Troubleshooting

#### Metrics Not Available

```bash
# Check if built with prom tag
curl http://localhost:8667/metrics
# Should return Prometheus format, not 404

# Rebuild with prom tag if needed
go build -tags=prom -o bin/accumen ./cmd/accumen
```

#### Missing Metrics

```bash
# Check expvar metrics for comparison
curl http://localhost:8667/debug/vars

# Verify metric registration
grep "MustRegister" internal/metrics/prom.go
```

#### High Cardinality

```bash
# Monitor metric cardinality in Prometheus
curl http://localhost:9090/api/v1/label/__name__/values | jq '.data | length'

# Review metric labels for optimization
curl http://localhost:8667/metrics | grep accumen_ | head -10
```

The Prometheus integration provides production-grade observability for Accumen deployments, enabling proactive monitoring, alerting, and performance optimization.