use serde::{Deserialize, Serialize};
use std::collections::HashMap;

// ERC721 NFT State
#[derive(Serialize, Deserialize, Default)]
pub struct NFTState {
    pub name: String,
    pub symbol: String,
    pub next_token_id: u64,
    pub owners: HashMap<u64, String>,           // token_id -> owner
    pub token_uris: HashMap<u64, String>,       // token_id -> URI
    pub balances: HashMap<String, u64>,         // owner -> count
    pub token_approvals: HashMap<u64, String>,  // token_id -> approved
    pub operator_approvals: HashMap<String, HashMap<String, bool>>, // owner -> operator -> approved
}

// Events
#[derive(Serialize, Deserialize)]
pub struct TransferEvent {
    pub from: String,
    pub to: String,
    pub token_id: u64,
}

#[derive(Serialize, Deserialize)]
pub struct ApprovalEvent {
    pub owner: String,
    pub approved: String,
    pub token_id: u64,
}

#[derive(Serialize, Deserialize)]
pub struct ApprovalForAllEvent {
    pub owner: String,
    pub operator: String,
    pub approved: bool,
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
fn load_state() -> NFTState {
    let key = b"state";
    unsafe {
        let result = get_state(key.as_ptr(), key.len());
        if result == 0 {
            return NFTState::default();
        }

        let data_ptr = allocate(result as usize);
        get_state(key.as_ptr(), key.len());
        let data = std::slice::from_raw_parts(data_ptr, result as usize);
        serde_json::from_slice(data).unwrap_or_default()
    }
}

fn save_state(state: &NFTState) {
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

fn emit_transfer_event(from: &str, to: &str, token_id: u64) {
    let event = TransferEvent {
        from: from.to_string(),
        to: to.to_string(),
        token_id,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn emit_approval_event(owner: &str, approved: &str, token_id: u64) {
    let event = ApprovalEvent {
        owner: owner.to_string(),
        approved: approved.to_string(),
        token_id,
    };
    let serialized = serde_json::to_vec(&event).unwrap();
    unsafe {
        emit_event(serialized.as_ptr(), serialized.len());
    }
}

fn emit_approval_for_all_event(owner: &str, operator: &str, approved: bool) {
    let event = ApprovalForAllEvent {
        owner: owner.to_string(),
        operator: operator.to_string(),
        approved,
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

// ERC721 Implementation
#[no_mangle]
pub extern "C" fn initialize() {
    let mut state = NFTState::default();
    state.name = "Example NFT".to_string();
    state.symbol = "ENFT".to_string();
    state.next_token_id = 1;
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
pub extern "C" fn balance_of(owner_ptr: *const u8, owner_len: usize) -> u64 {
    let owner = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(owner_ptr, owner_len)).to_string()
    };
    let state = load_state();
    *state.balances.get(&owner).unwrap_or(&0)
}

#[no_mangle]
pub extern "C" fn owner_of(token_id: u64) -> *const u8 {
    let state = load_state();
    if let Some(owner) = state.owners.get(&token_id) {
        let owner_bytes = owner.as_bytes();
        let ptr = allocate(owner_bytes.len());
        unsafe {
            std::ptr::copy_nonoverlapping(owner_bytes.as_ptr(), ptr, owner_bytes.len());
        }
        ptr
    } else {
        std::ptr::null()
    }
}

#[no_mangle]
pub extern "C" fn token_uri(token_id: u64) -> *const u8 {
    let state = load_state();
    if let Some(uri) = state.token_uris.get(&token_id) {
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
pub extern "C" fn get_approved(token_id: u64) -> *const u8 {
    let state = load_state();
    if let Some(approved) = state.token_approvals.get(&token_id) {
        let approved_bytes = approved.as_bytes();
        let ptr = allocate(approved_bytes.len());
        unsafe {
            std::ptr::copy_nonoverlapping(approved_bytes.as_ptr(), ptr, approved_bytes.len());
        }
        ptr
    } else {
        std::ptr::null()
    }
}

#[no_mangle]
pub extern "C" fn is_approved_for_all(owner_ptr: *const u8, owner_len: usize, operator_ptr: *const u8, operator_len: usize) -> bool {
    let owner = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(owner_ptr, owner_len)).to_string()
    };
    let operator = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(operator_ptr, operator_len)).to_string()
    };
    let state = load_state();
    state.operator_approvals.get(&owner)
        .and_then(|operators| operators.get(&operator))
        .unwrap_or(&false)
        .clone()
}

#[no_mangle]
pub extern "C" fn mint(to_ptr: *const u8, to_len: usize, uri_ptr: *const u8, uri_len: usize) -> u64 {
    let caller = get_caller_address();
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };
    let uri = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(uri_ptr, uri_len)).to_string()
    };

    let mut state = load_state();
    let token_id = state.next_token_id;
    state.next_token_id += 1;

    // Set ownership
    state.owners.insert(token_id, to.clone());
    state.token_uris.insert(token_id, uri);

    // Update balance
    let current_balance = state.balances.get(&to).unwrap_or(&0);
    state.balances.insert(to.clone(), current_balance + 1);

    save_state(&state);
    emit_transfer_event("", &to, token_id);

    token_id
}

#[no_mangle]
pub extern "C" fn burn(token_id: u64) {
    let caller = get_caller_address();
    let mut state = load_state();

    // Check if token exists
    let owner = match state.owners.get(&token_id) {
        Some(owner) => owner.clone(),
        None => abort_execution("Token does not exist"),
    };

    // Check permission
    if caller != owner && !is_approved_or_owner(&caller, token_id, &state) {
        abort_execution("Unauthorized burn");
    }

    // Remove token
    state.owners.remove(&token_id);
    state.token_uris.remove(&token_id);
    state.token_approvals.remove(&token_id);

    // Update balance
    let current_balance = state.balances.get(&owner).unwrap_or(&0);
    if *current_balance > 0 {
        state.balances.insert(owner.clone(), current_balance - 1);
    }

    save_state(&state);
    emit_transfer_event(&owner, "", token_id);
}

#[no_mangle]
pub extern "C" fn transfer_from(from_ptr: *const u8, from_len: usize, to_ptr: *const u8, to_len: usize, token_id: u64) {
    let caller = get_caller_address();
    let from = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(from_ptr, from_len)).to_string()
    };
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    let mut state = load_state();

    // Check if token exists and from owns it
    let current_owner = match state.owners.get(&token_id) {
        Some(owner) => owner.clone(),
        None => abort_execution("Token does not exist"),
    };

    if current_owner != from {
        abort_execution("From is not owner");
    }

    // Check permission
    if caller != from && !is_approved_or_owner(&caller, token_id, &state) {
        abort_execution("Unauthorized transfer");
    }

    // Clear approval
    state.token_approvals.remove(&token_id);

    // Update ownership
    state.owners.insert(token_id, to.clone());

    // Update balances
    let from_balance = state.balances.get(&from).unwrap_or(&0);
    if *from_balance > 0 {
        state.balances.insert(from.clone(), from_balance - 1);
    }

    let to_balance = state.balances.get(&to).unwrap_or(&0);
    state.balances.insert(to.clone(), to_balance + 1);

    save_state(&state);
    emit_transfer_event(&from, &to, token_id);
}

#[no_mangle]
pub extern "C" fn approve(to_ptr: *const u8, to_len: usize, token_id: u64) {
    let caller = get_caller_address();
    let to = unsafe {
        String::from_utf8_lossy(std::slice::from_raw_parts(to_ptr, to_len)).to_string()
    };

    let mut state = load_state();

    // Check if token exists
    let owner = match state.owners.get(&token_id) {
        Some(owner) => owner.clone(),
        None => abort_execution("Token does not exist"),
    };

    // Check permission
    if caller != owner && !is_approved_for_all_internal(&owner, &caller, &state) {
        abort_execution("Unauthorized approve");
    }

    // Set approval
    state.token_approvals.insert(token_id, to.clone());

    save_state(&state);
    emit_approval_event(&owner, &to, token_id);
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

// Helper functions
fn is_approved_or_owner(spender: &str, token_id: u64, state: &NFTState) -> bool {
    let owner = match state.owners.get(&token_id) {
        Some(owner) => owner,
        None => return false,
    };

    if spender == owner {
        return true;
    }

    if let Some(approved) = state.token_approvals.get(&token_id) {
        if spender == approved {
            return true;
        }
    }

    is_approved_for_all_internal(owner, spender, state)
}

fn is_approved_for_all_internal(owner: &str, operator: &str, state: &NFTState) -> bool {
    state.operator_approvals.get(owner)
        .and_then(|operators| operators.get(operator))
        .unwrap_or(&false)
        .clone()
}