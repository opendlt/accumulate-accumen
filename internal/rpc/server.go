package rpc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/pricing"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/engine/state/contracts"
	"github.com/opendlt/accumulate-accumen/follower/indexer"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/types/l1"
	"github.com/opendlt/accumulate-accumen/types/proto/accumen"
	"google.golang.org/protobuf/proto"
)

// Server provides a JSON-RPC interface for Accumen
type Server struct {
	mu            sync.RWMutex
	server        *http.Server
	sequencer     *sequencer.Sequencer
	kvStore       state.KVStore
	contractStore *contracts.Store
	mempool       *sequencer.PersistentMempool
	indexer       *indexer.Indexer
	readOnly      bool
	running       bool
	stats         *ServerStats
}

// Dependencies holds the required dependencies for the RPC server
type Dependencies struct {
	Sequencer     *sequencer.Sequencer
	KVStore       state.KVStore
	ContractStore *contracts.Store
	Mempool       *sequencer.PersistentMempool
}

// ReadOnlyDependencies holds dependencies for read-only (follower) mode
type ReadOnlyDependencies struct {
	KVStore state.KVStore
	Indexer *indexer.Indexer
}

// Request represents a JSON-RPC request
type Request struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params,omitempty"`
}

// Response represents a JSON-RPC response
type Response struct {
	ID     interface{} `json:"id"`
	Result interface{} `json:"result,omitempty"`
	Error  *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC error
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// StatusResult represents the result of accumen.status()
type StatusResult struct {
	ChainID        string    `json:"chainId"`
	Height         uint64    `json:"height"`
	LastAnchorTime *string   `json:"lastAnchorTime"`
	BlockTime      string    `json:"blockTime"`
	Running        bool      `json:"running"`
	TxsProcessed   uint64    `json:"txsProcessed"`
	Uptime         string    `json:"uptime"`
}

// SubmitTxParams represents parameters for accumen.submitTx()
// Supports both structured JSON format and raw CBOR data
type SubmitTxParams struct {
	// Structured format
	Contract  string                 `json:"contract,omitempty"`
	Entry     string                 `json:"entry,omitempty"`
	Args      map[string]interface{} `json:"args,omitempty"`
	Nonce     []byte                 `json:"nonce,omitempty"`
	Timestamp int64                  `json:"timestamp,omitempty"`

	// Raw CBOR format (base64 encoded)
	RawCBOR   string `json:"rawCBOR,omitempty"`

	// Legacy fields for backward compatibility
	From     string `json:"from,omitempty"`
	GasLimit uint64 `json:"gasLimit,omitempty"`
}

// SubmitTxResult represents the result of accumen.submitTx()
type SubmitTxResult struct {
	TxHash string `json:"txHash"` // L1 transaction hash (32-byte SHA256)
}

// QueryParams represents parameters for accumen.query()
// Supports both state key queries and contract function calls
type QueryParams struct {
	Contract string                 `json:"contract"`
	Key      string                 `json:"key,omitempty"`      // For state queries
	Entry    string                 `json:"entry,omitempty"`    // For function calls
	Args     map[string]interface{} `json:"args,omitempty"`     // For function calls
}

// QueryResult represents the result of accumen.query()
type QueryResult struct {
	Value    *string `json:"value"`
	Exists   bool    `json:"exists"`
	Type     string  `json:"type,omitempty"`
	Events   []Event `json:"events,omitempty"`   // For contract function calls
	GasUsed  uint64  `json:"gasUsed,omitempty"`  // For contract function calls
}

// Event represents a contract event for query results
type Event struct {
	Type string `json:"type"`
	Data string `json:"data"` // hex-encoded event data
}

// DeployContractParams represents parameters for accumen.deployContract()
type DeployContractParams struct {
	Addr   string `json:"addr"`
	WasmB64 string `json:"wasm_b64"`
}

// DeployContractResult represents the result of accumen.deployContract()
type DeployContractResult struct {
	WasmHash string `json:"wasm_hash"`
}

// UpgradeContractParams represents parameters for accumen.upgradeContract()
type UpgradeContractParams struct {
	Addr        string                 `json:"addr"`
	WasmB64     string                 `json:"wasm_b64"`
	MigrateEntry *string               `json:"migrate_entry,omitempty"`
	MigrateArgs  map[string]interface{} `json:"migrate_args,omitempty"`
}

// UpgradeContractResult represents the result of accumen.upgradeContract()
type UpgradeContractResult struct {
	Version  int    `json:"version"`
	WasmHash string `json:"wasm_hash"`
}

// ActivateVersionParams represents parameters for accumen.activateVersion()
type ActivateVersionParams struct {
	Addr    string `json:"addr"`
	Version int    `json:"version"`
}

// ActivateVersionResult represents the result of accumen.activateVersion()
type ActivateVersionResult struct {
	Success bool `json:"success"`
}

// SimulateParams represents parameters for accumen.simulate()
type SimulateParams struct {
	Contract string                 `json:"contract"`
	Entry    string                 `json:"entry"`
	Args     map[string]interface{} `json:"args,omitempty"`
}

// SimulateResult represents the result of accumen.simulate()
type SimulateResult struct {
	GasUsed           uint64  `json:"gasUsed"`
	Events            []Event `json:"events"`
	L0Credits         uint64  `json:"l0Credits"`
	ACME              string  `json:"acme"`
	EstimatedDNBytes  uint64  `json:"estimatedDNBytes"`
	Success           bool    `json:"success"`
	Error             string  `json:"error,omitempty"`
}

// ServerStats contains RPC server statistics
type ServerStats struct {
	mu           sync.RWMutex
	RequestCount uint64        `json:"request_count"`
	ErrorCount   uint64        `json:"error_count"`
	Uptime       time.Duration `json:"uptime"`
	StartTime    time.Time     `json:"start_time"`
}

// NewServer creates a new RPC server
func NewServer(deps *Dependencies) *Server {
	return &Server{
		sequencer:     deps.Sequencer,
		kvStore:       deps.KVStore,
		contractStore: deps.ContractStore,
		mempool:       deps.Mempool,
		readOnly:      false,
		running:       false,
		stats:         &ServerStats{StartTime: time.Now()},
	}
}

// NewReadOnlyServer creates a new read-only RPC server for followers
func NewReadOnlyServer(deps *ReadOnlyDependencies) *Server {
	return &Server{
		kvStore:  deps.KVStore,
		indexer:  deps.Indexer,
		readOnly: true,
		running:  false,
		stats:    &ServerStats{StartTime: time.Now()},
	}
}

// Start starts the RPC server on the specified address
func (s *Server) Start(addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("RPC server is already running")
	}

	// Create HTTP server
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)
	mux.HandleFunc("/health", s.handleHealth)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	s.running = true

	// Start server in background
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("RPC server error: %v\n", err)
		}
	}()

	fmt.Printf("RPC server started on %s\n", addr)
	return nil
}

// Stop stops the RPC server
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.running = false

	if s.server != nil {
		return s.server.Shutdown(ctx)
	}

	return nil
}

// handleRequest handles JSON-RPC requests
func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Set CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Content-Type", "application/json")

	// Handle preflight OPTIONS request
	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Only accept POST requests for JSON-RPC
	if r.Method != "POST" {
		s.writeError(w, nil, -32600, "Invalid Request")
		return
	}

	// Parse request
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, nil, -32700, "Parse error")
		return
	}

	// Track request
	s.recordRequest()

	// Route method
	var result interface{}
	var err *RPCError

	switch req.Method {
	case "accumen.status":
		result, err = s.handleStatus()
	case "accumen.query":
		result, err = s.handleQuery(req.Params)
	case "accumen.getTx":
		result, err = s.handleGetTx(req.Params)
	case "accumen.submitTx":
		if s.readOnly {
			err = &RPCError{Code: -32601, Message: "Method not available in read-only mode"}
		} else {
			result, err = s.handleSubmitTx(req.Params)
		}
	case "accumen.deployContract":
		if s.readOnly {
			err = &RPCError{Code: -32601, Message: "Method not available in read-only mode"}
		} else {
			result, err = s.handleDeployContract(req.Params)
		}
	case "accumen.upgradeContract":
		if s.readOnly {
			err = &RPCError{Code: -32601, Message: "Method not available in read-only mode"}
		} else {
			result, err = s.handleUpgradeContract(req.Params)
		}
	case "accumen.activateVersion":
		if s.readOnly {
			err = &RPCError{Code: -32601, Message: "Method not available in read-only mode"}
		} else {
			result, err = s.handleActivateVersion(req.Params)
		}
	case "accumen.getL1Receipt":
		result, err = s.handleGetL1Receipt(req.Params)
	case "accumen.getTxReceipt":
		result, err = s.handleGetTxReceipt(req.Params)
	case "accumen.getBlockHeader":
		result, err = s.handleGetBlockHeader(req.Params)
	case "accumen.simulate":
		if s.readOnly {
			err = &RPCError{Code: -32601, Message: "Method not available in read-only mode"}
		} else {
			result, err = s.handleSimulate(req.Params)
		}
	default:
		err = &RPCError{Code: -32601, Message: "Method not found"}
	}

	// Track errors
	if err != nil {
		s.recordError()
	}

	// Write response
	if err != nil {
		s.writeError(w, req.ID, err.Code, err.Message)
	} else {
		s.writeResult(w, req.ID, result)
	}
}

// handleHealth handles health check requests
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var running bool
	if s.readOnly {
		running = s.indexer != nil
	} else {
		running = s.sequencer != nil && s.sequencer.IsRunning()
	}

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"running":   running,
		"mode":      s.getMode(),
	}

	json.NewEncoder(w).Encode(health)
}

// handleStatus handles accumen.status() method
func (s *Server) handleStatus() (interface{}, *RPCError) {
	if s.readOnly {
		// Follower mode status
		if s.indexer == nil {
			return nil, &RPCError{Code: -32603, Message: "Indexer not available"}
		}

		indexerStats := s.indexer.GetStats()
		result := &StatusResult{
			ChainID:        "accumen-devnet-1", // TODO: get from config
			Height:         indexerStats.CurrentHeight,
			LastAnchorTime: nil, // Not applicable for followers
			BlockTime:      "5s", // TODO: get from config
			Running:        indexerStats.Running,
			TxsProcessed:   indexerStats.EntriesProcessed,
			Uptime:         time.Since(indexerStats.StartTime).String(),
		}
		return result, nil
	}

	// Sequencer mode status
	if !s.sequencer.IsRunning() {
		return nil, &RPCError{Code: -32603, Message: "Sequencer not running"}
	}

	stats := s.sequencer.GetStats()

	var lastAnchorTime *string
	lastAnchorHeight := s.sequencer.GetLastAnchorHeight()
	if lastAnchorHeight > 0 {
		// Estimate last anchor time based on current time and block interval
		// In a real implementation, this would be stored properly
		estimatedTime := time.Now().Add(-time.Duration(stats.BlockHeight-lastAnchorHeight) * time.Second * 5) // Assuming 5s block time
		timeStr := estimatedTime.UTC().Format(time.RFC3339)
		lastAnchorTime = &timeStr
	}

	result := &StatusResult{
		ChainID:        "accumen-devnet-1", // TODO: get from config
		Height:         stats.BlockHeight,
		LastAnchorTime: lastAnchorTime,
		BlockTime:      "5s", // TODO: get from config
		Running:        stats.Running,
		TxsProcessed:   stats.TxsProcessed,
		Uptime:         stats.Uptime.String(),
	}

	return result, nil
}

// handleSubmitTx handles accumen.submitTx() method
func (s *Server) handleSubmitTx(params interface{}) (interface{}, *RPCError) {
	if !s.sequencer.IsRunning() {
		return nil, &RPCError{Code: -32603, Message: "Sequencer not running"}
	}

	if s.mempool == nil {
		return nil, &RPCError{Code: -32603, Message: "Persistent mempool not available"}
	}

	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var submitParams SubmitTxParams
	if err := json.Unmarshal(paramsBytes, &submitParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	var l1Tx l1.Tx

	// Handle raw CBOR format first
	if submitParams.RawCBOR != "" {
		// Decode base64 CBOR data
		cborData, err := base64.StdEncoding.DecodeString(submitParams.RawCBOR)
		if err != nil {
			return nil, &RPCError{Code: -32602, Message: "Invalid base64 CBOR data"}
		}

		// Unmarshal L1 transaction from CBOR
		if err := l1Tx.UnmarshalCBOR(cborData); err != nil {
			return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("Invalid CBOR transaction: %v", err)}
		}
	} else {
		// Handle structured JSON format
		if submitParams.Contract == "" {
			return nil, &RPCError{Code: -32602, Message: "Missing required field: contract"}
		}
		if submitParams.Entry == "" {
			return nil, &RPCError{Code: -32602, Message: "Missing required field: entry"}
		}

		// Generate nonce if not provided
		nonce := submitParams.Nonce
		if len(nonce) == 0 {
			nonce = make([]byte, 16)
			if _, err := rand.Read(nonce); err != nil {
				return nil, &RPCError{Code: -32603, Message: "Failed to generate nonce"}
			}
		}

		// Set timestamp if not provided
		timestamp := submitParams.Timestamp
		if timestamp == 0 {
			timestamp = time.Now().UnixNano()
		}

		// Create L1 transaction
		l1Tx = l1.Tx{
			Contract:  submitParams.Contract,
			Entry:     submitParams.Entry,
			Args:      submitParams.Args,
			Nonce:     nonce,
			Timestamp: timestamp,
		}
	}

	// Validate the L1 transaction
	if err := l1Tx.Validate(); err != nil {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("Invalid transaction: %v", err)}
	}

	// Check namespace permissions if sequencer is available
	if s.sequencer != nil {
		engine := s.sequencer.GetExecutionEngine()
		if engine != nil {
			ctx := context.Background()
			if err := engine.ValidateNamespacePermission(ctx, l1Tx.Contract); err != nil {
				return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("Namespace validation failed: %v", err)}
			}
		}
	}

	// Add to persistent mempool
	hash, err := s.mempool.Add(l1Tx)
	if err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Failed to add transaction to mempool: %v", err)}
	}

	// TODO: Submit to sequencer for processing
	// For now, we just add to mempool and let the sequencer poll it

	result := &SubmitTxResult{
		TxHash: hex.EncodeToString(hash[:]),
	}

	return result, nil
}

// handleQuery handles accumen.query() method
func (s *Server) handleQuery(params interface{}) (interface{}, *RPCError) {
	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var queryParams QueryParams
	if err := json.Unmarshal(paramsBytes, &queryParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if queryParams.Contract == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: contract"}
	}

	// Either key (for state query) or entry (for function call) must be provided
	if queryParams.Key == "" && queryParams.Entry == "" {
		return nil, &RPCError{Code: -32602, Message: "Either 'key' (for state query) or 'entry' (for function call) must be provided"}
	}

	// Both key and entry cannot be provided simultaneously
	if queryParams.Key != "" && queryParams.Entry != "" {
		return nil, &RPCError{Code: -32602, Message: "Cannot specify both 'key' and 'entry' - use either state query or function call"}
	}

	// Handle function call queries (read-only contract execution)
	if queryParams.Entry != "" {
		// This is a contract function call query
		if s.readOnly {
			return nil, &RPCError{Code: -32601, Message: "Contract function calls not supported in read-only mode"}
		}

		// Get execution engine from sequencer
		engine := s.sequencer.GetExecutionEngine()
		if engine == nil {
			return nil, &RPCError{Code: -32603, Message: "Execution engine not available"}
		}

		// Execute contract in query mode
		returnValue, events, gasUsed, err := engine.ExecuteQuery(queryParams.Contract, queryParams.Entry, queryParams.Args)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Query execution failed: %v", err)}
		}

		// Convert events to RPC format
		var rpcEvents []Event
		for _, event := range events {
			rpcEvents = append(rpcEvents, Event{
				Type: event.Type,
				Data: hex.EncodeToString(event.Data),
			})
		}

		// Convert return value to hex string
		var valueStr *string
		if len(returnValue) > 0 {
			hexValue := hex.EncodeToString(returnValue)
			valueStr = &hexValue
		}

		result := &QueryResult{
			Value:   valueStr,
			Exists:  len(returnValue) > 0,
			Type:    "bytes",
			Events:  rpcEvents,
			GasUsed: gasUsed,
		}

		return result, nil
	}

	// Handle state key queries (traditional state lookup)
	var value []byte
	var exists bool

	if s.readOnly && s.indexer != nil {
		// Query via indexer for follower mode
		var queryErr error
		value, queryErr = s.indexer.QueryContractState(queryParams.Contract, queryParams.Key)
		exists = queryErr == nil
	} else {
		// Query from state store (sequencer mode)
		stateKey := fmt.Sprintf("state:%s:%s", queryParams.Contract, queryParams.Key)
		var queryErr error
		value, queryErr = s.kvStore.Get([]byte(stateKey))
		exists = queryErr == nil
	}

	var valueStr *string
	if exists && len(value) > 0 {
		// Convert bytes to hex string
		hexValue := hex.EncodeToString(value)
		valueStr = &hexValue
	}

	result := &QueryResult{
		Value:  valueStr,
		Exists: exists,
		Type:   "bytes",
	}

	return result, nil
}

// GetTxParams represents parameters for accumen.getTx()
type GetTxParams struct {
	TxHash string `json:"txHash"`
}

// GetL1ReceiptParams represents parameters for accumen.getL1Receipt()
type GetL1ReceiptParams struct {
	Hash string `json:"hash"`
}

// GetL1ReceiptResult represents the result of accumen.getL1Receipt()
type GetL1ReceiptResult struct {
	L1Hash      string                    `json:"l1Hash"`
	DNTxID      string                    `json:"dnTxId"` // hex-encoded DN transaction ID
	DNKey       string                    `json:"dnKey,omitempty"`
	Contract    string                    `json:"contract,omitempty"`
	Entry       string                    `json:"entry,omitempty"`
	Metadata    *indexer.MetadataEntry    `json:"metadata,omitempty"`
	AnchorTxIDs []string                  `json:"anchorTxIds,omitempty"` // hex-encoded anchor tx IDs
}

// GetTxReceiptParams represents parameters for accumen.getTxReceipt()
type GetTxReceiptParams struct {
	Hash string `json:"hash"`
}

// GetTxReceiptResult represents the result of accumen.getTxReceipt()
type GetTxReceiptResult struct {
	Height  uint64        `json:"height"`
	Index   int           `json:"index"`
	Events  []ReceiptEvent `json:"events"`
	GasUsed uint64        `json:"gasUsed"`
	Error   string        `json:"error,omitempty"`
	L0TxIDs []string      `json:"l0TxIds"`
}

// ReceiptEvent represents an event in the receipt result
type ReceiptEvent struct {
	Type string `json:"type"`
	Data string `json:"data"` // hex-encoded
}

// GetBlockHeaderParams represents parameters for accumen.getBlockHeader()
type GetBlockHeaderParams struct {
	Height uint64 `json:"height"`
}

// GetBlockHeaderResult represents the result of accumen.getBlockHeader()
type GetBlockHeaderResult struct {
	Height      uint64 `json:"height"`
	PrevHash    string `json:"prevHash"`
	TxsRoot     string `json:"txsRoot"`
	ResultsRoot string `json:"resultsRoot"`
	StateRoot   string `json:"stateRoot"`
	Time        int64  `json:"time"`
}

// handleGetTx handles accumen.getTx() method
func (s *Server) handleGetTx(params interface{}) (interface{}, *RPCError) {
	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var getTxParams GetTxParams
	if err := json.Unmarshal(paramsBytes, &getTxParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if getTxParams.TxHash == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: txHash"}
	}

	if s.readOnly && s.indexer != nil {
		// Query via indexer for follower mode
		entry, err := s.indexer.QueryTransaction(getTxParams.TxHash)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Transaction not found: %v", err)}
		}
		return entry, nil
	} else {
		// Query from KV store (sequencer mode)
		txKey := fmt.Sprintf("tx:%s", getTxParams.TxHash)
		data, err := s.kvStore.Get([]byte(txKey))
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: "Transaction not found"}
		}

		// Return raw transaction data as hex
		return map[string]interface{}{
			"txHash": getTxParams.TxHash,
			"data":   hex.EncodeToString(data),
		}, nil
	}
}

// handleDeployContract handles accumen.deployContract() method
func (s *Server) handleDeployContract(params interface{}) (interface{}, *RPCError) {
	if s.contractStore == nil {
		return nil, &RPCError{Code: -32603, Message: "Contract store not available"}
	}

	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var deployParams DeployContractParams
	if err := json.Unmarshal(paramsBytes, &deployParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if deployParams.Addr == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: addr"}
	}
	if deployParams.WasmB64 == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: wasm_b64"}
	}

	// Validate address format
	if !strings.HasPrefix(deployParams.Addr, "acc://") {
		return nil, &RPCError{Code: -32602, Message: "Invalid address format, must start with 'acc://'"}
	}

	// Decode WASM bytecode from base64
	wasmBytes, err := base64.StdEncoding.DecodeString(deployParams.WasmB64)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid base64 WASM data"}
	}

	// Validate size limits (1-2 MB)
	const minSize = 1024        // 1 KB minimum
	const maxSize = 2 * 1024 * 1024 // 2 MB maximum
	if len(wasmBytes) < minSize {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("WASM module too small: %d bytes (min: %d)", len(wasmBytes), minSize)}
	}
	if len(wasmBytes) > maxSize {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("WASM module too large: %d bytes (max: %d)", len(wasmBytes), maxSize)}
	}

	// Comprehensive WASM audit for determinism
	auditReport := runtime.AuditWASMModule(wasmBytes)
	if !auditReport.Ok {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("WASM audit failed: %s", strings.Join(auditReport.Reasons, "; "))}
	}

	// Check namespace permissions if sequencer is available
	if s.sequencer != nil {
		engine := s.sequencer.GetExecutionEngine()
		if engine != nil {
			ctx := context.Background()
			if err := engine.ValidateNamespacePermission(ctx, deployParams.Addr); err != nil {
				return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("Namespace validation failed: %v", err)}
			}
		}
	}

	// Check if contract already exists
	exists, err := s.contractStore.HasModule(deployParams.Addr)
	if err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Failed to check contract existence: %v", err)}
	}
	if exists {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("Contract already deployed at address: %s", deployParams.Addr)}
	}

	// Save the module
	hash, err := s.contractStore.SaveModule(deployParams.Addr, wasmBytes)
	if err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Failed to deploy contract: %v", err)}
	}

	// Return the WASM hash
	result := &DeployContractResult{
		WasmHash: hex.EncodeToString(hash[:]),
	}

	return result, nil
}

// handleUpgradeContract handles accumen.upgradeContract() method
func (s *Server) handleUpgradeContract(params interface{}) (interface{}, *RPCError) {
	if s.contractStore == nil {
		return nil, &RPCError{Code: -32603, Message: "Contract store not available"}
	}

	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var upgradeParams UpgradeContractParams
	if err := json.Unmarshal(paramsBytes, &upgradeParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if upgradeParams.Addr == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: addr"}
	}
	if upgradeParams.WasmB64 == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: wasm_b64"}
	}

	// Validate address format
	if !strings.HasPrefix(upgradeParams.Addr, "acc://") {
		return nil, &RPCError{Code: -32602, Message: "Invalid address format, must start with 'acc://'"}
	}

	// Check L0 authority permission if sequencer is available
	if s.sequencer != nil {
		engine := s.sequencer.GetExecutionEngine()
		if engine != nil {
			ctx := context.Background()
			if err := engine.ValidateNamespacePermission(ctx, upgradeParams.Addr); err != nil {
				return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("L0 authority validation failed: %v", err)}
			}
		}
	}

	// Decode WASM bytecode from base64
	wasmBytes, err := base64.StdEncoding.DecodeString(upgradeParams.WasmB64)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid base64 WASM data"}
	}

	// Validate size limits (1-2 MB)
	const minSize = 1024        // 1 KB minimum
	const maxSize = 2 * 1024 * 1024 // 2 MB maximum
	if len(wasmBytes) < minSize {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("WASM module too small: %d bytes (min: %d)", len(wasmBytes), minSize)}
	}
	if len(wasmBytes) > maxSize {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("WASM module too large: %d bytes (max: %d)", len(wasmBytes), maxSize)}
	}

	// Comprehensive WASM audit for determinism
	auditReport := runtime.AuditWASMModule(wasmBytes)
	if !auditReport.Ok {
		return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("WASM audit failed: %s", strings.Join(auditReport.Reasons, "; "))}
	}

	// Save new version
	version, hash, err := s.contractStore.SaveVersion(upgradeParams.Addr, wasmBytes)
	if err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Failed to save contract version: %v", err)}
	}

	// Execute migration if specified
	if upgradeParams.MigrateEntry != nil && *upgradeParams.MigrateEntry != "" {
		if s.sequencer != nil {
			engine := s.sequencer.GetExecutionEngine()
			if engine != nil {
				// Execute migration in Execute mode
				ctx := context.Background()
				if err := engine.ExecuteMigration(ctx, upgradeParams.Addr, version, *upgradeParams.MigrateEntry, upgradeParams.MigrateArgs); err != nil {
					// Migration failed - the engine should handle rollback
					return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Migration failed: %v", err)}
				}
			}
		}
	}

	// Return the version and hash
	result := &UpgradeContractResult{
		Version:  version,
		WasmHash: hex.EncodeToString(hash[:]),
	}

	return result, nil
}

// handleActivateVersion handles accumen.activateVersion() method
func (s *Server) handleActivateVersion(params interface{}) (interface{}, *RPCError) {
	if s.contractStore == nil {
		return nil, &RPCError{Code: -32603, Message: "Contract store not available"}
	}

	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var activateParams ActivateVersionParams
	if err := json.Unmarshal(paramsBytes, &activateParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if activateParams.Addr == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: addr"}
	}
	if activateParams.Version <= 0 {
		return nil, &RPCError{Code: -32602, Message: "Missing or invalid field: version must be > 0"}
	}

	// Validate address format
	if !strings.HasPrefix(activateParams.Addr, "acc://") {
		return nil, &RPCError{Code: -32602, Message: "Invalid address format, must start with 'acc://'"}
	}

	// Check L0 authority permission if sequencer is available
	if s.sequencer != nil {
		engine := s.sequencer.GetExecutionEngine()
		if engine != nil {
			ctx := context.Background()
			if err := engine.ValidateNamespacePermission(ctx, activateParams.Addr); err != nil {
				return nil, &RPCError{Code: -32602, Message: fmt.Sprintf("L0 authority validation failed: %v", err)}
			}
		}
	}

	// Activate the version
	if err := s.contractStore.ActivateVersion(activateParams.Addr, activateParams.Version); err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Failed to activate version: %v", err)}
	}

	// Return success
	result := &ActivateVersionResult{
		Success: true,
	}

	return result, nil
}

// handleGetL1Receipt handles accumen.getL1Receipt() method
func (s *Server) handleGetL1Receipt(params interface{}) (interface{}, *RPCError) {
	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var receiptParams GetL1ReceiptParams
	if err := json.Unmarshal(paramsBytes, &receiptParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if receiptParams.Hash == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: hash"}
	}

	// Check if we have an indexer (for followers) or fallback to direct lookup
	if s.indexer != nil {
		// Query L1 receipt from indexer
		receipt, err := s.indexer.GetL1Receipt(receiptParams.Hash)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("L1 receipt not found: %v", err)}
		}

		// Convert to RPC result format
		result := &GetL1ReceiptResult{
			L1Hash:   receipt.L1Hash,
			DNTxID:   hex.EncodeToString(receipt.DNTxID),
			DNKey:    receipt.DNKey,
			Contract: receipt.Contract,
			Entry:    receipt.Entry,
			Metadata: receipt.Metadata,
		}

		// Convert anchor TX IDs to hex strings
		for _, anchorTxID := range receipt.AnchorTxIDs {
			result.AnchorTxIDs = append(result.AnchorTxIDs, hex.EncodeToString(anchorTxID))
		}

		return result, nil
	}

	// Fallback: Try to find receipt info from stored transaction data (sequencer mode)
	// Look for transaction with L1 hash in various formats
	possibleKeys := []string{
		fmt.Sprintf("tx:%s", receiptParams.Hash),
		fmt.Sprintf("l1_receipt:%s", receiptParams.Hash),
		fmt.Sprintf("l1:%s", receiptParams.Hash),
	}

	for _, key := range possibleKeys {
		data, err := s.kvStore.Get([]byte(key))
		if err != nil {
			continue // Try next key format
		}

		// Try to parse as MetadataEntry first
		var metadata indexer.MetadataEntry
		if err := json.Unmarshal(data, &metadata); err == nil && metadata.L1Hash == receiptParams.Hash {
			result := &GetL1ReceiptResult{
				L1Hash:   metadata.L1Hash,
				DNTxID:   hex.EncodeToString(metadata.DNTxID),
				DNKey:    metadata.DNKey,
				Contract: metadata.ContractAddr,
				Entry:    metadata.Entry,
				Metadata: &metadata,
			}
			return result, nil
		}

		// Try to parse as raw receipt data
		var receiptData map[string]interface{}
		if err := json.Unmarshal(data, &receiptData); err == nil {
			if hash, ok := receiptData["l1Hash"].(string); ok && hash == receiptParams.Hash {
				result := &GetL1ReceiptResult{
					L1Hash: hash,
				}

				// Extract optional fields
				if dnTxID, ok := receiptData["dnTxId"].(string); ok {
					result.DNTxID = dnTxID
				}
				if dnKey, ok := receiptData["dnKey"].(string); ok {
					result.DNKey = dnKey
				}
				if contract, ok := receiptData["contract"].(string); ok {
					result.Contract = contract
				}
				if entry, ok := receiptData["entry"].(string); ok {
					result.Entry = entry
				}

				return result, nil
			}
		}
	}

	// L1 receipt not found
	return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("L1 receipt not found for hash: %s", receiptParams.Hash)}
}

// handleGetTxReceipt handles accumen.getTxReceipt() method
func (s *Server) handleGetTxReceipt(params interface{}) (interface{}, *RPCError) {
	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var receiptParams GetTxReceiptParams
	if err := json.Unmarshal(paramsBytes, &receiptParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate hash parameter
	if receiptParams.Hash == "" {
		return nil, &RPCError{
			Code:    -32602,
			Message: "hash parameter is required",
		}
	}

	// Parse hex hash
	hashBytes, err := hex.DecodeString(receiptParams.Hash)
	if err != nil {
		return nil, &RPCError{
			Code:    -32602,
			Message: "hash must be valid hex string",
		}
	}

	if len(hashBytes) != 32 {
		return nil, &RPCError{
			Code:    -32602,
			Message: "hash must be 32 bytes (64 hex characters)",
		}
	}

	// Convert to [32]byte
	var hash [32]byte
	copy(hash[:], hashBytes)

	// Load receipt from storage
	receipt, height, index, err := state.LoadReceiptByHash(s.kvStore, hash)
	if err != nil {
		return nil, &RPCError{
			Code:    -32000,
			Message: "Receipt not found",
		}
	}

	// Convert events to result format
	events := make([]ReceiptEvent, len(receipt.Events))
	for i, event := range receipt.Events {
		events[i] = ReceiptEvent{
			Type: event.Type,
			Data: hex.EncodeToString(event.Data),
		}
	}

	// Build result
	result := GetTxReceiptResult{
		Height:  height,
		Index:   index,
		Events:  events,
		GasUsed: receipt.GasUsed,
		Error:   receipt.Error,
		L0TxIDs: receipt.L0TxIDs,
	}

	return result, nil
}

// handleGetBlockHeader handles accumen.getBlockHeader() method
func (s *Server) handleGetBlockHeader(params interface{}) (interface{}, *RPCError) {
	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var headerParams GetBlockHeaderParams
	if err := json.Unmarshal(paramsBytes, &headerParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Load block header from storage
	headerKey := fmt.Sprintf("/block/header/%d", headerParams.Height)
	headerBytes, err := s.kvStore.Get([]byte(headerKey))
	if err != nil {
		return nil, &RPCError{
			Code:    -32000,
			Message: fmt.Sprintf("Block header not found for height %d", headerParams.Height),
		}
	}

	// Deserialize block header using protobuf
	var header accumen.BlockHeader
	if err := proto.Unmarshal(headerBytes, &header); err != nil {
		return nil, &RPCError{
			Code:    -32000,
			Message: "Failed to parse block header",
		}
	}

	// Build result
	result := GetBlockHeaderResult{
		Height:      header.Height,
		PrevHash:    hex.EncodeToString(header.PrevHash),
		TxsRoot:     hex.EncodeToString(header.TxsRoot),
		ResultsRoot: hex.EncodeToString(header.ResultsRoot),
		StateRoot:   hex.EncodeToString(header.StateRoot),
		Time:        header.Time,
	}

	return result, nil
}

// generateTxID generates a unique transaction ID
func (s *Server) generateTxID() string {
	return fmt.Sprintf("tx_%d_%d", time.Now().UnixNano(), time.Now().UnixMicro()%1000000)
}

// encodeCallData encodes function call data (simplified)
func (s *Server) encodeCallData(entry string, args interface{}) []byte {
	// Simple encoding: entry:json(args)
	argsBytes, _ := json.Marshal(args)
	callData := fmt.Sprintf("%s:%s", entry, string(argsBytes))
	return []byte(callData)
}

// writeResult writes a successful JSON-RPC response
func (s *Server) writeResult(w http.ResponseWriter, id interface{}, result interface{}) {
	response := Response{
		ID:     id,
		Result: result,
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// writeError writes an error JSON-RPC response
func (s *Server) writeError(w http.ResponseWriter, id interface{}, code int, message string) {
	response := Response{
		ID: id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}

	w.WriteHeader(http.StatusOK) // JSON-RPC errors still return 200
	json.NewEncoder(w).Encode(response)
}

// IsRunning returns whether the server is running
func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// GetAddr returns the server address
func (s *Server) GetAddr() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.server != nil {
		return s.server.Addr
	}
	return ""
}

// validateJSONRPC performs basic JSON-RPC validation
func (s *Server) validateJSONRPC(req *Request) *RPCError {
	if req.Method == "" {
		return &RPCError{Code: -32600, Message: "Missing method"}
	}

	if !strings.HasPrefix(req.Method, "accumen.") {
		return &RPCError{Code: -32601, Message: "Method not found"}
	}

	return nil
}

// getMode returns the server mode
func (s *Server) getMode() string {
	if s.readOnly {
		return "follower"
	}
	return "sequencer"
}

// recordRequest increments the request counter
func (s *Server) recordRequest() {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	s.stats.RequestCount++
}

// recordError increments the error counter
func (s *Server) recordError() {
	s.stats.mu.Lock()
	defer s.stats.mu.Unlock()
	s.stats.ErrorCount++
}

// GetStats returns server statistics
func (s *Server) GetStats() *ServerStats {
	s.stats.mu.RLock()
	defer s.stats.mu.RUnlock()

	statsCopy := *s.stats
	statsCopy.Uptime = time.Since(s.stats.StartTime)
	return &statsCopy
}

// handleSimulate handles accumen.simulate() method
func (s *Server) handleSimulate(params interface{}) (interface{}, *RPCError) {
	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var simulateParams SimulateParams
	if err := json.Unmarshal(paramsBytes, &simulateParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if simulateParams.Contract == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: contract"}
	}
	if simulateParams.Entry == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: entry"}
	}

	// Get execution engine from sequencer
	if s.sequencer == nil {
		return nil, &RPCError{Code: -32603, Message: "Sequencer not available"}
	}

	engine := s.sequencer.GetExecutionEngine()
	if engine == nil {
		return nil, &RPCError{Code: -32603, Message: "Execution engine not available"}
	}

	// Perform simulation
	simResult, err := engine.Simulate(simulateParams.Contract, simulateParams.Entry, simulateParams.Args)
	if err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Simulation failed: %v", err)}
	}

	// Convert events to RPC format
	var rpcEvents []Event
	for _, event := range simResult.Events {
		rpcEvents = append(rpcEvents, Event{
			Type: event.Type,
			Data: hex.EncodeToString(event.Data),
		})
	}

	// Calculate ACME cost from gas used
	credits, acmeDecimal := pricing.QuoteForGas(simResult.GasUsed)

	// Build response
	result := &SimulateResult{
		GasUsed:          simResult.GasUsed,
		Events:           rpcEvents,
		L0Credits:        simResult.L0Credits,
		ACME:             acmeDecimal,
		EstimatedDNBytes: simResult.L0Bytes,
		Success:          simResult.Success,
		Error:            simResult.Error,
	}

	return result, nil
}