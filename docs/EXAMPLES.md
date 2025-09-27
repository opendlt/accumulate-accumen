# Accumen Examples

This document provides a complete walkthrough of deploying and interacting with Accumen contracts, demonstrating the security and economics model where L0 owns the keys and credits come from burning ACME tokens.

## Prerequisites

- Go 1.21+ installed
- Accumulate CLI (`accucli`) built and available
- Access to Accumulate devnet or simulator

## Complete Walkthrough

### 1. Generate Key

First, generate a new key pair for signing transactions:

```bash
accucli keys gen --key-type ed25519 --output my-key.json
```

This creates a new Ed25519 key pair and saves it to `my-key.json`. The key will be used to sign all subsequent transactions.

### 2. Create ADI/Book/Page on Devnet

Start the devnet environment:

```bash
# Using devnet scripts
./ops/devnet-up.sh

# Or using simulator
go run ./cmd/simulator
```

Create an Accumulate Digital Identity (ADI) with associated key book and page:

```bash
# Create ADI
accucli adi create \
  --url http://localhost:16695/v3 \
  --sponsor acc://dn.acme/ACME \
  --key-file my-key.json \
  my-identity

# Create key book
accucli page create \
  --url http://localhost:16695/v3 \
  --sponsor acc://my-identity \
  --key-file my-key.json \
  acc://my-identity/book

# Create key page
accucli page create \
  --url http://localhost:16695/v3 \
  --sponsor acc://my-identity/book \
  --key-file my-key.json \
  acc://my-identity/book/1

# Add key to page
accucli key add \
  --url http://localhost:16695/v3 \
  --sponsor acc://my-identity/book/1 \
  --key-file my-key.json \
  --public-key $(accucli keys export my-key.json --public)
```

### 3. Bind Contract to Key Page

Create authority binding to link your contract with the key page:

```bash
accucli authority bind \
  --url http://localhost:16695/v3 \
  --sponsor acc://my-identity/book/1 \
  --key-file my-key.json \
  --contract-address 0x1234567890123456789012345678901234567890 \
  --authority acc://my-identity/book/1 \
  --scope-file authority-scope.yaml
```

Example `authority-scope.yaml`:

```yaml
authority_binding:
  contract_address: "0x1234567890123456789012345678901234567890"
  key_page_url: "acc://my-identity/book/1"
  permissions:
    - action: "deploy"
      rate_limit: 10
    - action: "submit"
      rate_limit: 100
    - action: "query"
      rate_limit: 1000
  emergency_controls:
    pause_contract: true
    revoke_authority: true
```

### 4. Ensure Credits

Check current credit balance and add credits if needed:

```bash
# Check credit status
accucli credits status \
  --url http://localhost:16695/v3 \
  acc://my-identity/book/1

# Add credits by burning ACME tokens
accucli credits add \
  --url http://localhost:16695/v3 \
  --sponsor acc://dn.acme/ACME \
  --key-file my-key.json \
  --recipient acc://my-identity/book/1 \
  --amount 1000000  # 1 ACME = 1,000,000 credits
```

This demonstrates the economics model: credits are obtained by burning ACME tokens through the AddCredits transaction flow.

### 5. Deploy + Submit + Query

Deploy your Accumen contract:

```bash
# Deploy contract
accucli contract deploy \
  --url http://localhost:16695/v3 \
  --sponsor acc://my-identity/book/1 \
  --key-file my-key.json \
  --contract-file ./examples/counter/counter.wasm \
  --init-data '{"initial_count": 0}'

# Submit transaction to contract
accucli contract submit \
  --url http://localhost:16695/v3 \
  --sponsor acc://my-identity/book/1 \
  --key-file my-key.json \
  --contract-address 0x1234567890123456789012345678901234567890 \
  --method increment \
  --params '{"amount": 1}'

# Query contract state
accucli contract query \
  --url http://localhost:16695/v3 \
  --contract-address 0x1234567890123456789012345678901234567890 \
  --method get_count
```

### 6. Verify DN Write and Follower Receipt

#### DN Write Verification

Each L1 transaction creates a cross-link entry in the L0 Directory Network:

```bash
# Query DN for cross-link metadata
accucli tx query \
  --url http://localhost:16695/v3 \
  --tx-id <l0-transaction-id> \
  --include-metadata

# Verify cross-link format
accucli cross-link verify \
  --l1-tx-hash 0xabcdef1234567890... \
  --l0-tx-id <l0-transaction-id>
```

Expected cross-link format in L0 transaction memo:

```json
{
  "cross_links": [
    {
      "l1_chain_id": "acumen-l1",
      "l1_tx_hash": "0xabcdef1234567890...",
      "l1_block_number": 12345,
      "contract_address": "0x1234567890123456789012345678901234567890",
      "method": "increment",
      "timestamp": "2024-01-15T10:30:00Z"
    }
  ]
}
```

#### Follower Receipt Verification

Followers can reconstruct and verify the transaction chain:

```bash
# Scan DN for receipts
accucli follower scan \
  --url http://localhost:16695/v3 \
  --start-block 100 \
  --end-block 200 \
  --contract-filter 0x1234567890123456789012345678901234567890

# Verify receipt chain
accucli follower verify \
  --receipt-file receipts.json \
  --state-root 0x9876543210... \
  --contract-address 0x1234567890123456789012345678901234567890
```

Receipt verification process:

1. **Receipt Collection**: Follower scans DN for cross-link entries
2. **State Reconstruction**: Rebuilds contract state from transaction sequence
3. **Cryptographic Verification**: Validates signatures and merkle proofs
4. **Consensus Validation**: Confirms transactions were included in valid blocks

Example receipt structure:

```json
{
  "receipt_id": "receipt_12345",
  "l0_transaction": {
    "id": "<l0-tx-id>",
    "block_height": 150,
    "merkle_proof": "0x...",
    "validator_signatures": ["0x...", "0x..."]
  },
  "l1_cross_link": {
    "chain_id": "acumen-l1",
    "tx_hash": "0xabcdef...",
    "block_number": 12345,
    "contract_state_root": "0x9876543210..."
  },
  "verification_status": "valid"
}
```

## Security Model Summary

This walkthrough demonstrates Accumen's security model:

1. **L0 Root of Trust**: All authority derives from Accumulate identity system
2. **Authority Chain**: Contract → Key Page → Signer → Permissions
3. **Cryptographic Binding**: Contract addresses bound to specific key pages
4. **Permission Scoping**: Granular controls over contract operations
5. **Cross-Link Integrity**: L1 transactions anchored in L0 for verifiability

## Economics Model Summary

The economics flow shown:

1. **ACME Burning**: AddCredits transactions burn ACME tokens
2. **Credit Issuance**: Credits issued at gas-to-credits ratio (GCR)
3. **L1 Gas Conversion**: L1 transaction fees paid in credits to L0
4. **Resource Management**: Credits provide anti-spam and resource allocation

This completes the full cycle from key generation through contract deployment, execution, and verification within Accumen's dual-layer architecture.

## Advanced Examples

### Build a Rust Contract to WASM

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

## Meeting a Key Page m-of-n Threshold (Manual/Offline)

Accumulate Key Pages can be configured with threshold requirements (e.g., 2-of-3, 3-of-5) where multiple signatures are required to authorize transactions. Accumen supports offline multi-signature workflows for meeting these thresholds.

### Scenario

You have a Key Page with a 2-of-3 threshold that requires signatures from at least 2 out of 3 authorized keys. The keys are distributed across different machines for security.

### Prerequisites

1. **Keystore Setup**: Each signer has their own keystore with their respective key
2. **Transaction Envelope**: An unsigned transaction envelope ready for signing
3. **Coordination**: A secure way to share the envelope between signers

### Step-by-Step Process

#### 1. Initialize Keystores (Each Signer)

```bash
# Signer A initializes their keystore
accucli keystore init --path ./keystore-alice

# Signer B initializes their keystore
accucli keystore init --path ./keystore-bob

# Signer C initializes their keystore
accucli keystore init --path ./keystore-charlie
```

#### 2. Import or Generate Keys

```bash
# Signer A imports their key
accucli keystore import --keystore ./keystore-alice --alias alice-key --priv <ALICE_PRIVATE_KEY_HEX>

# Signer B imports their key
accucli keystore import --keystore ./keystore-bob --alias bob-key --priv <BOB_PRIVATE_KEY_HEX>

# Signer C imports their key
accucli keystore import --keystore ./keystore-charlie --alias charlie-key --priv <CHARLIE_PRIVATE_KEY_HEX>
```

#### 3. Create Transaction Envelope

The transaction initiator creates an unsigned envelope (this could be done by any party or automated system):

```bash
# This step depends on your specific transaction type
# For example, using the L1 sequencer to create a WriteData transaction
# The envelope is created but not yet signed

# Example unsigned envelope (base64):
export UNSIGNED_ENVELOPE="eyJoZWFkZXIi..."
```

#### 4. First Signature (Signer A)

```bash
# Alice signs the envelope with her key
accucli tx sign \
  --envelope $UNSIGNED_ENVELOPE \
  --keystore ./keystore-alice \
  --alias alice-key

# Output includes the signed envelope:
{
  "success": true,
  "signed_aliases": ["alice-key"],
  "keystore": "./keystore-alice",
  "envelope": "eyJoZWFkZXIi...",  // New base64 envelope with Alice's signature
  "note": "Envelope has been signed with the specified keys"
}

# Save the signed envelope
export SIGNED_BY_ALICE="eyJoZWFkZXIi..."
```

#### 5. Second Signature (Signer B)

```bash
# Bob receives the envelope signed by Alice and adds his signature
accucli tx sign \
  --envelope $SIGNED_BY_ALICE \
  --keystore ./keystore-bob \
  --alias bob-key

# Output includes the envelope with both signatures:
{
  "success": true,
  "signed_aliases": ["bob-key"],
  "keystore": "./keystore-bob",
  "envelope": "eyJoZWFkZXIi...",  // New base64 envelope with Alice's + Bob's signatures
  "note": "Envelope has been signed with the specified keys"
}

# Save the envelope with both signatures
export SIGNED_BY_ALICE_AND_BOB="eyJoZWFkZXIi..."
```

#### 6. Submit the Multi-Signed Envelope

```bash
# Submit the envelope with 2 signatures (meeting the 2-of-3 threshold)
accucli tx submit \
  --envelope $SIGNED_BY_ALICE_AND_BOB \
  --l0 https://testnet.accumulate.net/v3

# Output:
{
  "success": true,
  "transaction_hash": "abc123...",
  "l0_endpoint": "https://testnet.accumulate.net/v3"
}
```

### Advanced: Multiple Signatures in One Command

If a single party has access to multiple keys, they can sign with multiple aliases in one command:

```bash
# Sign with multiple keys from the same keystore
accucli tx sign \
  --envelope $UNSIGNED_ENVELOPE \
  --keystore ./keystore-multi \
  --alias key1 \
  --alias key2 \
  --alias key3
```

### Security Considerations

1. **Key Distribution**: Never store multiple threshold keys on the same machine
2. **Envelope Verification**: Validate envelope contents before signing
3. **Secure Transport**: Use secure channels to share envelopes between signers
4. **Audit Trail**: Log all signing operations with timestamps and key aliases
5. **Backup**: Maintain secure backups of keystores

### Troubleshooting

#### Invalid Signatures
```bash
# Verify envelope before submission
accucli tx submit --envelope $ENVELOPE --l0 $L0_ENDPOINT --dry-run
```

#### Missing Keys
```bash
# List available keys in keystore
accucli keystore list --keystore ./keystore-path
```

#### Threshold Not Met
Ensure you have enough signatures. Check the Key Page configuration:
- Query the Key Page on Accumulate to verify the threshold requirement
- Count the signatures in your envelope
- Verify all signatures are from authorized keys

### Example: 3-of-5 Threshold

For a 3-of-5 threshold, you need at least 3 signatures:

```bash
# Step 1: Create base envelope (unsigned)
export BASE_ENVELOPE="..."

# Step 2: Signer 1 signs
export ENV_1=$(accucli tx sign --envelope $BASE_ENVELOPE --keystore ./ks1 --alias key1 | jq -r '.envelope')

# Step 3: Signer 2 adds their signature
export ENV_2=$(accucli tx sign --envelope $ENV_1 --keystore ./ks2 --alias key2 | jq -r '.envelope')

# Step 4: Signer 3 adds their signature (threshold met)
export ENV_3=$(accucli tx sign --envelope $ENV_2 --keystore ./ks3 --alias key3 | jq -r '.envelope')

# Step 5: Submit
accucli tx submit --envelope $ENV_3 --l0 $L0_ENDPOINT
```

### Integration with Authority Bindings

When using Accumen's authority binding system, the keystore aliases should match the aliases configured in the bindings:

```bash
# List bindings to see which keys are configured for which contracts
accucli authority list --config ./accumen.yaml

# The key aliases in your keystore should match the KeyAlias field in the bindings
```

This ensures that the contract-specific signer selection works correctly with your multi-signature workflow.

## Tokens on L1 (ACME as base currency; Credits from L0)

Accumen provides ready-to-use token implementations that demonstrate how to build DeFi applications on L1 while leveraging L0's credit system. These examples show how ACME tokens burned on L0 provide credits for L1 operations.

### Token Standard Examples

The SDK includes three standard token implementations in Rust:

#### ERC20 - Fungible Tokens

**Location**: `sdk/rust/examples/erc20/`

A standard fungible token implementation with mint, burn, transfer, and approval functionality:

```rust
// Core ERC20 functions
#[no_mangle]
pub extern "C" fn mint(to_ptr: *const u8, to_len: usize, amount: u64);

#[no_mangle]
pub extern "C" fn transfer(to_ptr: *const u8, to_len: usize, amount: u64) -> bool;

#[no_mangle]
pub extern "C" fn approve(spender_ptr: *const u8, spender_len: usize, amount: u64) -> bool;

#[no_mangle]
pub extern "C" fn balance_of(account_ptr: *const u8, account_len: usize) -> u64;
```

**Features**:
- **State management**: Balances and allowances stored in HashMap
- **Events**: Transfer and Approval events emitted for indexing
- **Access control**: Simplified permission model (expandable)
- **L0 integration**: Metadata written to DN for all operations

**Build and deploy**:
```bash
cd sdk/rust/examples/erc20
cargo build --target wasm32-unknown-unknown --release

# Deploy to Accumen
./bin/accucli deploy \
  --addr=acc://game-tokens.acme \
  --wasm=target/wasm32-unknown-unknown/release/erc20_example.wasm \
  --rpc=http://localhost:8666
```

#### ERC721 - Non-Fungible Tokens (NFTs)

**Location**: `sdk/rust/examples/erc721/`

A complete NFT implementation with ownership, transfers, and approvals:

```rust
// Core ERC721 functions
#[no_mangle]
pub extern "C" fn mint(to_ptr: *const u8, to_len: usize, uri_ptr: *const u8, uri_len: usize) -> u64;

#[no_mangle]
pub extern "C" fn transfer_from(from_ptr: *const u8, from_len: usize, to_ptr: *const u8, to_len: usize, token_id: u64);

#[no_mangle]
pub extern "C" fn approve(to_ptr: *const u8, to_len: usize, token_id: u64);

#[no_mangle]
pub extern "C" fn owner_of(token_id: u64) -> *const u8;
```

**Features**:
- **Unique ownership**: Each token has a single owner
- **Metadata URIs**: Token metadata stored with URIs
- **Operator approvals**: Approve all tokens or individual tokens
- **Enumeration**: Balance tracking per owner

**Use cases**:
- Digital collectibles
- Game items and artifacts
- Identity tokens
- Certificate of authenticity

#### ERC1155 - Multi-Token Standard

**Location**: `sdk/rust/examples/erc1155/`

A flexible multi-token standard supporting both fungible and non-fungible tokens:

```rust
// Core ERC1155 functions
#[no_mangle]
pub extern "C" fn mint_batch(to_ptr: *const u8, to_len: usize, ids_ptr: *const u8, ids_len: usize, amounts_ptr: *const u8, amounts_len: usize);

#[no_mangle]
pub extern "C" fn safe_batch_transfer_from(from_ptr: *const u8, from_len: usize, to_ptr: *const u8, to_len: usize, ids_ptr: *const u8, ids_len: usize, amounts_ptr: *const u8, amounts_len: usize);

#[no_mangle]
pub extern "C" fn balance_of_batch(accounts_ptr: *const u8, accounts_len: usize, ids_ptr: *const u8, ids_len: usize) -> *const u8;
```

**Features**:
- **Batch operations**: Mint and transfer multiple token types at once
- **Mixed tokens**: Fungible and non-fungible in same contract
- **Gas efficiency**: Batch operations reduce transaction costs
- **Flexible metadata**: URI pattern for token metadata

**Use cases**:
- Gaming ecosystems (weapons, armor, consumables)
- DeFi baskets (multiple assets in one transaction)
- Supply chain tracking (batch processing)

### Economic Model

The token examples demonstrate Accumen's dual-layer economics:

#### L0 Foundation Layer
- **ACME base currency**: Native token of Accumulate network
- **Credit system**: ACME burned → Credits issued for L1 operations
- **Authority chain**: L0 identities control L1 contract permissions

#### L1 Application Layer
- **Custom tokens**: Application-specific currencies and assets
- **Gas payment**: L1 operations consume L0 credits
- **Cross-layer value**: L1 tokens can represent L0 assets or vice versa

### Running the Token Tests

Comprehensive end-to-end tests demonstrate full token lifecycle:

```bash
# Set environment variable to enable simulator tests
export ACC_SIM=1

# Run all token tests
go test ./tests/e2e -run TestTokenExamples -v

# Run specific token test
go test ./tests/e2e -run TestTokenExamples/ERC20 -v
go test ./tests/e2e -run TestTokenExamples/ERC721 -v
go test ./tests/e2e -run TestTokenExamples/ERC1155 -v
```

**Test flow for each token standard**:

1. **Setup**: Start Accumulate simulator and Accumen sequencer
2. **Deploy**: Deploy token contract to L1
3. **Operations**: Execute token operations (mint, transfer, etc.)
4. **Verification**: Verify DN metadata writes for all operations
5. **L0 integration**: Confirm metadata stored in Accumulate DN

**Expected output**:
```
=== RUN   TestTokenExamples/ERC20
    tokens_test.go:45: 🚀 Starting ERC20 Token Test
    tokens_test.go:89: ✅ Sequencer started successfully
    tokens_test.go:96: 📦 Deploying ERC20 contract at acc://erc20-test.acme
    tokens_test.go:99: ✅ ERC20 contract deployed with tx hash: deploy-acc://erc20-test.acme-1640995200
    tokens_test.go:106: 💰 Minting 1000 tokens to acc://test-user.acme
    tokens_test.go:112: ✅ Mint transaction hash: invoke-acc://erc20-test.acme-mint-1640995202
    tokens_test.go:118: 💸 Transferring 100 tokens from acc://test-user.acme to acc://recipient.acme
    tokens_test.go:124: ✅ Transfer transaction hash: invoke-acc://erc20-test.acme-transfer-1640995204
    tokens_test.go:131: 🔍 Verifying DN metadata writes for ERC20 operations
    tokens_test.go:135: ✅ Verified metadata at acc://accumen-metadata.acme/2024/01/15/tx-deploy-acc://erc20-test.acme-1640995200.json
    tokens_test.go:139: 🎉 ERC20 Token Test completed successfully
--- PASS: TestTokenExamples/ERC20 (12.34s)
```

### Integration Patterns

#### Game Economy Example

```rust
// Multi-token game economy using ERC20 + ERC721 + ERC1155
#[no_mangle]
pub extern "C" fn purchase_item_with_tokens() {
    // 1. Check ERC20 balance (game currency)
    let currency_balance = erc20_balance_of(&caller);
    require(currency_balance >= item_price, "Insufficient currency");

    // 2. Burn ERC20 tokens
    erc20_burn(&caller, item_price);

    // 3. Mint ERC721 unique item
    let item_id = erc721_mint(&caller, &item_metadata_uri);

    // 4. Grant ERC1155 consumables as bonus
    erc1155_mint(&caller, &[health_potion_id], &[5]);

    // 5. Emit game event (automatically written to DN)
    emit_event(&PurchaseEvent { item_id, price: item_price });
}
```

#### DeFi Basket Example

```rust
// ERC1155 representing basket of L0 assets
#[no_mangle]
pub extern "C" fn create_asset_basket() {
    // Token ID represents different asset types
    let asset_ids = vec![1, 2, 3]; // ACME, STBL, other tokens
    let amounts = vec![100, 200, 50];

    // Mint basket tokens to user
    erc1155_mint_batch(&caller, &asset_ids, &amounts);

    // Stage L0 operations to lock underlying assets
    for (asset_id, amount) in asset_ids.iter().zip(amounts.iter()) {
        l0_lock_tokens(*asset_id, *amount);
    }
}
```

### Credit Consumption Model

Each token operation consumes L0 credits according to complexity:

| Operation | Credit Cost | L0 Metadata Size |
|-----------|------------|------------------|
| ERC20 transfer | ~100 credits | ~512 bytes |
| ERC721 mint | ~150 credits | ~768 bytes |
| ERC1155 batch (5 items) | ~300 credits | ~1024 bytes |
| Contract deployment | ~1000 credits | ~2048 bytes |

**Credit provisioning**:
```bash
# Check current credits
curl -X POST http://localhost:26660/v3 \
  -d '{"method": "query", "params": {"url": "acc://my-key-page.acme"}}'

# Add credits by burning ACME
curl -X POST http://localhost:26660/v3 \
  -d '{
    "method": "execute",
    "params": {
      "envelope": "...",
      "transaction": {"type": "addCredits", "amount": 1000000}
    }
  }'
```

### Production Deployment

#### Multi-Sequencer Setup

```yaml
# config/production.yaml
sequencers:
  - role: primary
    addr: "seq1.example.com:8666"
    tokens: ["acc://game-tokens.acme", "acc://game-items.acme"]
  - role: replica
    addr: "seq2.example.com:8666"
    tokens: ["acc://defi-basket.acme"]

# Load balancing token operations across sequencers
```

#### Monitoring Token Operations

```promql
# Prometheus queries for token operations
rate(accumen_transactions_executed_total{contract=~".*token.*"}[5m])

# Token-specific error rates
rate(accumen_l0_failed_total{contract="acc://game-tokens.acme"}[5m])

# Credit consumption by token type
increase(accumen_wasm_gas_used_total{contract=~".*erc.*"}[1h])
```

### Security Considerations

1. **Access control**: Implement proper mint/burn permissions
2. **Overflow protection**: Use safe arithmetic for balance operations
3. **Reentrancy**: Guard against cross-contract call attacks
4. **L0 authority**: Ensure proper L0 identity binding for contract operations
5. **Credit management**: Monitor and maintain L0 credit balances

### Next Steps for Token Development

1. **Custom implementations**: Extend examples for specific use cases
2. **Cross-chain bridges**: Connect L1 tokens to other networks
3. **DeFi protocols**: Build lending, swapping, and yield farming
4. **NFT marketplaces**: Create trading platforms for ERC721/1155
5. **Governance tokens**: Implement voting and proposal systems

The token examples provide a solid foundation for building sophisticated DeFi and gaming applications on Accumen while leveraging Accumulate's robust L0 infrastructure.

## Next Steps

- Explore more complex contract patterns
- Set up multiple sequencer nodes
- Integrate with Accumulate mainnet/testnet
- Build frontend applications using the RPC API
- Monitor production deployments with metrics and health checks