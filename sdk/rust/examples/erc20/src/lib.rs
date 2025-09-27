use serde::{Deserialize, Serialize};
use std::collections::HashMap;

// ERC20 Token State
#[derive(Serialize, Deserialize, Default)]
pub struct TokenState {
    pub name: String,
    pub symbol: String,
    pub decimals: u8,
    pub total_supply: u64,
    pub balances: HashMap<String, u64>,
    pub allowances: HashMap<String, HashMap<String, u64>>,
}

// Events
#[derive(Serialize, Deserialize)]
pub struct TransferEvent {
    pub from: String,
    pub to: String,
    pub value: u64,
}

#[derive(Serialize, Deserialize)]
pub struct ApprovalEvent {
    pub owner: String,
    pub spender: String,
    pub value: u64,
}

// WASM imports for L1 runtime
extern "C" {
    fn get_state(key_ptr: *const u8, key_len: usize) -> i32;
    fn set_state(key_ptr: *const u8, key_len: usize, value_ptr: *const u8, value_len: usize);
    fn emit_event(event_ptr: *const u8, event_len: usize);
    fn get_caller() -> *mut u8;
    fn get_caller_len() -> usize;
    fn abort_with_message(msg_ptr: *const u8, msg_len: usize);
}

// Memory management
static mut MEMORY_BUFFER: [u8; 8192] = [0; 8192];
static mut MEMORY_OFFSET: usize = 0;

fn allocate(size: usize) -> *mut u8 {
    unsafe {
        if MEMORY_OFFSET + size > MEMORY_BUFFER.len() {
            abort_with_message(b"Out of memory".as_ptr(), 13);
        }
        let ptr = MEMORY_BUFFER.as_mut_ptr().add(MEMORY_OFFSET);
        MEMORY_OFFSET += size;
        ptr
    }
}

// State management
fn load_state() -> TokenState {
    let key = b"state";
    unsafe {
        let result = get_state(key.as_ptr(), key.len());
        if result == 0 {
            return TokenState::default();
        }

        let data_ptr = allocate(result as usize);
        get_state(key.as_ptr(), key.len());
        let data = std::slice::from_raw_parts(data_ptr, result as usize);
        serde_json::from_slice(data).unwrap_or_default()
    }
}

fn save_state(state: &TokenState) {
    let key = b"state";
    let serialized = serde_json::to_vec(state).unwrap();
    unsafe {
        set_state(key.as_ptr(), key.len(), serialized.as_ptr(), serialized.len());
    }
}

fn get_caller_address() -> String {
    unsafe {
        let len = get_caller_len();
        let ptr = get_caller();
        let bytes = std::slice::from_raw_parts(ptr, len);
        String::from_utf8_lossy(bytes).to_string()
    }
}

fn emit_transfer_event(from: &str, to: &str, value: u64) {
    let event = TransferEvent {
        from: from.to_string(),
        to: to.to_string(),
        value,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn emit_approval_event(owner: &str, spender: &str, value: u64) {
    let event = ApprovalEvent {
        owner: owner.to_string(),
        spender: spender.to_string(),
        value,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn abort_execution(message: &str) -> ! {
    unsafe {
        abort_with_message(message.as_ptr(), message.len());
    }
    panic!("Execution aborted");
}

// ERC20 Implementation
#[no_mangle]
pub extern "C" fn initialize() {
    let mut state = TokenState::default();
    state.name = "Example Token".to_string();
    state.symbol = "EXT".to_string();
    state.decimals = 18;
    state.total_supply = 0;
    save_state(&state);
}

#[no_mangle]
pub extern "C" fn name() -> *const u8 {
    let state = load_state();
    let name_bytes = state.name.as_bytes();
    let ptr = allocate(name_bytes.len());
    unsafe {
        std::ptr::copy_nonoverlapping(name_bytes.as_ptr(), ptr, name_bytes.len());
    }
    ptr
}

#[no_mangle]
pub extern "C" fn symbol() -> *const u8 {
    let state = load_state();
    let symbol_bytes = state.symbol.as_bytes();
    let ptr = allocate(symbol_bytes.len());
    unsafe {
        std::ptr::copy_nonoverlapping(symbol_bytes.as_ptr(), ptr, symbol_bytes.len());
    }
    ptr
}

#[no_mangle]
pub extern "C" fn decimals() -> u8 {
    let state = load_state();
    state.decimals
}

#[no_mangle]
pub extern "C" fn total_supply() -> u64 {
    let state = load_state();
    state.total_supply
}

#[no_mangle]
pub extern "C" fn balance_of(account_ptr: *const u8, account_len: usize) -> u64 {
    let account = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(account_ptr, account_len)).to_string()
    };
    let state = load_state();
    *state.balances.get(&account).unwrap_or(&0)
}

#[no_mangle]
pub extern "C" fn allowance(owner_ptr: *const u8, owner_len: usize, spender_ptr: *const u8, spender_len: usize) -> u64 {
    let owner = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(owner_ptr, owner_len)).to_string()
    };
    let spender = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(spender_ptr, spender_len)).to_string()
    };
    let state = load_state();
    state.allowances.get(&owner)
        .and_then(|spenders| spenders.get(&spender))
        .unwrap_or(&0)
        .clone()
}

#[no_mangle]
pub extern "C" fn mint(to_ptr: *const u8, to_len: usize, amount: u64) {
    let caller = get_caller_address();
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    // Only contract deployer can mint (simplified permission model)
    // In production, use proper access control

    let mut state = load_state();

    // Update total supply
    state.total_supply += amount;

    // Update balance
    let current_balance = state.balances.get(&to).unwrap_or(&0);
    state.balances.insert(to.clone(), current_balance + amount);

    save_state(&state);
    emit_transfer_event("", &to, amount);
}

#[no_mangle]
pub extern "C" fn burn(from_ptr: *const u8, from_len: usize, amount: u64) {
    let caller = get_caller_address();
    let from = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(from_ptr, from_len)).to_string()
    };

    // Only account owner can burn their tokens
    if caller != from {
        abort_execution("Unauthorized burn");
    }

    let mut state = load_state();

    let current_balance = state.balances.get(&from).unwrap_or(&0);
    if *current_balance < amount {
        abort_execution("Insufficient balance");
    }

    // Update total supply
    state.total_supply -= amount;

    // Update balance
    state.balances.insert(from.clone(), current_balance - amount);

    save_state(&state);
    emit_transfer_event(&from, "", amount);
}

#[no_mangle]
pub extern "C" fn transfer(to_ptr: *const u8, to_len: usize, amount: u64) -> bool {
    let caller = get_caller_address();
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    if caller == to {
        return true; // Self-transfer is no-op
    }

    let mut state = load_state();

    let from_balance = state.balances.get(&caller).unwrap_or(&0);
    if *from_balance < amount {
        return false;
    }

    let to_balance = state.balances.get(&to).unwrap_or(&0);

    // Update balances
    state.balances.insert(caller.clone(), from_balance - amount);
    state.balances.insert(to.clone(), to_balance + amount);

    save_state(&state);
    emit_transfer_event(&caller, &to, amount);

    true
}

#[no_mangle]
pub extern "C" fn transfer_from(from_ptr: *const u8, from_len: usize, to_ptr: *const u8, to_len: usize, amount: u64) -> bool {
    let caller = get_caller_address();
    let from = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(from_ptr, from_len)).to_string()
    };
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    let mut state = load_state();

    // Check allowance
    let allowed = state.allowances.get(&from)
        .and_then(|spenders| spenders.get(&caller))
        .unwrap_or(&0);

    if *allowed < amount {
        return false;
    }

    // Check balance
    let from_balance = state.balances.get(&from).unwrap_or(&0);
    if *from_balance < amount {
        return false;
    }

    let to_balance = state.balances.get(&to).unwrap_or(&0);

    // Update balances
    state.balances.insert(from.clone(), from_balance - amount);
    state.balances.insert(to.clone(), to_balance + amount);

    // Update allowance
    if let Some(spenders) = state.allowances.get_mut(&from) {
        spenders.insert(caller.clone(), allowed - amount);
    }

    save_state(&state);
    emit_transfer_event(&from, &to, amount);

    true
}

#[no_mangle]
pub extern "C" fn approve(spender_ptr: *const u8, spender_len: usize, amount: u64) -> bool {
    let caller = get_caller_address();
    let spender = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(spender_ptr, spender_len)).to_string()
    };

    let mut state = load_state();

    // Initialize allowances for caller if not exists
    if !state.allowances.contains_key(&caller) {
        state.allowances.insert(caller.clone(), HashMap::new());
    }

    // Set allowance
    if let Some(spenders) = state.allowances.get_mut(&caller) {
        spenders.insert(spender.clone(), amount);
    }

    save_state(&state);
    emit_approval_event(&caller, &spender, amount);

    true
}