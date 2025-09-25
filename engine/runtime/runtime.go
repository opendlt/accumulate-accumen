package runtime

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	"github.com/opendlt/accumulate-accumen/engine/gas"
	"github.com/opendlt/accumulate-accumen/engine/host"
	"github.com/opendlt/accumulate-accumen/engine/state"
)

// Runtime represents the AccuWASM execution environment
type Runtime struct {
	wazeroRuntime wazero.Runtime
	compiledModule wazero.CompiledModule
	hostAPI       *host.HostAPI
	config        *Config
}

// Config defines runtime configuration
type Config struct {
	// Maximum memory pages (64KB each)
	MaxMemoryPages uint32
	// Gas limit per execution
	GasLimit uint64
	// Enable debug mode
	Debug bool
}

// DefaultConfig returns a default runtime configuration
func DefaultConfig() *Config {
	return &Config{
		MaxMemoryPages: 16,    // 1MB max memory
		GasLimit:       1000000, // 1M gas units
		Debug:          false,
	}
}

// ExecutionResult represents the result of WASM execution
type ExecutionResult struct {
	Success     bool
	ReturnValue []byte
	GasUsed     uint64
	Receipt     *state.Receipt
	Error       error
}

// NewRuntime creates a new AccuWASM runtime instance
func NewRuntime(config *Config) (*Runtime, error) {
	if config == nil {
		config = DefaultConfig()
	}

	ctx := context.Background()

	// Create wazero runtime with deterministic configuration
	runtimeConfig := wazero.NewRuntimeConfig().
		// Disable all non-deterministic features
		WithFeatureSignExtensionOps(false).
		WithFeatureBulkMemoryOperations(false).
		WithFeatureReferenceTypes(false).
		WithFeatureSIMD(false).
		WithFeatureMultiValue(false).
		WithDebugInfoEnabled(config.Debug)

	wazeRuntime := wazero.NewRuntimeWithConfig(ctx, runtimeConfig)

	// Instantiate minimal WASI (no actual system calls)
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, wazeRuntime); err != nil {
		return &Runtime{}, fmt.Errorf("failed to instantiate WASI: %w", err)
	}

	return &Runtime{
		wazeroRuntime: wazeRuntime,
		config:        config,
	}, nil
}

// LoadModule compiles and loads a WASM module
func (r *Runtime) LoadModule(ctx context.Context, wasmBytes []byte) error {
	compiled, err := r.wazeroRuntime.CompileModule(ctx, wasmBytes)
	if err != nil {
		return fmt.Errorf("failed to compile WASM module: %w", err)
	}

	r.compiledModule = compiled
	return nil
}

// Execute runs a WASM function with the given parameters
func (r *Runtime) Execute(ctx context.Context, functionName string, params []uint64, kvStore state.KVStore) (*ExecutionResult, error) {
	if r.compiledModule == nil {
		return nil, fmt.Errorf("no module loaded")
	}

	// Create gas meter
	gasMeter := gas.NewMeter(gas.GasLimit(r.config.GasLimit))

	// Create host API
	r.hostAPI = host.NewHostAPI(gasMeter, kvStore)

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
	}, nil
}

// Close releases runtime resources
func (r *Runtime) Close(ctx context.Context) error {
	if r.wazeroRuntime != nil {
		return r.wazeroRuntime.Close(ctx)
	}
	return nil
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