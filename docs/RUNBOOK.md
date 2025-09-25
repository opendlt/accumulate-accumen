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

### 2. Run Accumen L1 (Manual Mode)

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

### Key Configuration Parameters

```yaml
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

### Configuration Templates
See `config/` directory for environment-specific templates.

### Log Patterns
Common log patterns to watch for:
- `ERROR.*mempool.*full` - Mempool capacity issues
- `ERROR.*execution.*timeout` - Execution timeouts
- `ERROR.*bridge.*failed` - Bridge connectivity issues
- `WARN.*gas.*limit` - Gas limit warnings