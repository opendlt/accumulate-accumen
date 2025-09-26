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

	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/engine/state/contracts"
	"github.com/opendlt/accumulate-accumen/follower/indexer"
	"github.com/opendlt/accumulate-accumen/sequencer"
	"github.com/opendlt/accumulate-accumen/types/l1"
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

	// Basic WASM validation - check magic bytes
	if len(wasmBytes) < 4 || string(wasmBytes[:4]) != "\x00asm" {
		return nil, &RPCError{Code: -32602, Message: "Invalid WASM module: missing magic bytes"}
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