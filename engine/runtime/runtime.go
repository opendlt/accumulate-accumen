package runtime

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/engine/gas"
	"github.com/opendlt/accumulate-accumen/engine/host"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/accutil"
)

// ExecMode defines the execution mode for WASM contracts
type ExecMode int

const (
	// Execute mode allows full contract execution with state mutations and L0 operations
	Execute ExecMode = iota
	// Query mode is read-only and prevents state mutations and L0 operations
	Query
)

// Runtime represents the AccuWASM execution environment
type Runtime struct {
	wazeroRuntime  wazero.Runtime
	compiledModule wazero.CompiledModule
	hostAPI        *host.API
	config         *RuntimeConfig
	moduleCache    *ModuleCache
}

// RuntimeConfig defines runtime configuration (renamed from Config to avoid conflicts)
type RuntimeConfig struct {
	// Maximum memory pages (64KB each)
	MaxMemoryPages uint32
	// Gas limit per execution
	GasLimit uint64
	// Gas limit for query mode (typically lower than full execution)
	QueryGasLimit uint64
	// Enable debug mode
	Debug bool
	// Cache size for compiled modules
	CacheSize int
}

// Config is an alias for backward compatibility
type Config = RuntimeConfig

// DefaultConfig returns a default runtime configuration
func DefaultConfig() *Config {
	return &Config{
		MaxMemoryPages: 16,      // 1MB max memory
		GasLimit:       1000000, // 1M gas units
		QueryGasLimit:  100000,  // 100K gas units for queries (lower limit)
		Debug:          false,
		CacheSize:      16, // 16 compiled modules in cache
	}
}

// StagedOp represents a staged L0 operation
type StagedOp struct {
	Type    string   // "write_data", "send_tokens", "update_auth"
	Account *url.URL // Account URL
	From    *url.URL // For send_tokens
	To      *url.URL // For send_tokens
	Amount  uint64   // For send_tokens
	Data    []byte   // For write_data and update_auth
}

// Event represents an emitted event
type Event struct {
	Type string
	Data []byte
}

// ExecutionContext holds context for WASM execution
type ExecutionContext struct {
	stagedOps []*StagedOp
	events    []*Event
}

// NewExecutionContext creates a new execution context
func NewExecutionContext() *ExecutionContext {
	return &ExecutionContext{
		stagedOps: make([]*StagedOp, 0),
		events:    make([]*Event, 0),
	}
}

// GetStagedOps returns the staged operations
func (ec *ExecutionContext) GetStagedOps() []*StagedOp {
	return ec.stagedOps
}

// GetEvents returns the emitted events
func (ec *ExecutionContext) GetEvents() []*Event {
	return ec.events
}

// NewStagedOp creates a new staged operation with proper URL parsing
func NewStagedOp(opType string, accountURL string) (*StagedOp, error) {
	account, err := accutil.ParseURL(accountURL)
	if err != nil {
		return nil, fmt.Errorf("invalid account URL: %w", err)
	}

	return &StagedOp{
		Type:    opType,
		Account: account,
	}, nil
}

// SetFrom sets the From URL for send_tokens operations
func (so *StagedOp) SetFrom(fromURL string) error {
	if fromURL == "" {
		so.From = nil
		return nil
	}

	from, err := accutil.ParseURL(fromURL)
	if err != nil {
		return fmt.Errorf("invalid from URL: %w", err)
	}

	so.From = from
	return nil
}

// SetTo sets the To URL for send_tokens operations
func (so *StagedOp) SetTo(toURL string) error {
	if toURL == "" {
		so.To = nil
		return nil
	}

	to, err := accutil.ParseURL(toURL)
	if err != nil {
		return fmt.Errorf("invalid to URL: %w", err)
	}

	so.To = to
	return nil
}

// GetAccountString returns the account URL as a string
func (so *StagedOp) GetAccountString() string {
	if so.Account == nil {
		return ""
	}
	return accutil.Canonicalize(so.Account)
}

// GetFromString returns the from URL as a string
func (so *StagedOp) GetFromString() string {
	if so.From == nil {
		return ""
	}
	return accutil.Canonicalize(so.From)
}

// GetToString returns the to URL as a string
func (so *StagedOp) GetToString() string {
	if so.To == nil {
		return ""
	}
	return accutil.Canonicalize(so.To)
}

// Validate validates the staged operation
func (so *StagedOp) Validate() error {
	if so.Type == "" {
		return fmt.Errorf("operation type cannot be empty")
	}

	if so.Account == nil {
		return fmt.Errorf("account URL cannot be nil")
	}

	switch so.Type {
	case "write_data":
		// No additional validation needed
	case "send_tokens":
		if so.From == nil {
			return fmt.Errorf("send_tokens operation requires From URL")
		}
		if so.To == nil {
			return fmt.Errorf("send_tokens operation requires To URL")
		}
		if so.Amount == 0 {
			return fmt.Errorf("send_tokens operation requires non-zero amount")
		}
	case "update_auth":
		if len(so.Data) == 0 {
			return fmt.Errorf("update_auth operation requires data")
		}
	default:
		return fmt.Errorf("unknown operation type: %s", so.Type)
	}

	return nil
}

// ExecutionResult represents the result of WASM execution
type ExecutionResult struct {
	Success     bool
	ReturnValue []byte
	GasUsed     uint64
	Receipt     *state.Receipt
	StagedOps   []*StagedOp
	Events      []*Event
	Error       error
}

// NewRuntime creates a new AccuWASM runtime instance
func NewRuntime(config *RuntimeConfig) (*Runtime, error) {
	if config == nil {
		config = DefaultConfig()
	}

	ctx := context.Background()

	// Create wazero runtime with strict deterministic configuration
	runtimeConfig := wazero.NewRuntimeConfig().
		// Disable all non-deterministic features for strict determinism
		WithFeatureSignExtensionOps(false).     // No sign extension ops
		WithFeatureBulkMemoryOperations(false). // No bulk memory operations
		WithFeatureReferenceTypes(false).       // No reference types
		WithFeatureSIMD(false).                 // No SIMD operations
		WithFeatureMultiValue(false).           // No multi-value returns
		WithFeatureMutableGlobals(false).       // No mutable globals
		WithDebugInfoEnabled(config.Debug).
		// Enable strict deterministic execution
		WithCoreFeatures(wazero.CoreFeaturesV1) // Use only WASM 1.0 core features

	wazeroRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Instantiate minimal WASI (no actual system calls)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wazeroRuntime); err != nil {
		wazeroRuntime.Close(ctx)
		return nil, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	// Create module cache
	moduleCache := NewModuleCache(config.CacheSize, wazeroRuntime, config)

	return &Runtime{
		wazeroRuntime: wazeroRuntime,
		config:        config,
		moduleCache:   moduleCache,
	}, nil
}

// LoadModule compiles and loads a WASM module (deprecated - use ExecuteContract instead)
func (r *Runtime) LoadModule(ctx context.Context, wasmBytes []byte) error {
	prepared, _, err := r.moduleCache.Prepare(wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to prepare WASM module: %w", err)
	}

	r.compiledModule = prepared.Module
	return nil
}

// ExecuteContract executes a WASM contract with caching and validation
func (r *Runtime) ExecuteContract(ctx context.Context, wasmBytes []byte, wasmHash []byte, functionName string, params []uint64, kvStore state.KVStore) (*ExecutionResult, error) {
	// Use the unified execution path with Execute mode
	return r.executeContractWithMode(ctx, wasmBytes, wasmHash, functionName, params, kvStore, Execute)
}

// ExecuteContractQuery executes a WASM contract in read-only query mode
func (r *Runtime) ExecuteContractQuery(ctx context.Context, wasmBytes []byte, wasmHash []byte, functionName string, params []uint64, kvStore state.KVStore) (*ExecutionResult, error) {
	// Use the same logic as ExecuteContract but with Query mode restrictions
	return r.executeContractWithMode(ctx, wasmBytes, wasmHash, functionName, params, kvStore, Query)
}

// executeContractWithMode executes a contract with specified execution mode
func (r *Runtime) executeContractWithMode(ctx context.Context, wasmBytes []byte, wasmHash []byte, functionName string, params []uint64, kvStore state.KVStore, mode ExecMode) (*ExecutionResult, error) {
	// Convert hash to [32]byte
	var hash [32]byte
	copy(hash[:], wasmHash)

	// Try to get from cache first
	prepared, found := r.moduleCache.Get(hash)
	if !found {
		// Prepare and cache the module
		var err error
		prepared, hash, err = r.moduleCache.Prepare(wasmBytes)
		if err != nil {
			return &ExecutionResult{
				Success: false,
				Error:   fmt.Errorf("failed to prepare WASM module: %w", err),
			}, nil
		}

		// Store in cache
		r.moduleCache.Put(hash, prepared)
	}

	// Validate module metadata
	if !prepared.Metadata.IsValid {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("module validation failed: %s", prepared.Metadata.ValidationError),
		}, nil
	}

	// Create gas meter with appropriate limit based on mode
	var gasLimit uint64
	if mode == Query {
		gasLimit = r.config.QueryGasLimit
	} else {
		gasLimit = r.config.GasLimit
	}
	gasMeter := gas.NewMeter(gas.GasLimit(gasLimit))

	// Create host API
	r.hostAPI = host.NewAPI(gasMeter, kvStore)

	// Create execution context
	execContext := NewExecutionContext()

	// Register host bindings with execution mode restrictions
	hostModuleBuilder := r.wazeroRuntime.NewHostModuleBuilder("accuwasm_host")
	if err := RegisterHostBindingsWithMode(ctx, hostModuleBuilder, r.hostAPI, gasMeter, execContext, mode); err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to register host bindings: %w", err),
		}, nil
	}

	// Instantiate the host module
	if _, err := hostModuleBuilder.Instantiate(ctx); err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to instantiate host module: %w", err),
		}, nil
	}

	// Configure module with strict deterministic settings
	moduleConfig := wazero.NewModuleConfig().
		WithArgs("accuwasm").
		WithStdin(nil).                                                  // No stdin access
		WithStdout(nil).                                                 // No stdout access
		WithStderr(nil).                                                 // No stderr access
		WithSysWalltime(func() (sec int64, nsec int32) { return 0, 0 }). // Fixed time
		WithSysNanotime(func() int64 { return 0 }).                      // Fixed time
		WithRandSource(nil).                                             // No random source
		// Limit memory strictly
		WithMemoryLimitPages(prepared.Metadata.MaxMemoryPages)

	// Instantiate module with the prepared compiled module
	module, err := r.wazeroRuntime.InstantiateModule(ctx, prepared.Module, moduleConfig)
	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to instantiate module: %w", err),
		}, nil
	}
	defer module.Close(ctx)

	// Get the function to execute
	fn := module.ExportedFunction(functionName)
	if fn == nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("function %s not found in module exports: %v", functionName, prepared.Metadata.Exports),
		}, nil
	}

	// Execute the function with gas metering
	results, err := fn.Call(ctx, params...)

	gasUsed := gasMeter.GasConsumed()

	if err != nil {
		return &ExecutionResult{
			Success: false,
			GasUsed: gasUsed,
			Error:   fmt.Errorf("execution failed: %w", err),
		}, nil
	}

	// Convert results to bytes (simplified)
	var returnValue []byte
	if len(results) > 0 {
		returnValue = []byte(fmt.Sprintf("%d", results[0]))
	}

	// Create execution receipt with module information
	receipt := &state.Receipt{
		Success:      true,
		GasUsed:      gasUsed,
		GasLimit:     gasLimit,
		FunctionName: functionName,
		ReturnValue:  returnValue,
		ModuleHash:   hash[:],
	}

	return &ExecutionResult{
		Success:     true,
		ReturnValue: returnValue,
		GasUsed:     gasUsed,
		Receipt:     receipt,
		StagedOps:   execContext.GetStagedOps(),
		Events:      execContext.GetEvents(),
	}, nil
}

// Execute runs a WASM function with the given parameters
func (r *Runtime) Execute(ctx context.Context, functionName string, params []uint64, kvStore state.KVStore) (*ExecutionResult, error) {
	if r.compiledModule == nil {
		return nil, fmt.Errorf("no module loaded")
	}

	// Create gas meter
	gasMeter := gas.NewMeter(gas.GasLimit(r.config.GasLimit))

	// Create host API
	r.hostAPI = host.NewAPI(gasMeter, kvStore)

	// Create execution context
	execContext := NewExecutionContext()

	// Register host bindings
	hostModuleBuilder := r.wazeroRuntime.NewHostModuleBuilder("accuwasm_host")
	if err := RegisterHostBindings(ctx, hostModuleBuilder, r.hostAPI, gasMeter, execContext); err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to register host bindings: %w", err),
		}, nil
	}

	// Instantiate the host module
	if _, err := hostModuleBuilder.Instantiate(ctx); err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to instantiate host module: %w", err),
		}, nil
	}

	// Configure module with host functions and gas metering
	moduleConfig := wazero.NewModuleConfig().
		WithArgs("accuwasm").
		WithStdin(nil).
		WithStdout(nil).
		WithStderr(nil).
		// Limit memory
		WithMemoryLimitPages(r.config.MaxMemoryPages)

	// Instantiate module with host functions
	module, err := r.wazeroRuntime.InstantiateModule(ctx, r.compiledModule, moduleConfig)
	if err != nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("failed to instantiate module: %w", err),
		}, nil
	}
	defer module.Close(ctx)

	// Get the function to execute
	fn := module.ExportedFunction(functionName)
	if fn == nil {
		return &ExecutionResult{
			Success: false,
			Error:   fmt.Errorf("function %s not found", functionName),
		}, nil
	}

	// Execute the function
	results, err := fn.Call(ctx, params...)

	gasUsed := gasMeter.GasConsumed()

	if err != nil {
		return &ExecutionResult{
			Success: false,
			GasUsed: gasUsed,
			Error:   fmt.Errorf("execution failed: %w", err),
		}, nil
	}

	// Convert results to bytes (simplified)
	var returnValue []byte
	if len(results) > 0 {
		returnValue = []byte(fmt.Sprintf("%d", results[0]))
	}

	// Create execution receipt
	receipt := &state.Receipt{
		Success:      true,
		GasUsed:      gasUsed,
		GasLimit:     r.config.GasLimit,
		FunctionName: functionName,
		ReturnValue:  returnValue,
	}

	return &ExecutionResult{
		Success:     true,
		ReturnValue: returnValue,
		GasUsed:     gasUsed,
		Receipt:     receipt,
		StagedOps:   execContext.GetStagedOps(),
		Events:      execContext.GetEvents(),
	}, nil
}

// Close releases runtime resources
func (r *Runtime) Close(ctx context.Context) error {
	// Clear module cache first
	if r.moduleCache != nil {
		r.moduleCache.Clear()
	}

	// Close wazero runtime
	if r.wazeroRuntime != nil {
		return r.wazeroRuntime.Close(ctx)
	}
	return nil
}

// GetCacheStats returns module cache statistics
func (r *Runtime) GetCacheStats() CacheStats {
	if r.moduleCache != nil {
		return r.moduleCache.GetStats()
	}
	return CacheStats{}
}

// GetMemoryUsage returns current memory usage statistics
func (r *Runtime) GetMemoryUsage() (uint32, uint32) {
	if r.config == nil {
		return 0, 0
	}
	return 0, r.config.MaxMemoryPages // current, max (simplified)
}

// ValidateModule performs basic validation on WASM bytecode
func (r *Runtime) ValidateModule(ctx context.Context, wasmBytes []byte) error {
	_, err := r.wazeroRuntime.CompileModule(ctx, wasmBytes)
	return err
}
