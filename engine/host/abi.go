package host

import (
	"context"

	"github.com/tetratelabs/wazero/api"
)

// HostAPI defines the host functions available to WASM modules
type HostAPI struct {
	gasMeter GasMeter
	kvStore  KVStore
}

// GasMeter interface for gas tracking
type GasMeter interface {
	ConsumeGas(amount uint64) error
	GasRemaining() uint64
}

// KVStore interface for state access
type KVStore interface {
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
}

// NewHostAPI creates a new host API instance
func NewHostAPI(gasMeter GasMeter, kvStore KVStore) *HostAPI {
	return &HostAPI{
		gasMeter: gasMeter,
		kvStore:  kvStore,
	}
}

// ExportHostFunctions exports host functions to the WASM runtime
func (h *HostAPI) ExportHostFunctions() map[string]interface{} {
	return map[string]interface{}{
		"accuwasm_get":    h.get,
		"accuwasm_set":    h.set,
		"accuwasm_delete": h.delete,
		"accuwasm_log":    h.log,
	}
}

// get retrieves a value from the key-value store
func (h *HostAPI) get(ctx context.Context, m api.Module, keyPtr, keyLen, valuePtr uint32) uint32 {
	if err := h.gasMeter.ConsumeGas(100); err != nil {
		return 1 // error code
	}

	key, ok := m.Memory().Read(keyPtr, keyLen)
	if !ok {
		return 1
	}

	value, err := h.kvStore.Get(key)
	if err != nil {
		return 1
	}

	if len(value) == 0 {
		return 0 // key not found
	}

	if !m.Memory().Write(valuePtr, value) {
		return 1
	}

	return 0 // success
}

// set stores a value in the key-value store
func (h *HostAPI) set(ctx context.Context, m api.Module, keyPtr, keyLen, valuePtr, valueLen uint32) uint32 {
	if err := h.gasMeter.ConsumeGas(200); err != nil {
		return 1
	}

	key, ok := m.Memory().Read(keyPtr, keyLen)
	if !ok {
		return 1
	}

	value, ok := m.Memory().Read(valuePtr, valueLen)
	if !ok {
		return 1
	}

	if err := h.kvStore.Set(key, value); err != nil {
		return 1
	}

	return 0
}

// delete removes a key from the key-value store
func (h *HostAPI) delete(ctx context.Context, m api.Module, keyPtr, keyLen uint32) uint32 {
	if err := h.gasMeter.ConsumeGas(150); err != nil {
		return 1
	}

	key, ok := m.Memory().Read(keyPtr, keyLen)
	if !ok {
		return 1
	}

	if err := h.kvStore.Delete(key); err != nil {
		return 1
	}

	return 0
}

// log outputs a log message (for debugging)
func (h *HostAPI) log(ctx context.Context, m api.Module, msgPtr, msgLen uint32) uint32 {
	if err := h.gasMeter.ConsumeGas(50); err != nil {
		return 1
	}

	msg, ok := m.Memory().Read(msgPtr, msgLen)
	if !ok {
		return 1
	}

	// In production, this would go to a proper logger
	_ = string(msg)

	return 0
}