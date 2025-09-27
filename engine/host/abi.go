package host

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/tetratelabs/wazero/api"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
)

// HostAPI defines the host functions available to WASM modules
type HostAPI struct {
	gasMeter GasMeter
	kvStore  KVStore
	querier  *l0api.Querier
	schedule *pricing.Schedule
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
func NewHostAPI(gasMeter GasMeter, kvStore KVStore, querier *l0api.Querier, schedule *pricing.Schedule) *HostAPI {
	return &HostAPI{
		gasMeter: gasMeter,
		kvStore:  kvStore,
		querier:  querier,
		schedule: schedule,
	}
}

// ExportHostFunctions exports host functions to the WASM runtime
func (h *HostAPI) ExportHostFunctions() map[string]interface{} {
	return map[string]interface{}{
		"accuwasm_get":         h.get,
		"accuwasm_set":         h.set,
		"accuwasm_delete":      h.delete,
		"accuwasm_log":         h.log,
		"l0_read_account_json": h.l0ReadAccountJSON,
		"l0_get_balance":       h.l0GetBalance,
		"l0_read_data_entry":   h.l0ReadDataEntry,
		"l0_credits_quote":     h.l0CreditsQuote,
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

// l0ReadAccountJSON reads account data from L0 and returns JSON bytes
func (h *HostAPI) l0ReadAccountJSON(ctx context.Context, m api.Module, urlPtr, urlLen, outputPtr, outputLenPtr uint32) uint32 {
	if h.querier == nil {
		return 1 // No querier available
	}

	// Read URL from memory
	urlBytes, ok := m.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 1
	}

	// Parse URL
	accountURL, err := accutil.ParseURL(string(urlBytes))
	if err != nil {
		return 1
	}

	// Query account from L0
	accountRecord, err := h.querier.QueryAccount(ctx, accountURL)
	if err != nil {
		return 1
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(accountRecord.Account)
	if err != nil {
		return 1
	}

	// Charge gas based on bytes read
	if h.schedule != nil {
		gasPerByte := h.schedule.PerByte.Read
		gasCost := uint64(len(jsonBytes)) * gasPerByte
		if err := h.gasMeter.ConsumeGas(gasCost); err != nil {
			return 1
		}
	} else {
		// Fallback gas cost
		if err := h.gasMeter.ConsumeGas(uint64(len(jsonBytes))); err != nil {
			return 1
		}
	}

	// Write length to output length pointer
	if !m.Memory().WriteUint32Le(outputLenPtr, uint32(len(jsonBytes))) {
		return 1
	}

	// Write JSON data to output buffer
	if !m.Memory().Write(outputPtr, jsonBytes) {
		return 1
	}

	return 0 // Success
}

// l0GetBalance gets the balance of a token account and returns as u128 decimal string
func (h *HostAPI) l0GetBalance(ctx context.Context, m api.Module, urlPtr, urlLen, outputPtr, outputLenPtr uint32) uint32 {
	if h.querier == nil {
		return 1
	}

	// Read URL from memory
	urlBytes, ok := m.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 1
	}

	// Parse URL
	tokenURL, err := accutil.ParseURL(string(urlBytes))
	if err != nil {
		return 1
	}

	// Query token account from L0
	accountRecord, err := h.querier.QueryAccount(ctx, tokenURL)
	if err != nil {
		return 1
	}

	var balanceStr string
	switch acc := accountRecord.Account.(type) {
	case *protocol.TokenAccount:
		balanceStr = acc.Balance.String()
	case *protocol.LiteTokenAccount:
		balanceStr = acc.Balance.String()
	default:
		return 1 // Not a token account
	}

	balanceBytes := []byte(balanceStr)

	// Charge gas for read operation
	if h.schedule != nil {
		gasPerByte := h.schedule.PerByte.Read
		gasCost := uint64(len(balanceBytes)) * gasPerByte
		if err := h.gasMeter.ConsumeGas(gasCost); err != nil {
			return 1
		}
	} else {
		if err := h.gasMeter.ConsumeGas(100); err != nil {
			return 1
		}
	}

	// Write length to output length pointer
	if !m.Memory().WriteUint32Le(outputLenPtr, uint32(len(balanceBytes))) {
		return 1
	}

	// Write balance string to output buffer
	if !m.Memory().Write(outputPtr, balanceBytes) {
		return 1
	}

	return 0 // Success
}

// l0ReadDataEntry reads a data entry from an L0 data account
func (h *HostAPI) l0ReadDataEntry(ctx context.Context, m api.Module, urlPtr, urlLen, keyPtr, keyLen, outputPtr, outputLenPtr uint32) uint32 {
	if h.querier == nil {
		return 1
	}

	// Read URL from memory
	urlBytes, ok := m.Memory().Read(urlPtr, urlLen)
	if !ok {
		return 1
	}

	// Read key from memory
	keyBytes, ok := m.Memory().Read(keyPtr, keyLen)
	if !ok {
		return 1
	}

	// Parse URL
	dataURL, err := accutil.ParseURL(string(urlBytes))
	if err != nil {
		return 1
	}

	// Query data entry from L0
	entryRecord, err := h.querier.QueryDataEntry(ctx, dataURL, string(keyBytes))
	if err != nil {
		return 1 // Entry not found or other error
	}

	var dataBytes []byte
	if entryRecord != nil && entryRecord.Entry != nil {
		dataBytes = entryRecord.Entry.GetData()
	}

	// Charge gas based on bytes read
	if h.schedule != nil {
		gasPerByte := h.schedule.PerByte.Read
		gasCost := uint64(len(dataBytes)) * gasPerByte
		if err := h.gasMeter.ConsumeGas(gasCost); err != nil {
			return 1
		}
	} else {
		if err := h.gasMeter.ConsumeGas(uint64(len(dataBytes))); err != nil {
			return 1
		}
	}

	// Write length to output length pointer
	if !m.Memory().WriteUint32Le(outputLenPtr, uint32(len(dataBytes))) {
		return 1
	}

	// Write data to output buffer
	if !m.Memory().Write(outputPtr, dataBytes) {
		return 1
	}

	return 0 // Success
}

// l0CreditsQuote calculates credits required for gas and ACME cost
func (h *HostAPI) l0CreditsQuote(ctx context.Context, m api.Module, gas uint64, creditsPtr, acmePtr, acmeLenPtr uint32) uint32 {
	if h.schedule == nil {
		return 1 // No pricing schedule available
	}

	// Base gas charge for this operation
	if err := h.gasMeter.ConsumeGas(50); err != nil {
		return 1
	}

	// Calculate credits using the pricing schedule
	credits := h.schedule.CalculateCredits(gas)

	// Calculate ACME required (simplified conversion)
	acmeRequired := float64(credits) / h.schedule.GCR
	acmeStr := fmt.Sprintf("%.8f", acmeRequired)
	acmeBytes := []byte(acmeStr)

	// Write credits as u64
	if !m.Memory().WriteUint64Le(creditsPtr, credits) {
		return 1
	}

	// Write ACME length
	if !m.Memory().WriteUint32Le(acmeLenPtr, uint32(len(acmeBytes))) {
		return 1
	}

	// Write ACME decimal string
	if !m.Memory().Write(acmePtr, acmeBytes) {
		return 1
	}

	return 0 // Success
}
