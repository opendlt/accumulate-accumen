package rpc

import (
	"context"
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
	"github.com/opendlt/accumulate-accumen/sequencer"
)

// Server provides a JSON-RPC interface for Accumen
type Server struct {
	mu            sync.RWMutex
	server        *http.Server
	sequencer     *sequencer.Sequencer
	kvStore       state.KVStore
	contractStore *contracts.Store
	running       bool
}

// Dependencies holds the required dependencies for the RPC server
type Dependencies struct {
	Sequencer     *sequencer.Sequencer
	KVStore       state.KVStore
	ContractStore *contracts.Store
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
type SubmitTxParams struct {
	Contract string      `json:"contract"`
	Entry    string      `json:"entry"`
	Args     interface{} `json:"args,omitempty"`
	From     string      `json:"from,omitempty"`
	GasLimit uint64      `json:"gasLimit,omitempty"`
	Nonce    uint64      `json:"nonce,omitempty"`
}

// SubmitTxResult represents the result of accumen.submitTx()
type SubmitTxResult struct {
	TxHash string `json:"txHash"`
}

// QueryParams represents parameters for accumen.query()
type QueryParams struct {
	Contract string `json:"contract"`
	Key      string `json:"key"`
}

// QueryResult represents the result of accumen.query()
type QueryResult struct {
	Value  *string `json:"value"`
	Exists bool    `json:"exists"`
	Type   string  `json:"type,omitempty"`
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

// NewServer creates a new RPC server
func NewServer(deps *Dependencies) *Server {
	return &Server{
		sequencer:     deps.Sequencer,
		kvStore:       deps.KVStore,
		contractStore: deps.ContractStore,
		running:       false,
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

	// Route method
	var result interface{}
	var err *RPCError

	switch req.Method {
	case "accumen.status":
		result, err = s.handleStatus()
	case "accumen.submitTx":
		result, err = s.handleSubmitTx(req.Params)
	case "accumen.query":
		result, err = s.handleQuery(req.Params)
	case "accumen.deployContract":
		result, err = s.handleDeployContract(req.Params)
	default:
		err = &RPCError{Code: -32601, Message: "Method not found"}
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

	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"running":   s.sequencer.IsRunning(),
	}

	json.NewEncoder(w).Encode(health)
}

// handleStatus handles accumen.status() method
func (s *Server) handleStatus() (interface{}, *RPCError) {
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

	// Parse parameters
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params"}
	}

	var submitParams SubmitTxParams
	if err := json.Unmarshal(paramsBytes, &submitParams); err != nil {
		return nil, &RPCError{Code: -32602, Message: "Invalid params structure"}
	}

	// Validate required fields
	if submitParams.Contract == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: contract"}
	}
	if submitParams.Entry == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: entry"}
	}

	// Set defaults
	if submitParams.From == "" {
		submitParams.From = "acc://default.acme"
	}
	if submitParams.GasLimit == 0 {
		submitParams.GasLimit = 1000000 // Default 1M gas
	}

	// Create transaction
	tx := &sequencer.Transaction{
		ID:       s.generateTxID(),
		From:     submitParams.From,
		To:       submitParams.Contract,
		Data:     s.encodeCallData(submitParams.Entry, submitParams.Args),
		GasLimit: submitParams.GasLimit,
		GasPrice: 1, // Default gas price
		Nonce:    submitParams.Nonce,
		Priority: 100, // Normal priority
		Size:     uint64(len(s.encodeCallData(submitParams.Entry, submitParams.Args))),
	}

	// Submit transaction
	if err := s.sequencer.SubmitTransaction(tx); err != nil {
		return nil, &RPCError{Code: -32603, Message: fmt.Sprintf("Failed to submit transaction: %v", err)}
	}

	result := &SubmitTxResult{
		TxHash: tx.ID,
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
	if queryParams.Key == "" {
		return nil, &RPCError{Code: -32602, Message: "Missing required field: key"}
	}

	// Create state key (contract + key combination)
	stateKey := fmt.Sprintf("%s:%s", queryParams.Contract, queryParams.Key)

	// Query from state store
	value, err_kv := s.kvStore.Get(stateKey)
	exists := err_kv == nil

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