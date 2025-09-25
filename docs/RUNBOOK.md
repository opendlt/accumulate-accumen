# Accumen Operations Runbook

This runbook provides operational procedures for running, monitoring, and troubleshooting Accumen in production environments.

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