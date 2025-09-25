//! Accumen ABI - Rust bindings for the Accumen WASM runtime
//!
//! This crate provides safe Rust bindings for interacting with the Accumen
//! execution environment from within WASM modules.

#![cfg_attr(not(feature = "std"), no_std)]

#[cfg(feature = "alloc")]
extern crate alloc;

#[cfg(feature = "alloc")]
use alloc::{string::String, vec::Vec};

#[cfg(feature = "std")]
use std::{string::String, vec::Vec};

use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Error types for Accumen ABI operations
#[derive(Error, Debug)]
pub enum AccumenError {
    #[error("Invalid key: {0}")]
    InvalidKey(String),
    #[error("Key not found: {0}")]
    KeyNotFound(String),
    #[error("Buffer too small: needed {needed}, got {actual}")]
    BufferTooSmall { needed: usize, actual: usize },
    #[error("Gas limit exceeded")]
    OutOfGas,
    #[error("Invalid transaction data")]
    InvalidTransaction,
    #[error("Execution aborted: {0}")]
    Aborted(String),
    #[error("Serialization error: {0}")]
    SerializationError(String),
}

pub type Result<T> = core::result::Result<T, AccumenError>;

/// Log levels for the Accumen runtime
#[repr(u32)]
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum LogLevel {
    Debug = 1,
    Info = 2,
    Warn = 3,
    Error = 4,
}

/// Transaction context information
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransactionContext {
    pub id: String,
    pub sender: String,
    pub data: Vec<u8>,
    pub gas_limit: u64,
    pub block_height: u64,
    pub block_timestamp: u64,
}

/// External host functions provided by the Accumen runtime
extern "C" {
    /// Get a value from the key-value store
    fn accuwasm_get(key_ptr: *const u8, key_len: u32, value_ptr: *mut u8, value_len: u32) -> i32;

    /// Set a value in the key-value store
    fn accuwasm_set(key_ptr: *const u8, key_len: u32, value_ptr: *const u8, value_len: u32);

    /// Delete a value from the key-value store
    fn accuwasm_delete(key_ptr: *const u8, key_len: u32);

    /// Create a new iterator with optional key prefix
    fn accuwasm_iterator_new(prefix_ptr: *const u8, prefix_len: u32) -> i32;

    /// Get the next key-value pair from an iterator
    fn accuwasm_iterator_next(
        iterator_id: i32,
        key_ptr: *mut u8,
        key_len: u32,
        value_ptr: *mut u8,
        value_len: u32,
    ) -> i32;

    /// Close an iterator
    fn accuwasm_iterator_close(iterator_id: i32);

    /// Log a message with the specified level
    fn accuwasm_log(level: u32, msg_ptr: *const u8, msg_len: u32);

    /// Get the current transaction ID
    fn accuwasm_tx_get_id(id_ptr: *mut u8, id_len: u32) -> i32;

    /// Get the transaction sender address
    fn accuwasm_tx_get_sender(sender_ptr: *mut u8, sender_len: u32) -> i32;

    /// Get the transaction data
    fn accuwasm_tx_get_data(data_ptr: *mut u8, data_len: u32) -> i32;

    /// Get remaining gas
    fn accuwasm_gas_remaining() -> u64;

    /// Consume gas
    fn accuwasm_gas_consume(amount: u64);

    /// Get current block height
    fn accuwasm_block_height() -> u64;

    /// Get current block timestamp
    fn accuwasm_block_timestamp() -> u64;

    /// Allocate memory (for host-managed allocation)
    fn accuwasm_alloc(size: u32) -> *mut u8;

    /// Free memory (for host-managed allocation)
    fn accuwasm_free(ptr: *mut u8);

    /// Abort execution with error message
    fn accuwasm_abort(msg_ptr: *const u8, msg_len: u32) -> !;
}

/// Safe wrapper for key-value store operations
pub struct Storage;

impl Storage {
    /// Get a value from storage
    pub fn get(key: &[u8]) -> Result<Option<Vec<u8>>> {
        const MAX_VALUE_SIZE: usize = 1024 * 1024; // 1MB max
        let mut buffer = vec![0u8; MAX_VALUE_SIZE];

        let result = unsafe {
            accuwasm_get(
                key.as_ptr(),
                key.len() as u32,
                buffer.as_mut_ptr(),
                buffer.len() as u32,
            )
        };

        match result {
            -1 => Ok(None), // Key not found
            len if len >= 0 => {
                buffer.truncate(len as usize);
                Ok(Some(buffer))
            }
            _ => Err(AccumenError::BufferTooSmall {
                needed: (-result) as usize,
                actual: MAX_VALUE_SIZE,
            }),
        }
    }

    /// Set a value in storage
    pub fn set(key: &[u8], value: &[u8]) -> Result<()> {
        unsafe {
            accuwasm_set(
                key.as_ptr(),
                key.len() as u32,
                value.as_ptr(),
                value.len() as u32,
            );
        }
        Ok(())
    }

    /// Delete a value from storage
    pub fn delete(key: &[u8]) -> Result<()> {
        unsafe {
            accuwasm_delete(key.as_ptr(), key.len() as u32);
        }
        Ok(())
    }

    /// Get a JSON-serialized value from storage
    pub fn get_json<T: for<'de> Deserialize<'de>>(key: &[u8]) -> Result<Option<T>> {
        match Self::get(key)? {
            Some(data) => {
                let value = serde_json::from_slice(&data)
                    .map_err(|e| AccumenError::SerializationError(e.to_string()))?;
                Ok(Some(value))
            }
            None => Ok(None),
        }
    }

    /// Set a JSON-serialized value in storage
    pub fn set_json<T: Serialize>(key: &[u8], value: &T) -> Result<()> {
        let data = serde_json::to_vec(value)
            .map_err(|e| AccumenError::SerializationError(e.to_string()))?;
        Self::set(key, &data)
    }
}

/// Iterator for key-value store
pub struct StorageIterator {
    id: i32,
    closed: bool,
}

impl StorageIterator {
    /// Create a new iterator with optional key prefix
    pub fn new(prefix: Option<&[u8]>) -> Result<Self> {
        let id = unsafe {
            match prefix {
                Some(p) => accuwasm_iterator_new(p.as_ptr(), p.len() as u32),
                None => accuwasm_iterator_new(core::ptr::null(), 0),
            }
        };

        if id < 0 {
            return Err(AccumenError::InvalidKey("Failed to create iterator".to_string()));
        }

        Ok(StorageIterator { id, closed: false })
    }

    /// Get the next key-value pair
    pub fn next(&mut self) -> Result<Option<(Vec<u8>, Vec<u8>)>> {
        if self.closed {
            return Ok(None);
        }

        const MAX_SIZE: usize = 64 * 1024; // 64KB max per key/value
        let mut key_buffer = vec![0u8; MAX_SIZE];
        let mut value_buffer = vec![0u8; MAX_SIZE];

        let result = unsafe {
            accuwasm_iterator_next(
                self.id,
                key_buffer.as_mut_ptr(),
                key_buffer.len() as u32,
                value_buffer.as_mut_ptr(),
                value_buffer.len() as u32,
            )
        };

        match result {
            0 => Ok(None), // No more items
            1 => {
                // Find actual lengths (null-terminated or full buffer)
                let key_len = key_buffer.iter().position(|&x| x == 0).unwrap_or(key_buffer.len());
                let value_len = value_buffer.iter().position(|&x| x == 0).unwrap_or(value_buffer.len());

                key_buffer.truncate(key_len);
                value_buffer.truncate(value_len);

                Ok(Some((key_buffer, value_buffer)))
            }
            _ => Err(AccumenError::InvalidKey("Iterator error".to_string())),
        }
    }
}

impl Drop for StorageIterator {
    fn drop(&mut self) {
        if !self.closed {
            unsafe {
                accuwasm_iterator_close(self.id);
            }
            self.closed = true;
        }
    }
}

/// Logging functions
pub struct Log;

impl Log {
    pub fn debug(msg: &str) {
        unsafe {
            accuwasm_log(LogLevel::Debug as u32, msg.as_ptr(), msg.len() as u32);
        }
    }

    pub fn info(msg: &str) {
        unsafe {
            accuwasm_log(LogLevel::Info as u32, msg.as_ptr(), msg.len() as u32);
        }
    }

    pub fn warn(msg: &str) {
        unsafe {
            accuwasm_log(LogLevel::Warn as u32, msg.as_ptr(), msg.len() as u32);
        }
    }

    pub fn error(msg: &str) {
        unsafe {
            accuwasm_log(LogLevel::Error as u32, msg.as_ptr(), msg.len() as u32);
        }
    }
}

/// Transaction context functions
pub struct Transaction;

impl Transaction {
    /// Get the current transaction ID
    pub fn id() -> Result<String> {
        let mut buffer = vec![0u8; 256];
        let len = unsafe {
            accuwasm_tx_get_id(buffer.as_mut_ptr(), buffer.len() as u32)
        };

        if len < 0 {
            return Err(AccumenError::InvalidTransaction);
        }

        buffer.truncate(len as usize);
        String::from_utf8(buffer)
            .map_err(|_| AccumenError::InvalidTransaction)
    }

    /// Get the transaction sender address
    pub fn sender() -> Result<String> {
        let mut buffer = vec![0u8; 256];
        let len = unsafe {
            accuwasm_tx_get_sender(buffer.as_mut_ptr(), buffer.len() as u32)
        };

        if len < 0 {
            return Err(AccumenError::InvalidTransaction);
        }

        buffer.truncate(len as usize);
        String::from_utf8(buffer)
            .map_err(|_| AccumenError::InvalidTransaction)
    }

    /// Get the transaction data
    pub fn data() -> Result<Vec<u8>> {
        let mut buffer = vec![0u8; 1024 * 1024]; // 1MB max
        let len = unsafe {
            accuwasm_tx_get_data(buffer.as_mut_ptr(), buffer.len() as u32)
        };

        if len < 0 {
            return Err(AccumenError::BufferTooSmall {
                needed: (-len) as usize,
                actual: buffer.len(),
            });
        }

        buffer.truncate(len as usize);
        Ok(buffer)
    }

    /// Get transaction context as a structured object
    pub fn context() -> Result<TransactionContext> {
        let id = Self::id()?;
        let sender = Self::sender()?;
        let data = Self::data()?;
        let gas_limit = Gas::remaining();
        let block_height = Block::height();
        let block_timestamp = Block::timestamp();

        Ok(TransactionContext {
            id,
            sender,
            data,
            gas_limit,
            block_height,
            block_timestamp,
        })
    }
}

/// Gas-related functions
pub struct Gas;

impl Gas {
    /// Get remaining gas
    pub fn remaining() -> u64 {
        unsafe { accuwasm_gas_remaining() }
    }

    /// Consume gas
    pub fn consume(amount: u64) -> Result<()> {
        let remaining = Self::remaining();
        if remaining < amount {
            return Err(AccumenError::OutOfGas);
        }

        unsafe {
            accuwasm_gas_consume(amount);
        }
        Ok(())
    }

    /// Check if enough gas is available
    pub fn check(amount: u64) -> bool {
        Self::remaining() >= amount
    }
}

/// Block context functions
pub struct Block;

impl Block {
    /// Get current block height
    pub fn height() -> u64 {
        unsafe { accuwasm_block_height() }
    }

    /// Get current block timestamp (Unix timestamp)
    pub fn timestamp() -> u64 {
        unsafe { accuwasm_block_timestamp() }
    }
}

/// Abort execution with an error message
pub fn abort(msg: &str) -> ! {
    unsafe {
        accuwasm_abort(msg.as_ptr(), msg.len() as u32);
    }
}

/// Panic handler for no_std environments
#[cfg(not(feature = "std"))]
#[panic_handler]
fn panic(info: &core::panic::PanicInfo) -> ! {
    let msg = if let Some(s) = info.payload().downcast_ref::<&str>() {
        *s
    } else {
        "panic occurred"
    };
    abort(msg)
}

/// Macro for easier error handling with abort
#[macro_export]
macro_rules! ensure {
    ($cond:expr, $msg:expr) => {
        if !($cond) {
            $crate::abort($msg);
        }
    };
}

/// Macro for easier logging
#[macro_export]
macro_rules! log {
    (debug, $($arg:tt)*) => {
        $crate::Log::debug(&format!($($arg)*))
    };
    (info, $($arg:tt)*) => {
        $crate::Log::info(&format!($($arg)*))
    };
    (warn, $($arg:tt)*) => {
        $crate::Log::warn(&format!($($arg)*))
    };
    (error, $($arg:tt)*) => {
        $crate::Log::error(&format!($($arg)*))
    };
}

/// Export a main function for WASM modules
#[macro_export]
macro_rules! export_main {
    ($func:ident) => {
        #[no_mangle]
        pub extern "C" fn main() -> i32 {
            match $func() {
                Ok(()) => 0,
                Err(e) => {
                    $crate::Log::error(&format!("Execution failed: {}", e));
                    1
                }
            }
        }
    };
}

// Re-export commonly used types
pub use serde::{Deserialize, Serialize};
pub use serde_json;

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_log_levels() {
        assert_eq!(LogLevel::Debug as u32, 1);
        assert_eq!(LogLevel::Info as u32, 2);
        assert_eq!(LogLevel::Warn as u32, 3);
        assert_eq!(LogLevel::Error as u32, 4);
    }
}