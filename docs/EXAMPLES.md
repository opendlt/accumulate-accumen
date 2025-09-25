# Accumen CLI Examples

This guide shows how to use the Accumen CLI for local development, from building and deploying contracts to interacting with them and monitoring DN metadata writes.

## Prerequisites

- Rust toolchain for WASM compilation
- Accumen sequencer running locally
- `accucli` binary built and available

## Example Walkthrough

### 1. Build a Rust Contract to WASM

First, create a simple counter contract in Rust:

```bash
# Create a new Rust project
cargo new --lib counter-contract
cd counter-contract
```

Edit `Cargo.toml`:
```toml
[package]
name = "counter-contract"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib"]

[dependencies]
# Add any WASM-specific dependencies here
```

Create `src/lib.rs`:
```rust
#[no_mangle]
pub extern "C" fn increment() {
    // Simple counter increment logic
    // In a real implementation, this would interact with L1 state

    // For demonstration, this contract would:
    // 1. Read current counter value from state
    // 2. Increment by 1
    // 3. Write new value back to state
}

#[no_mangle]
pub extern "C" fn get_counter() -> u64 {
    // Return current counter value
    // In a real implementation, this would read from L1 state
    42 // placeholder return value
}
```

Build the WASM module:
```bash
# Add WASM target if not already added
rustup target add wasm32-unknown-unknown

# Build for WASM
cargo build --target wasm32-unknown-unknown --release

# The compiled WASM will be at:
# target/wasm32-unknown-unknown/release/counter_contract.wasm
```

### 2. Start Accumen Sequencer

Start the local sequencer:
```bash
# Build accumen if not already built
go build -o bin/accumen ./cmd/accumen

# Start sequencer with local config
./bin/accumen \
  --role=sequencer \
  --config=config/local.yaml \
  --rpc=:8666 \
  --log-level=info
```

You should see output like:
```
INFO[2024-01-15T10:30:00Z] Starting Accumen sequencer
INFO[2024-01-15T10:30:00Z] RPC server started on :8666
INFO[2024-01-15T10:30:00Z] Sequencer running with 1s block time
```

### 3. Build and Use accucli

```bash
# Build the CLI
go build -o bin/accucli ./cmd/accucli

# Check sequencer status
./bin/accucli status --rpc=http://127.0.0.1:8666
```

Expected output:
```json
{
  "chain_id": "accumen-local",
  "height": 0,
  "running": true,
  "last_anchor": null
}
```

### 4. Deploy the Counter Contract

Deploy your compiled WASM contract:
```bash
./bin/accucli deploy \
  --rpc=http://127.0.0.1:8666 \
  --addr=acc://counter.acme \
  --wasm=target/wasm32-unknown-unknown/release/counter_contract.wasm
```

Expected output:
```json
{
  "success": true,
  "address": "acc://counter.acme",
  "wasm_hash": "a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456",
  "wasm_size": 1024
}
```

### 5. Submit Increment Transactions (3x)

Submit three increment transactions to test the counter:

```bash
# First increment
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --entry=increment

# Second increment
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --entry=increment

# Third increment
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --entry=increment
```

Each command should return output like:
```json
{
  "tx_hash": "0x1234567890abcdef...",
  "status": "pending",
  "block_height": 1
}
```

You can also pass arguments to contract functions:
```bash
# Increment by a specific amount
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --entry=increment \
  --arg=amount:5
```

### 6. Query the Counter Value

Check the current counter value:
```bash
./bin/accucli query \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://counter.acme \
  --key=counter
```

Expected output:
```json
{
  "contract": "acc://counter.acme",
  "key": "counter",
  "exists": true,
  "value": "3",
  "type": "uint64"
}
```

### 7. Check Sequencer Status Again

Verify the sequencer has processed blocks:
```bash
./bin/accucli status --rpc=http://127.0.0.1:8666
```

Expected output:
```json
{
  "chain_id": "accumen-local",
  "height": 4,
  "running": true,
  "last_anchor": "2024-01-15T10:35:00Z"
}
```

### 8. Monitor DN Metadata Writes

The sequencer automatically writes transaction metadata to the Accumulate Directory Network (DN). You can see these writes in the sequencer logs:

```
INFO[2024-01-15T10:35:15Z] Writing metadata to DN url=acc://accumen-metadata.acme/2024/01/15/block-1-tx-0-a1b2c3d4.json
INFO[2024-01-15T10:35:15Z] Metadata written successfully calls=1 total_bytes=512
INFO[2024-01-15T10:35:16Z] Writing metadata to DN url=acc://accumen-metadata.acme/2024/01/15/block-2-tx-0-e5f6g7h8.json
INFO[2024-01-15T10:35:16Z] Metadata written successfully calls=2 total_bytes=1024
INFO[2024-01-15T10:35:17Z] Writing metadata to DN url=acc://accumen-metadata.acme/2024/01/15/block-3-tx-0-i9j0k1l2.json
INFO[2024-01-15T10:35:17Z] Metadata written successfully calls=3 total_bytes=1536
```

Each successful transaction generates a metadata entry written to the DN with the following structure:

```json
{
  "version": "1.0",
  "chainId": "accumen-local",
  "blockHeight": 1,
  "blockTime": "2024-01-15T10:35:15Z",
  "txIndex": 0,
  "txHash": "0x1234567890abcdef...",
  "contract": "acc://counter.acme",
  "entry": "increment",
  "args": {},
  "events": [
    {"type": "counter.incremented", "data": {"old": 0, "new": 1}}
  ],
  "gasUsed": 1000,
  "signature": "0xabcdef1234567890..."
}
```

### 9. View DN Metadata (Optional)

If you have access to the Accumulate network tools, you can query the metadata directly:

```bash
# Example URLs for the metadata written above:
# acc://accumen-metadata.acme/2024/01/15/block-1-tx-0-a1b2c3d4.json
# acc://accumen-metadata.acme/2024/01/15/block-2-tx-0-e5f6g7h8.json
# acc://accumen-metadata.acme/2024/01/15/block-3-tx-0-i9j0k1l2.json

# Query via Accumulate CLI (if available)
accumulate account get acc://accumen-metadata.acme/2024/01/15/block-1-tx-0-a1b2c3d4.json

# Or via L0 API directly
curl -X POST http://localhost:26660/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "query",
    "params": {
      "url": "acc://accumen-metadata.acme/2024/01/15/block-1-tx-0-a1b2c3d4.json"
    }
  }'
```

## Advanced Usage

### Contract with Complex Arguments

```bash
# Submit transaction with multiple arguments
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://calculator.acme \
  --entry=add \
  --arg=a:10 \
  --arg=b:20 \
  --arg=operator:add
```

### Query Multiple Keys

```bash
# Query different state keys
./bin/accucli query --contract=acc://counter.acme --key=counter
./bin/accucli query --contract=acc://counter.acme --key=owner
./bin/accucli query --contract=acc://counter.acme --key=initialized
```

### Using Different RPC Endpoints

```bash
# Connect to different sequencer instance
./bin/accucli status --rpc=http://localhost:8667

# All commands support the --rpc flag
./bin/accucli deploy --rpc=http://staging.example.com:8666 --addr=acc://test.acme --wasm=contract.wasm
```

## Troubleshooting

### Common Issues

1. **Connection Refused**
   ```
   Error: RPC call failed: HTTP request failed: dial tcp 127.0.0.1:8666: connect: connection refused
   ```
   - Ensure the sequencer is running
   - Check the RPC endpoint URL

2. **Contract Already Exists**
   ```json
   {
     "error": "Contract already deployed at address: acc://counter.acme"
   }
   ```
   - Use a different contract address
   - Or redeploy to the same address if supported

3. **Invalid WASM File**
   ```
   Error: RPC error: Invalid WASM module: missing magic bytes
   ```
   - Verify the WASM file is valid
   - Rebuild the contract with correct target

4. **Transaction Failed**
   ```json
   {
     "tx_hash": "0x...",
     "status": "failed",
     "error": "execution failed: out of gas"
   }
   ```
   - Check contract logic for infinite loops
   - Verify sufficient gas is available

### Debug Tips

- Use `--log-level=debug` when starting the sequencer for verbose output
- Monitor sequencer logs in real-time: `tail -f logs/sequencer.log`
- Check sequencer health: `curl http://localhost:8667/healthz`
- View metrics: `curl http://localhost:8667/debug/vars`

## Raw Transaction Submission

Accumen now supports submitting L1 transactions directly via JSON-RPC using either structured JSON or raw CBOR format. This provides more control over transaction parameters and ensures deterministic hashing.

### JSON Format Transaction Submission

Submit a raw L1 transaction using structured JSON parameters:

```bash
curl -X POST http://127.0.0.1:8666 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "accumen.submitTx",
    "params": {
      "contract": "acc://counter.acme",
      "entry": "increment",
      "args": {
        "amount": 5
      }
    }
  }'
```

**Response:**
```json
{
  "id": 1,
  "result": {
    "txHash": "a1b2c3d4e5f6789012345678901234567890123456789012345678901234567890"
  }
}
```

### CBOR Format Transaction Submission

For maximum efficiency and deterministic serialization, submit transactions using raw CBOR:

#### Using Python with cbor2

```python
import requests
import cbor2
import base64
import time
import os

# Create L1 transaction
tx = {
    "contract": "acc://counter.acme",
    "entry": "increment",
    "args": {"amount": 5},
    "nonce": os.urandom(16),  # 16 random bytes
    "timestamp": int(time.time() * 1_000_000_000)  # nanoseconds
}

# Encode to CBOR and base64
cbor_data = cbor2.dumps(tx)
base64_cbor = base64.b64encode(cbor_data).decode('ascii')

# Submit transaction
response = requests.post('http://127.0.0.1:8666', json={
    "id": 2,
    "method": "accumen.submitTx",
    "params": {
        "rawCBOR": base64_cbor
    }
})

result = response.json()
print(f"Transaction hash: {result['result']['txHash']}")
```

#### Using Go with fxamacker/cbor

```go
package main

import (
    "bytes"
    "crypto/rand"
    "encoding/base64"
    "encoding/json"
    "fmt"
    "net/http"
    "time"

    "github.com/fxamacker/cbor/v2"
)

type L1Tx struct {
    Contract  string         `cbor:"contract"`
    Entry     string         `cbor:"entry"`
    Args      map[string]any `cbor:"args"`
    Nonce     []byte         `cbor:"nonce"`
    Timestamp int64          `cbor:"timestamp"`
}

func main() {
    // Create L1 transaction
    nonce := make([]byte, 16)
    rand.Read(nonce)

    tx := L1Tx{
        Contract:  "acc://counter.acme",
        Entry:     "increment",
        Args:      map[string]any{"amount": 5},
        Nonce:     nonce,
        Timestamp: time.Now().UnixNano(),
    }

    // Encode to CBOR
    cborData, err := cbor.Marshal(tx)
    if err != nil {
        panic(err)
    }

    // Encode to base64
    base64CBOR := base64.StdEncoding.EncodeToString(cborData)

    // Submit transaction
    reqBody := map[string]interface{}{
        "id":     3,
        "method": "accumen.submitTx",
        "params": map[string]string{
            "rawCBOR": base64CBOR,
        },
    }

    reqBytes, _ := json.Marshal(reqBody)
    resp, err := http.Post("http://127.0.0.1:8666",
        "application/json", bytes.NewBuffer(reqBytes))
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    var result map[string]interface{}
    json.NewDecoder(resp.Body).Decode(&result)

    fmt.Printf("Transaction hash: %s\n",
        result["result"].(map[string]interface{})["txHash"])
}
```

#### Using Node.js with cbor

```javascript
const cbor = require('cbor');
const crypto = require('crypto');
const fetch = require('node-fetch');

// Create L1 transaction
const tx = {
    contract: "acc://counter.acme",
    entry: "increment",
    args: { amount: 5 },
    nonce: crypto.randomBytes(16),
    timestamp: Date.now() * 1000000 // nanoseconds
};

// Encode to CBOR and base64
const cborData = cbor.encode(tx);
const base64CBOR = cborData.toString('base64');

// Submit transaction
const response = await fetch('http://127.0.0.1:8666', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
        id: 4,
        method: 'accumen.submitTx',
        params: {
            rawCBOR: base64CBOR
        }
    })
});

const result = await response.json();
console.log('Transaction hash:', result.result.txHash);
```

### Transaction Hash Calculation

The L1 transaction hash is calculated as SHA256 of the canonical CBOR encoding:

```python
import hashlib
import cbor2

tx = {
    "contract": "acc://counter.acme",
    "entry": "increment",
    "args": {"amount": 5},
    "nonce": bytes([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]),
    "timestamp": 1640995200000000000
}

# Encode to canonical CBOR (deterministic ordering)
cbor_data = cbor2.dumps(tx, canonical=True)

# Calculate SHA256 hash
tx_hash = hashlib.sha256(cbor_data).hexdigest()
print(f"Transaction hash: {tx_hash}")
```

### Mempool Persistence

Accumen automatically persists all submitted transactions to disk:

- **Persistent storage**: Transactions survive node restarts
- **Automatic replay**: Unprocessed transactions are replayed on startup
- **Badger backend**: High-performance embedded database
- **ACID properties**: Consistent transaction ordering

You can see mempool replay in the logs during startup:
```
INFO[2024-01-15T10:30:00Z] Persistent mempool initialized at: data/l1/mempool
INFO[2024-01-15T10:30:00Z] Starting mempool replay
INFO[2024-01-15T10:30:00Z] Mempool replay completed: replayed=5 errors=0
```

### Performance Considerations

1. **CBOR vs JSON**: CBOR is ~20% smaller and faster to process
2. **Pre-generate nonces**: Avoid blocking on crypto.rand for high throughput
3. **Batch submissions**: Group related transactions together
4. **Connection pooling**: Reuse HTTP connections for multiple requests

## Next Steps

- Explore more complex contract patterns
- Set up multiple sequencer nodes
- Integrate with Accumulate mainnet/testnet
- Build frontend applications using the RPC API
- Monitor production deployments with metrics and health checks