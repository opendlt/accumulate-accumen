use serde::{Deserialize, Serialize};
use std::collections::HashMap;

// ERC1155 Multi-Token State
#[derive(Serialize, Deserialize, Default)]
pub struct MultiTokenState {
    pub next_token_id: u64,
    pub balances: HashMap<String, HashMap<u64, u64>>, // owner -> token_id -> balance
    pub token_uris: HashMap<u64, String>,            // token_id -> URI
    pub operator_approvals: HashMap<String, HashMap<String, bool>>, // owner -> operator -> approved
}

// Events
#[derive(Serialize, Deserialize)]
pub struct TransferSingleEvent {
    pub operator: String,
    pub from: String,
    pub to: String,
    pub id: u64,
    pub value: u64,
}

#[derive(Serialize, Deserialize)]
pub struct TransferBatchEvent {
    pub operator: String,
    pub from: String,
    pub to: String,
    pub ids: Vec<u64>,
    pub values: Vec<u64>,
}

#[derive(Serialize, Deserialize)]
pub struct ApprovalForAllEvent {
    pub account: String,
    pub operator: String,
    pub approved: bool,
}

#[derive(Serialize, Deserialize)]
pub struct URIEvent {
    pub value: String,
    pub id: u64,
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
static mut MEMORY_BUFFER: [u8; 16384] = [0; 16384]; // Larger buffer for batch operations
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
fn load_state() -> MultiTokenState {
    let key = b"state";
    unsafe {
        let result = get_state(key.as_ptr(), key.len());
        if result == 0 {
            return MultiTokenState::default();
        }

        let data_ptr = allocate(result as usize);
        get_state(key.as_ptr(), key.len());
        let data = std::slice::from_raw_parts(data_ptr, result as usize);
        serde_json::from_slice(data).unwrap_or_default()
    }
}

fn save_state(state: &MultiTokenState) {
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

fn emit_transfer_single_event(operator: &str, from: &str, to: &str, id: u64, value: u64) {
    let event = TransferSingleEvent {
        operator: operator.to_string(),
        from: from.to_string(),
        to: to.to_string(),
        id,
        value,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn emit_transfer_batch_event(operator: &str, from: &str, to: &str, ids: Vec<u64>, values: Vec<u64>) {
    let event = TransferBatchEvent {
        operator: operator.to_string(),
        from: from.to_string(),
        to: to.to_string(),
        ids,
        values,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn emit_approval_for_all_event(account: &str, operator: &str, approved: bool) {
    let event = ApprovalForAllEvent {
        account: account.to_string(),
        operator: operator.to_string(),
        approved,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn emit_uri_event(value: &str, id: u64) {
    let event = URIEvent {
        value: value.to_string(),
        id,
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

// ERC1155 Implementation
#[no_mangle]
pub extern "C" fn initialize() {
    let mut state = MultiTokenState::default();
    state.next_token_id = 1;
    save_state(&state);
}

#[no_mangle]
pub extern "C" fn balance_of(account_ptr: *const u8, account_len: usize, id: u64) -> u64 {
    let account = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(account_ptr, account_len)).to_string()
    };
    let state = load_state();
    state.balances.get(&account)
        .and_then(|tokens| tokens.get(&id))
        .unwrap_or(&0)
        .clone()
}

#[no_mangle]
pub extern "C" fn balance_of_batch(accounts_ptr: *const u8, accounts_len: usize, ids_ptr: *const u8, ids_len: usize) -> *const u8 {
    // Parse accounts and ids from JSON arrays
    let accounts_json = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(accounts_ptr, accounts_len))
    };
    let ids_json = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(ids_ptr, ids_len))
    };

    let accounts: Vec<String> = serde_json::from_str(&accounts_json).unwrap_or_default();
    let ids: Vec<u64> = serde_json::from_str(&ids_json).unwrap_or_default();

    if accounts.len() != ids.len() {
        abort_execution("Accounts and ids length mismatch");
    }

    let state = load_state();
    let mut balances = Vec::new();

    for (account, id) in accounts.iter().zip(ids.iter()) {
        let balance = state.balances.get(account)
            .and_then(|tokens| tokens.get(id))
            .unwrap_or(&0)
            .clone();
        balances.push(balance);
    }

    let result_json = serde_json::to_string(&balances).unwrap();
    let result_bytes = result_json.as_bytes();
    let ptr = allocate(result_bytes.len());
    unsafe {
        std::ptr::copy_nonoverlapping(result_bytes.as_ptr(), ptr, result_bytes.len());
    }
    ptr
}

#[no_mangle]
pub extern "C" fn uri(id: u64) -> *const u8 {
    let state = load_state();
    if let Some(uri) = state.token_uris.get(&id) {
        let uri_bytes = uri.as_bytes();
        let ptr = allocate(uri_bytes.len());
        unsafe {
            std::ptr::copy_nonoverlapping(uri_bytes.as_ptr(), ptr, uri_bytes.len());
        }
        ptr
    } else {
        std::ptr::null()
    }
}

#[no_mangle]
pub extern "C" fn is_approved_for_all(account_ptr: *const u8, account_len: usize, operator_ptr: *const u8, operator_len: usize) -> bool {
    let account = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(account_ptr, account_len)).to_string()
    };
    let operator = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(operator_ptr, operator_len)).to_string()
    };
    let state = load_state();
    state.operator_approvals.get(&account)
        .and_then(|operators| operators.get(&operator))
        .unwrap_or(&false)
        .clone()
}

#[no_mangle]
pub extern "C" fn mint(to_ptr: *const u8, to_len: usize, id: u64, amount: u64, uri_ptr: *const u8, uri_len: usize) {
    let caller = get_caller_address();
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };
    let uri = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(uri_ptr, uri_len)).to_string()
    };

    let mut state = load_state();

    // Set URI if provided and token doesn't exist
    if !uri.is_empty() && !state.token_uris.contains_key(&id) {
        state.token_uris.insert(id, uri.clone());
        emit_uri_event(&uri, id);
    }

    // Initialize balances for 'to' if not exists
    if !state.balances.contains_key(&to) {
        state.balances.insert(to.clone(), HashMap::new());
    }

    // Update balance
    if let Some(tokens) = state.balances.get_mut(&to) {
        let current_balance = tokens.get(&id).unwrap_or(&0);
        tokens.insert(id, current_balance + amount);
    }

    save_state(&state);
    emit_transfer_single_event(&caller, "", &to, id, amount);
}

#[no_mangle]
pub extern "C" fn mint_batch(to_ptr: *const u8, to_len: usize, ids_ptr: *const u8, ids_len: usize, amounts_ptr: *const u8, amounts_len: usize) {
    let caller = get_caller_address();
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    // Parse ids and amounts from JSON arrays
    let ids_json = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(ids_ptr, ids_len))
    };
    let amounts_json = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(amounts_ptr, amounts_len))
    };

    let ids: Vec<u64> = serde_json::from_str(&ids_json).unwrap_or_default();
    let amounts: Vec<u64> = serde_json::from_str(&amounts_json).unwrap_or_default();

    if ids.len() != amounts.len() {
        abort_execution("Ids and amounts length mismatch");
    }

    let mut state = load_state();

    // Initialize balances for 'to' if not exists
    if !state.balances.contains_key(&to) {
        state.balances.insert(to.clone(), HashMap::new());
    }

    // Update balances
    if let Some(tokens) = state.balances.get_mut(&to) {
        for (id, amount) in ids.iter().zip(amounts.iter()) {
            let current_balance = tokens.get(id).unwrap_or(&0);
            tokens.insert(*id, current_balance + amount);
        }
    }

    save_state(&state);
    emit_transfer_batch_event(&caller, "", &to, ids, amounts);
}

#[no_mangle]
pub extern "C" fn burn(from_ptr: *const u8, from_len: usize, id: u64, amount: u64) {
    let caller = get_caller_address();
    let from = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(from_ptr, from_len)).to_string()
    };

    let mut state = load_state();

    // Check permission
    if caller != from && !is_approved_for_all_internal(&from, &caller, &state) {
        abort_execution("Unauthorized burn");
    }

    // Check balance
    let current_balance = state.balances.get(&from)
        .and_then(|tokens| tokens.get(&id))
        .unwrap_or(&0);

    if *current_balance < amount {
        abort_execution("Insufficient balance");
    }

    // Update balance
    if let Some(tokens) = state.balances.get_mut(&from) {
        tokens.insert(id, current_balance - amount);
    }

    save_state(&state);
    emit_transfer_single_event(&caller, &from, "", id, amount);
}

#[no_mangle]
pub extern "C" fn safe_transfer_from(from_ptr: *const u8, from_len: usize, to_ptr: *const u8, to_len: usize, id: u64, amount: u64) {
    let caller = get_caller_address();
    let from = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(from_ptr, from_len)).to_string()
    };
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    let mut state = load_state();

    // Check permission
    if caller != from && !is_approved_for_all_internal(&from, &caller, &state) {
        abort_execution("Unauthorized transfer");
    }

    // Check balance
    let current_balance = state.balances.get(&from)
        .and_then(|tokens| tokens.get(&id))
        .unwrap_or(&0);

    if *current_balance < amount {
        abort_execution("Insufficient balance");
    }

    // Initialize balances for 'to' if not exists
    if !state.balances.contains_key(&to) {
        state.balances.insert(to.clone(), HashMap::new());
    }

    // Update balances
    if let Some(from_tokens) = state.balances.get_mut(&from) {
        from_tokens.insert(id, current_balance - amount);
    }

    if let Some(to_tokens) = state.balances.get_mut(&to) {
        let to_balance = to_tokens.get(&id).unwrap_or(&0);
        to_tokens.insert(id, to_balance + amount);
    }

    save_state(&state);
    emit_transfer_single_event(&caller, &from, &to, id, amount);
}

#[no_mangle]
pub extern "C" fn safe_batch_transfer_from(from_ptr: *const u8, from_len: usize, to_ptr: *const u8, to_len: usize, ids_ptr: *const u8, ids_len: usize, amounts_ptr: *const u8, amounts_len: usize) {
    let caller = get_caller_address();
    let from = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(from_ptr, from_len)).to_string()
    };
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    // Parse ids and amounts from JSON arrays
    let ids_json = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(ids_ptr, ids_len))
    };
    let amounts_json = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(amounts_ptr, amounts_len))
    };

    let ids: Vec<u64> = serde_json::from_str(&ids_json).unwrap_or_default();
    let amounts: Vec<u64> = serde_json::from_str(&amounts_json).unwrap_or_default();

    if ids.len() != amounts.len() {
        abort_execution("Ids and amounts length mismatch");
    }

    let mut state = load_state();

    // Check permission
    if caller != from && !is_approved_for_all_internal(&from, &caller, &state) {
        abort_execution("Unauthorized transfer");
    }

    // Check all balances first
    for (id, amount) in ids.iter().zip(amounts.iter()) {
        let current_balance = state.balances.get(&from)
            .and_then(|tokens| tokens.get(id))
            .unwrap_or(&0);

        if *current_balance < *amount {
            abort_execution("Insufficient balance");
        }
    }

    // Initialize balances for 'to' if not exists
    if !state.balances.contains_key(&to) {
        state.balances.insert(to.clone(), HashMap::new());
    }

    // Update all balances
    for (id, amount) in ids.iter().zip(amounts.iter()) {
        // Update from balance
        if let Some(from_tokens) = state.balances.get_mut(&from) {
            let current_balance = from_tokens.get(id).unwrap_or(&0);
            from_tokens.insert(*id, current_balance - amount);
        }

        // Update to balance
        if let Some(to_tokens) = state.balances.get_mut(&to) {
            let to_balance = to_tokens.get(id).unwrap_or(&0);
            to_tokens.insert(*id, to_balance + amount);
        }
    }

    save_state(&state);
    emit_transfer_batch_event(&caller, &from, &to, ids, amounts);
}

#[no_mangle]
pub extern "C" fn set_approval_for_all(operator_ptr: *const u8, operator_len: usize, approved: bool) {
    let caller = get_caller_address();
    let operator = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(operator_ptr, operator_len)).to_string()
    };

    let mut state = load_state();

    // Initialize operator approvals for caller if not exists
    if !state.operator_approvals.contains_key(&caller) {
        state.operator_approvals.insert(caller.clone(), HashMap::new());
    }

    // Set approval
    if let Some(operators) = state.operator_approvals.get_mut(&caller) {
        operators.insert(operator.clone(), approved);
    }

    save_state(&state);
    emit_approval_for_all_event(&caller, &operator, approved);
}

#[no_mangle]
pub extern "C" fn set_uri(id: u64, uri_ptr: *const u8, uri_len: usize) {
    let caller = get_caller_address();
    let uri = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(uri_ptr, uri_len)).to_string()
    };

    // Only contract deployer can set URI (simplified permission model)
    // In production, use proper access control

    let mut state = load_state();
    state.token_uris.insert(id, uri.clone());
    save_state(&state);

    emit_uri_event(&uri, id);
}

// Helper functions
fn is_approved_for_all_internal(account: &str, operator: &str, state: &MultiTokenState) -> bool {
    state.operator_approvals.get(account)
        .and_then(|operators| operators.get(operator))
        .unwrap_or(&false)
        .clone()
}