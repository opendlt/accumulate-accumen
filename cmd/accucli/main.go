package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/opendlt/accumulate-accumen/internal/rpc"
)

var (
	rpcEndpoint = "http://127.0.0.1:8666"
	client      = &http.Client{Timeout: 30 * time.Second}
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "accucli",
		Short: "Accumen CLI for L1 transaction submission and status checking",
		Long:  "Command-line interface for interacting with Accumen L1 sequencer",
	}

	rootCmd.PersistentFlags().StringVar(&rpcEndpoint, "rpc", "http://127.0.0.1:8666", "RPC endpoint URL")

	rootCmd.AddCommand(
		statusCommand(),
		deployCommand(),
		submitCommand(),
		queryCommand(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Get sequencer status",
		RunE: func(cmd *cobra.Command, args []string) error {
			req := rpc.RPCRequest{
				ID:     1,
				Method: "accumen.status",
				Params: map[string]interface{}{},
			}

			var resp struct {
				Result *rpc.StatusResult `json:"result"`
				Error  *rpc.RPCError     `json:"error"`
			}

			if err := makeRPCCall(req, &resp); err != nil {
				return fmt.Errorf("RPC call failed: %v", err)
			}

			if resp.Error != nil {
				return fmt.Errorf("RPC error: %s", resp.Error.Message)
			}

			if resp.Result == nil {
				return fmt.Errorf("no result returned")
			}

			// Pretty-print the result
			prettyPrint(map[string]interface{}{
				"chain_id":    resp.Result.ChainID,
				"height":      resp.Result.Height,
				"running":     resp.Result.Running,
				"last_anchor": formatTimePtr(resp.Result.LastAnchor),
			})

			return nil
		},
	}
}

func deployCommand() *cobra.Command {
	var addr string
	var wasmPath string

	cmd := &cobra.Command{
		Use:   "deploy",
		Short: "Deploy WASM contract",
		RunE: func(cmd *cobra.Command, args []string) error {
			if addr == "" {
				return fmt.Errorf("--addr is required")
			}
			if wasmPath == "" {
				return fmt.Errorf("--wasm is required")
			}

			// Read WASM file
			wasmBytes, err := os.ReadFile(wasmPath)
			if err != nil {
				return fmt.Errorf("failed to read WASM file: %v", err)
			}

			// Encode to base64
			wasmB64 := base64.StdEncoding.EncodeToString(wasmBytes)

			req := rpc.RPCRequest{
				ID:     1,
				Method: "accumen.deployContract",
				Params: map[string]interface{}{
					"addr":    addr,
					"wasm_b64": wasmB64,
				},
			}

			var resp struct {
				Result *rpc.DeployResult `json:"result"`
				Error  *rpc.RPCError     `json:"error"`
			}

			if err := makeRPCCall(req, &resp); err != nil {
				return fmt.Errorf("RPC call failed: %v", err)
			}

			if resp.Error != nil {
				return fmt.Errorf("RPC error: %s", resp.Error.Message)
			}

			if resp.Result == nil {
				return fmt.Errorf("no result returned")
			}

			// Pretty-print the result
			prettyPrint(map[string]interface{}{
				"success":    true,
				"address":    addr,
				"wasm_hash":  resp.Result.WasmHash,
				"wasm_size":  len(wasmBytes),
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&addr, "addr", "", "Contract address (required, e.g., acc://counter.acme)")
	cmd.Flags().StringVar(&wasmPath, "wasm", "", "Path to WASM file (required)")

	return cmd
}

func submitCommand() *cobra.Command {
	var contract string
	var entry string
	var args []string

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit L1 transaction",
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			if contract == "" {
				return fmt.Errorf("--contract is required")
			}
			if entry == "" {
				return fmt.Errorf("--entry is required")
			}

			// Parse key:value arguments
			txArgs := make(map[string]string)
			for _, arg := range args {
				parts := strings.SplitN(arg, ":", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid argument format '%s', expected key:value", arg)
				}
				txArgs[parts[0]] = parts[1]
			}

			req := rpc.RPCRequest{
				ID:     1,
				Method: "accumen.submitTx",
				Params: map[string]interface{}{
					"contract": contract,
					"entry":    entry,
					"args":     txArgs,
				},
			}

			var resp struct {
				Result *rpc.SubmitTxResult `json:"result"`
				Error  *rpc.RPCError       `json:"error"`
			}

			if err := makeRPCCall(req, &resp); err != nil {
				return fmt.Errorf("RPC call failed: %v", err)
			}

			if resp.Error != nil {
				return fmt.Errorf("RPC error: %s", resp.Error.Message)
			}

			if resp.Result == nil {
				return fmt.Errorf("no result returned")
			}

			// Pretty-print the result
			result := map[string]interface{}{
				"tx_hash": resp.Result.TxHash,
				"status":  resp.Result.Status,
			}
			if resp.Result.BlockHeight > 0 {
				result["block_height"] = resp.Result.BlockHeight
			}
			if resp.Result.Error != "" {
				result["error"] = resp.Result.Error
			}

			prettyPrint(result)

			return nil
		},
	}

	cmd.Flags().StringVar(&contract, "contract", "", "Contract address (required)")
	cmd.Flags().StringVar(&entry, "entry", "", "Entry point function (required)")
	cmd.Flags().StringArrayVar(&args, "arg", []string{}, "Function arguments in key:value format (repeatable)")

	return cmd
}

func queryCommand() *cobra.Command {
	var contract string
	var key string

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query contract state",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contract == "" {
				return fmt.Errorf("--contract is required")
			}
			if key == "" {
				return fmt.Errorf("--key is required")
			}

			req := rpc.RPCRequest{
				ID:     1,
				Method: "accumen.query",
				Params: map[string]interface{}{
					"contract": contract,
					"key":      key,
				},
			}

			var resp struct {
				Result *rpc.QueryResult `json:"result"`
				Error  *rpc.RPCError    `json:"error"`
			}

			if err := makeRPCCall(req, &resp); err != nil {
				return fmt.Errorf("RPC call failed: %v", err)
			}

			if resp.Error != nil {
				return fmt.Errorf("RPC error: %s", resp.Error.Message)
			}

			if resp.Result == nil {
				return fmt.Errorf("no result returned")
			}

			// Pretty-print the result
			result := map[string]interface{}{
				"contract": contract,
				"key":      key,
				"exists":   resp.Result.Exists,
			}
			if resp.Result.Exists {
				result["value"] = resp.Result.Value
				result["type"] = resp.Result.Type
			}

			prettyPrint(result)

			return nil
		},
	}

	cmd.Flags().StringVar(&contract, "contract", "", "Contract address (required)")
	cmd.Flags().StringVar(&key, "key", "", "State key to query (required)")

	return cmd
}

func makeRPCCall(request rpc.RPCRequest, response interface{}) error {
	// Marshal request
	reqBytes, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %v", err)
	}

	// Create HTTP request
	httpReq, err := http.NewRequest("POST", rpcEndpoint, bytes.NewReader(reqBytes))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %v", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	// Make request
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("failed to decode response: %v", err)
	}

	return nil
}

// prettyPrint formats and prints JSON objects with proper indentation
func prettyPrint(data interface{}) {
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Error formatting output: %v\n", err)
		fmt.Printf("%+v\n", data)
		return
	}
	fmt.Println(string(jsonBytes))
}

// formatTimePtr formats a time pointer for display, returning nil string if nil
func formatTimePtr(t *time.Time) interface{} {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}