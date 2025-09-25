//! Simple counter contract example for Accumen
//!
//! This contract maintains a global counter that can be incremented,
//! decremented, and queried. It demonstrates basic state management
//! and transaction handling in the Accumen WASM runtime.

#![no_std]
#![no_main]

use accumen_abi::{
    Storage, Transaction, Gas, Log, Result, AccumenError,
    Serialize, Deserialize, serde_json, log, ensure, export_main
};

/// Counter state stored in the key-value store
#[derive(Serialize, Deserialize, Debug, Clone)]
struct CounterState {
    /// Current counter value
    pub value: i64,
    /// Number of increment operations
    pub increments: u64,
    /// Number of decrement operations
    pub decrements: u64,
    /// Last transaction ID that modified the counter
    pub last_tx_id: String,
    /// Block height when counter was last modified
    pub last_block_height: u64,
}

impl Default for CounterState {
    fn default() -> Self {
        Self {
            value: 0,
            increments: 0,
            decrements: 0,
            last_tx_id: String::new(),
            last_block_height: 0,
        }
    }
}

/// Commands that can be executed on the counter
#[derive(Serialize, Deserialize, Debug)]
#[serde(tag = "type", content = "params")]
enum CounterCommand {
    /// Increment the counter by the specified amount (default: 1)
    Increment { amount: Option<i64> },
    /// Decrement the counter by the specified amount (default: 1)
    Decrement { amount: Option<i64> },
    /// Set the counter to a specific value
    Set { value: i64 },
    /// Get the current counter value and statistics
    Get,
    /// Reset the counter to zero
    Reset,
}

/// Response from counter operations
#[derive(Serialize, Deserialize, Debug)]
struct CounterResponse {
    /// Whether the operation was successful
    pub success: bool,
    /// Current counter value after the operation
    pub value: i64,
    /// Operation statistics
    pub stats: CounterStats,
    /// Error message if operation failed
    pub error: Option<String>,
}

/// Counter statistics
#[derive(Serialize, Deserialize, Debug)]
struct CounterStats {
    pub increments: u64,
    pub decrements: u64,
    pub last_tx_id: String,
    pub last_block_height: u64,
    pub total_operations: u64,
}

/// Storage keys
const COUNTER_STATE_KEY: &[u8] = b"counter_state";
const OPERATION_LOG_PREFIX: &[u8] = b"op_log_";

/// Gas costs for different operations
const GAS_COST_READ: u64 = 100;
const GAS_COST_WRITE: u64 = 200;
const GAS_COST_COMPUTE: u64 = 50;
const GAS_COST_LOG: u64 = 25;

/// Load counter state from storage
fn load_counter_state() -> Result<CounterState> {
    Gas::consume(GAS_COST_READ)?;

    match Storage::get_json(COUNTER_STATE_KEY)? {
        Some(state) => {
            log!(info, "Loaded counter state: value={}", state.value);
            Ok(state)
        }
        None => {
            log!(info, "No existing counter state, using default");
            Ok(CounterState::default())
        }
    }
}

/// Save counter state to storage
fn save_counter_state(state: &CounterState) -> Result<()> {
    Gas::consume(GAS_COST_WRITE)?;

    Storage::set_json(COUNTER_STATE_KEY, state)?;
    log!(info, "Saved counter state: value={}", state.value);
    Ok(())
}

/// Log an operation for audit trail
fn log_operation(op_type: &str, old_value: i64, new_value: i64) -> Result<()> {
    Gas::consume(GAS_COST_LOG)?;

    let tx_context = Transaction::context()?;
    let log_key = format!("{}{}_{}",
        core::str::from_utf8(OPERATION_LOG_PREFIX).unwrap(),
        tx_context.block_height,
        tx_context.id
    );

    let log_entry = serde_json::json!({
        "operation": op_type,
        "old_value": old_value,
        "new_value": new_value,
        "tx_id": tx_context.id,
        "sender": tx_context.sender,
        "block_height": tx_context.block_height,
        "timestamp": tx_context.block_timestamp
    });

    Storage::set_json(log_key.as_bytes(), &log_entry)?;
    Ok(())
}

/// Handle increment command
fn handle_increment(amount: Option<i64>) -> Result<CounterResponse> {
    let mut state = load_counter_state()?;
    let old_value = state.value;
    let increment = amount.unwrap_or(1);

    Gas::consume(GAS_COST_COMPUTE)?;

    // Check for overflow
    if increment > 0 && state.value > i64::MAX - increment {
        return Ok(CounterResponse {
            success: false,
            value: state.value,
            stats: CounterStats {
                increments: state.increments,
                decrements: state.decrements,
                last_tx_id: state.last_tx_id,
                last_block_height: state.last_block_height,
                total_operations: state.increments + state.decrements,
            },
            error: Some("Integer overflow".to_string()),
        });
    }

    state.value += increment;
    state.increments += 1;
    state.last_tx_id = Transaction::id()?;
    state.last_block_height = accumen_abi::Block::height();

    save_counter_state(&state)?;
    log_operation("increment", old_value, state.value)?;

    log!(info, "Incremented counter by {} from {} to {}", increment, old_value, state.value);

    Ok(CounterResponse {
        success: true,
        value: state.value,
        stats: CounterStats {
            increments: state.increments,
            decrements: state.decrements,
            last_tx_id: state.last_tx_id.clone(),
            last_block_height: state.last_block_height,
            total_operations: state.increments + state.decrements,
        },
        error: None,
    })
}

/// Handle decrement command
fn handle_decrement(amount: Option<i64>) -> Result<CounterResponse> {
    let mut state = load_counter_state()?;
    let old_value = state.value;
    let decrement = amount.unwrap_or(1);

    Gas::consume(GAS_COST_COMPUTE)?;

    // Check for underflow
    if decrement > 0 && state.value < i64::MIN + decrement {
        return Ok(CounterResponse {
            success: false,
            value: state.value,
            stats: CounterStats {
                increments: state.increments,
                decrements: state.decrements,
                last_tx_id: state.last_tx_id,
                last_block_height: state.last_block_height,
                total_operations: state.increments + state.decrements,
            },
            error: Some("Integer underflow".to_string()),
        });
    }

    state.value -= decrement;
    state.decrements += 1;
    state.last_tx_id = Transaction::id()?;
    state.last_block_height = accumen_abi::Block::height();

    save_counter_state(&state)?;
    log_operation("decrement", old_value, state.value)?;

    log!(info, "Decremented counter by {} from {} to {}", decrement, old_value, state.value);

    Ok(CounterResponse {
        success: true,
        value: state.value,
        stats: CounterStats {
            increments: state.increments,
            decrements: state.decrements,
            last_tx_id: state.last_tx_id.clone(),
            last_block_height: state.last_block_height,
            total_operations: state.increments + state.decrements,
        },
        error: None,
    })
}

/// Handle set command
fn handle_set(value: i64) -> Result<CounterResponse> {
    let mut state = load_counter_state()?;
    let old_value = state.value;

    Gas::consume(GAS_COST_COMPUTE)?;

    state.value = value;
    state.last_tx_id = Transaction::id()?;
    state.last_block_height = accumen_abi::Block::height();

    save_counter_state(&state)?;
    log_operation("set", old_value, state.value)?;

    log!(info, "Set counter from {} to {}", old_value, state.value);

    Ok(CounterResponse {
        success: true,
        value: state.value,
        stats: CounterStats {
            increments: state.increments,
            decrements: state.decrements,
            last_tx_id: state.last_tx_id.clone(),
            last_block_height: state.last_block_height,
            total_operations: state.increments + state.decrements,
        },
        error: None,
    })
}

/// Handle get command
fn handle_get() -> Result<CounterResponse> {
    let state = load_counter_state()?;

    Gas::consume(GAS_COST_COMPUTE)?;

    log!(info, "Retrieved counter value: {}", state.value);

    Ok(CounterResponse {
        success: true,
        value: state.value,
        stats: CounterStats {
            increments: state.increments,
            decrements: state.decrements,
            last_tx_id: state.last_tx_id.clone(),
            last_block_height: state.last_block_height,
            total_operations: state.increments + state.decrements,
        },
        error: None,
    })
}

/// Handle reset command
fn handle_reset() -> Result<CounterResponse> {
    let mut state = load_counter_state()?;
    let old_value = state.value;

    Gas::consume(GAS_COST_COMPUTE)?;

    state.value = 0;
    state.increments = 0;
    state.decrements = 0;
    state.last_tx_id = Transaction::id()?;
    state.last_block_height = accumen_abi::Block::height();

    save_counter_state(&state)?;
    log_operation("reset", old_value, state.value)?;

    log!(info, "Reset counter from {} to 0", old_value);

    Ok(CounterResponse {
        success: true,
        value: state.value,
        stats: CounterStats {
            increments: state.increments,
            decrements: state.decrements,
            last_tx_id: state.last_tx_id.clone(),
            last_block_height: state.last_block_height,
            total_operations: state.increments + state.decrements,
        },
        error: None,
    })
}

/// Main entry point for the counter contract
fn counter_main() -> Result<()> {
    log!(info, "Counter contract started");

    // Check if we have enough gas for basic operations
    ensure!(Gas::remaining() >= 1000, "Insufficient gas");

    // Get transaction data and parse command
    let tx_data = Transaction::data()?;
    let command: CounterCommand = serde_json::from_slice(&tx_data)
        .map_err(|e| AccumenError::SerializationError(e.to_string()))?;

    log!(info, "Processing command: {:?}", command);

    // Execute the command
    let response = match command {
        CounterCommand::Increment { amount } => handle_increment(amount)?,
        CounterCommand::Decrement { amount } => handle_decrement(amount)?,
        CounterCommand::Set { value } => handle_set(value)?,
        CounterCommand::Get => handle_get()?,
        CounterCommand::Reset => handle_reset()?,
    };

    // Store the response in a well-known location for the caller to retrieve
    let response_json = serde_json::to_vec(&response)
        .map_err(|e| AccumenError::SerializationError(e.to_string()))?;

    Storage::set(b"last_response", &response_json)?;

    if response.success {
        log!(info, "Command executed successfully. Counter value: {}", response.value);
    } else {
        log!(error, "Command failed: {:?}", response.error);
    }

    Ok(())
}

// Export the main function for the WASM runtime
export_main!(counter_main);

/// Initialize function (called when contract is first deployed)
#[no_mangle]
pub extern "C" fn init() -> i32 {
    log!(info, "Initializing counter contract");

    match load_counter_state() {
        Ok(_) => {
            log!(info, "Counter contract initialized successfully");
            0
        }
        Err(e) => {
            log!(error, "Failed to initialize counter contract: {}", e);
            1
        }
    }
}

/// Query function for read-only operations (doesn't consume gas or modify state)
#[no_mangle]
pub extern "C" fn query() -> i32 {
    match handle_get() {
        Ok(response) => {
            if let Ok(response_json) = serde_json::to_vec(&response) {
                if Storage::set(b"query_response", &response_json).is_ok() {
                    return 0;
                }
            }
            1
        }
        Err(_) => 1,
    }
}