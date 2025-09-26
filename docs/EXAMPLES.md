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

## Onboard a Contract as ADI Delegate

This walkthrough demonstrates how to set up an Accumulate Decentralized Identity (ADI) and configure it so that a contract can legally stage L0 operations. This enables contracts to write data to the Accumulate network, send tokens, and perform other L0 operations on behalf of the ADI.

### Prerequisites

- Running Accumen sequencer
- Access to Accumulate L0 network (mainnet or testnet)
- Ed25519 key pair for signing transactions
- ACME credits for transaction fees

### Step 1: Generate Keys and Prepare Configuration

First, generate an Ed25519 key pair for your ADI:

```bash
# Generate a new Ed25519 key pair (you can use any tool that generates Ed25519 keys)
# For development, you can use the accumulate CLI or OpenSSL

# Example with OpenSSL:
openssl genpkey -algorithm Ed25519 -out adi_private_key.pem
openssl pkey -in adi_private_key.pem -pubout -out adi_public_key.pem

# Extract the hex-encoded public key
openssl pkey -in adi_public_key.pem -pubin -text -noout
```

Save your configuration:

```yaml
# config/adi_setup.yaml
identity:
  url: "acc://mycompany.acme"
  key_book_url: "acc://mycompany.acme/book"
  key_page_url: "acc://mycompany.acme/book/1"
  data_account_url: "acc://mydata.mycompany.acme"
contract:
  url: "acc://mycontract.mycompany.acme"
keys:
  public_key_hex: "your-ed25519-public-key-hex-here"
  private_key_hex: "your-ed25519-private-key-hex-here"
credits:
  initial_amount: 1000000  # 1 million credits
```

### Step 2: Create Go Program to Setup ADI Delegation

Create a setup program using the Accumen auth helpers:

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/internal/crypto/devsigner"
)

func main() {
	// Configuration
	config := &l0api.ContractDelegationConfig{
		IdentityURL:    "acc://mycompany.acme",
		KeyBookURL:     "acc://mycompany.acme/book",
		KeyPageURL:     "acc://mycompany.acme/book/1",
		ContractURL:    "acc://mycontract.mycompany.acme",
		DataAccountURL: "acc://mydata.mycompany.acme",
		PublicKeyHex:   "your-ed25519-public-key-hex-here",
		Permissions: []l0api.AuthorityPermission{
			l0api.PermissionWriteData,
			l0api.PermissionSendTokens,
		},
		InitialCredits: 1000000, // 1 million credits
	}

	// Validate configuration
	if err := config.ValidateConfig(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Create L0 client
	clientConfig := &l0api.ClientConfig{
		Endpoint: "https://testnet.accumulatenetwork.io/v3",
		Timeout:  30 * time.Second,
	}

	client, err := l0api.NewClient(clientConfig)
	if err != nil {
		log.Fatalf("Failed to create L0 client: %v", err)
	}

	// Create signer with your private key
	signer, err := devsigner.New("your-ed25519-private-key-hex-here")
	if err != nil {
		log.Fatalf("Failed to create signer: %v", err)
	}

	// Build all delegation setup transactions
	envelopes, err := l0api.BuildFullDelegationSetup(config)
	if err != nil {
		log.Fatalf("Failed to build delegation setup: %v", err)
	}

	fmt.Printf("Created %d transactions for ADI delegation setup\n", len(envelopes))

	// Execute transactions in order
	ctx := context.Background()
	for i, envelope := range envelopes {
		fmt.Printf("Submitting transaction %d/%d...\n", i+1, len(envelopes))

		// Sign the transaction
		signedEnvelope, err := signer.Sign(envelope)
		if err != nil {
			log.Fatalf("Failed to sign transaction %d: %v", i+1, err)
		}

		// Submit to L0 network
		result, err := client.Submit(ctx, signedEnvelope)
		if err != nil {
			log.Fatalf("Failed to submit transaction %d: %v", i+1, err)
		}

		fmt.Printf("Transaction %d submitted successfully: %x\n", i+1, result.TransactionHash)

		// Wait between transactions to avoid nonce issues
		time.Sleep(2 * time.Second)
	}

	fmt.Println("ADI delegation setup completed successfully!")
	fmt.Printf("Contract %s can now stage L0 operations for identity %s\n",
		config.ContractURL, config.IdentityURL)
}
```

### Step 3: Execute the Setup

Build and run the setup program:

```bash
# Build the program
go mod tidy
go build -o adi-setup ./cmd/adi-setup

# Run the setup
./adi-setup
```

Expected output:
```
Created 6 transactions for ADI delegation setup
Submitting transaction 1/6...
Transaction 1 submitted successfully: a1b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef123456
Submitting transaction 2/6...
Transaction 2 submitted successfully: b2c3d4e5f6789012345678901234567890abcdef1234567890abcdef1234567a
...
ADI delegation setup completed successfully!
Contract acc://mycontract.mycompany.acme can now stage L0 operations for identity acc://mycompany.acme
```

### Step 4: Deploy Contract with Delegation Authority

Now deploy your contract that can perform L0 operations:

```rust
// src/lib.rs - Example Rust contract with L0 staging capability
#[no_mangle]
pub extern "C" fn write_data_to_l0() {
    // Get the data to write
    let data = b"Hello from Accumen contract!";

    // Stage L0 write data operation
    // The contract URL (acc://mycontract.mycompany.acme) now has authority
    // to write data to acc://mydata.mycompany.acme
    unsafe {
        l0_write_data(
            b"acc://mydata.mycompany.acme\0".as_ptr(),
            data.as_ptr(),
            data.len() as u32,
        );
    }
}

#[no_mangle]
pub extern "C" fn send_tokens_via_l0() {
    // Stage L0 send tokens operation
    // The contract can now send tokens from the ADI's token accounts
    let recipient = b"acc://recipient.acme\0";
    let amount = 1000000u64; // 1 ACME

    unsafe {
        l0_send_tokens(
            b"acc://tokens.mycompany.acme\0".as_ptr(),
            recipient.as_ptr(),
            amount,
        );
    }
}

// External function declarations for L0 operations
extern "C" {
    fn l0_write_data(account: *const u8, data: *const u8, len: u32);
    fn l0_send_tokens(from: *const u8, to: *const u8, amount: u64);
}
```

Build and deploy the contract:

```bash
# Build the WASM contract
cargo build --target wasm32-unknown-unknown --release

# Deploy via Accumen CLI
./bin/accucli deploy \
  --rpc=http://127.0.0.1:8666 \
  --addr=acc://mycontract.mycompany.acme \
  --wasm=target/wasm32-unknown-unknown/release/my_contract.wasm
```

### Step 5: Test Contract L0 Operations

Execute contract functions that perform L0 operations:

```bash
# Test data writing capability
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://mycontract.mycompany.acme \
  --entry=write_data_to_l0

# Test token sending capability
./bin/accucli submit \
  --rpc=http://127.0.0.1:8666 \
  --contract=acc://mycontract.mycompany.acme \
  --entry=send_tokens_via_l0
```

### Step 6: Verify L0 Operations

Check that the L0 operations were successfully staged and submitted:

```bash
# Check the sequencer logs for L0 operation staging
tail -f logs/sequencer.log | grep "L0 operation staged"

# Expected output:
# INFO[2024-01-15T10:35:15Z] L0 operation staged: WriteData to acc://mydata.mycompany.acme
# INFO[2024-01-15T10:35:16Z] L0 operation staged: SendTokens from acc://tokens.mycompany.acme to acc://recipient.acme

# Verify data was written to the Accumulate network
curl -X POST https://testnet.accumulatenetwork.io/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "query",
    "params": {
      "url": "acc://mydata.mycompany.acme"
    }
  }'

# Verify token transfer
curl -X POST https://testnet.accumulatenetwork.io/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "id": 1,
    "method": "query",
    "params": {
      "url": "acc://tokens.mycompany.acme"
    }
  }'
```

### Individual Helper Functions

You can also use the individual helper functions for specific operations:

#### Create Identity Only

```go
envelope, err := l0api.BuildCreateIdentity(
    "acc://mycompany.acme",
    "acc://mycompany.acme/book",
    "your-public-key-hex",
)
```

#### Create Key Book Only

```go
envelope, err := l0api.BuildCreateKeyBook(
    "acc://mycompany.acme/book",
    "your-public-key-hex",
)
```

#### Update Account Authority

```go
envelope, err := l0api.BuildUpdateAccountAuthDelegate(
    "acc://mydata.mycompany.acme",      // Account to update
    "acc://mycontract.mycompany.acme",  // Contract to delegate to
    "acc://mycompany.acme/book",        // Key book for authority
    l0api.PermissionWriteData,          // Permissions to grant
    l0api.PermissionSendTokens,
)
```

#### Remove Contract Delegation

```go
envelope, err := l0api.BuildRemoveContractDelegate(
    "acc://mydata.mycompany.acme",      // Account to update
    "acc://mycontract.mycompany.acme",  // Contract to remove
)
```

### Permission Levels

The system supports different permission levels for contracts:

- **PermissionSign**: Basic signing capability
- **PermissionWriteData**: Can write data to accounts
- **PermissionSendTokens**: Can send tokens and write data
- **PermissionUpdateAuth**: Can update account authority (dangerous)
- **PermissionFull**: All permissions (very dangerous)

### Security Considerations

1. **Principle of Least Privilege**: Grant contracts only the minimum permissions needed
2. **Key Security**: Store private keys securely, never in code or public repositories
3. **Contract Auditing**: Audit contracts thoroughly before granting delegation authority
4. **Permission Review**: Regularly review and rotate delegation permissions
5. **Monitoring**: Monitor L0 operations staged by contracts for suspicious activity

### Troubleshooting

#### Common Issues

1. **"Authority not found" errors**:
   - Ensure the ADI setup completed successfully
   - Verify the contract URL has been granted delegation authority
   - Check that the key page has sufficient credits

2. **"Insufficient credits" errors**:
   - Add more credits to the key page: `BuildAddCreditsToKeyPage()`
   - Verify credit balance before executing operations

3. **"Invalid signature" errors**:
   - Ensure the private key matches the public key used in setup
   - Verify the signer is correctly configured

4. **Contract execution failures**:
   - Check sequencer logs for detailed error messages
   - Verify the contract WASM is valid and properly deployed
   - Ensure L0 operations are correctly formatted

#### Monitoring Commands

```bash
# Monitor sequencer L0 operations
tail -f logs/sequencer.log | grep "L0"

# Check ADI status
curl -X POST https://testnet.accumulatenetwork.io/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "method": "query",
    "params": {"url": "acc://mycompany.acme"}
  }'

# Check contract delegation status
curl -X POST https://testnet.accumulatenetwork.io/v3 \
  -H "Content-Type: application/json" \
  -d '{
    "method": "query-directory",
    "params": {"url": "acc://mycompany.acme"}
  }'
```

This completes the setup process. Your contract can now legally stage L0 operations through the delegated ADI authority, enabling powerful cross-chain capabilities while maintaining security through Accumulate's authority system.

## Next Steps

- Explore more complex contract patterns
- Set up multiple sequencer nodes
- Integrate with Accumulate mainnet/testnet
- Build frontend applications using the RPC API
- Monitor production deployments with metrics and health checks