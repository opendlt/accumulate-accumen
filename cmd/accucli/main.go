package main

import (
	"bytes"
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

			fmt.Printf("Chain ID: %s\n", resp.Result.ChainID)
			fmt.Printf("Height: %d\n", resp.Result.Height)
			fmt.Printf("Running: %t\n", resp.Result.Running)
			if resp.Result.LastAnchor != nil {
				fmt.Printf("Last Anchor: %s\n", resp.Result.LastAnchor.Format(time.RFC3339))
			}

			return nil
		},
	}
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

			fmt.Printf("Transaction Hash: %s\n", resp.Result.TxHash)
			fmt.Printf("Status: %s\n", resp.Result.Status)
			if resp.Result.BlockHeight > 0 {
				fmt.Printf("Block Height: %d\n", resp.Result.BlockHeight)
			}
			if resp.Result.Error != "" {
				fmt.Printf("Error: %s\n", resp.Result.Error)
			}

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

			fmt.Printf("Contract: %s\n", contract)
			fmt.Printf("Key: %s\n", key)
			fmt.Printf("Exists: %t\n", resp.Result.Exists)
			if resp.Result.Exists {
				fmt.Printf("Value: %s\n", resp.Result.Value)
				fmt.Printf("Type: %s\n", resp.Result.Type)
			}

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