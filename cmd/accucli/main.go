package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3/jsonrpc"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/bridge/l0api"
	"github.com/opendlt/accumulate-accumen/bridge/outputs"
	"github.com/opendlt/accumulate-accumen/engine/runtime"
	"github.com/opendlt/accumulate-accumen/engine/state"
	"github.com/opendlt/accumulate-accumen/internal/crypto/keystore"
	"github.com/opendlt/accumulate-accumen/internal/crypto/signer"
	"github.com/opendlt/accumulate-accumen/internal/rpc"
)

var (
	rpcEndpoint = "http://127.0.0.1:8666"
	httpClient  = &http.Client{Timeout: 30 * time.Second}
	dnEndpoint  = "https://testnet.accumulate.net/v3" // Default DN endpoint
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "accucli",
		Short: "Accumen CLI for L1 transaction submission and status checking",
		Long:  "Command-line interface for interacting with Accumen L1 sequencer",
	}

	rootCmd.PersistentFlags().StringVar(&rpcEndpoint, "rpc", "http://127.0.0.1:8666", "RPC endpoint URL")
	rootCmd.PersistentFlags().StringVar(&dnEndpoint, "dn", "https://testnet.accumulate.net/v3", "DN endpoint URL for scope operations")

	rootCmd.AddCommand(
		statusCommand(),
		deployCommand(),
		submitCommand(),
		queryCommand(),
		keysCommand(),
		keystoreCommand(),
		txCommand(),
		simulateCommand(),
		scopeCommand(),
		auditWasmCommand(),
		snapshotCommand(),
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
					"addr":     addr,
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
				"success":   true,
				"address":   addr,
				"wasm_hash": resp.Result.WasmHash,
				"wasm_size": len(wasmBytes),
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

func keysCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Key management utilities",
		Long:  "Generate, save, and display Ed25519 keys for Accumen",
	}

	cmd.AddCommand(keysGenCommand())
	cmd.AddCommand(keysSaveCommand())
	cmd.AddCommand(keysShowCommand())

	return cmd
}

func keysGenCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "gen",
		Short: "Generate a new Ed25519 key pair",
		Long:  "Generate a new Ed25519 key pair and display public and private keys in hex format",
		RunE: func(cmd *cobra.Command, args []string) error {
			pubHex, privHex, err := signer.GenerateEd25519()
			if err != nil {
				return fmt.Errorf("failed to generate key pair: %v", err)
			}

			prettyPrint(map[string]interface{}{
				"public_key":  pubHex,
				"private_key": privHex,
				"note":        "Store the private key securely. It will not be shown again.",
			})

			return nil
		},
	}
}

func keysSaveCommand() *cobra.Command {
	var filePath string
	var privKey string

	cmd := &cobra.Command{
		Use:   "save",
		Short: "Save a private key to a file",
		Long:  "Save a private key (in hex format) to a file with secure permissions (0600)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				return fmt.Errorf("--file is required")
			}
			if privKey == "" {
				return fmt.Errorf("--priv is required")
			}

			err := signer.SaveToFile(filePath, privKey)
			if err != nil {
				return fmt.Errorf("failed to save key: %v", err)
			}

			prettyPrint(map[string]interface{}{
				"success":     true,
				"file":        filePath,
				"permissions": "0600 (owner read/write only)",
				"note":        "Key saved securely. Verify file permissions on your system.",
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to save the private key file (required)")
	cmd.Flags().StringVar(&privKey, "priv", "", "Private key in hex format (required)")

	return cmd
}

func keysShowCommand() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Display the private key from a file",
		Long:  "Load and display the private key from a file (in hex format)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if filePath == "" {
				return fmt.Errorf("--file is required")
			}

			privHex, err := signer.LoadFromFile(filePath)
			if err != nil {
				return fmt.Errorf("failed to load key: %v", err)
			}

			// Calculate public key from private key
			privBytes, err := hex.DecodeString(privHex)
			if err != nil {
				return fmt.Errorf("invalid private key format: %v", err)
			}

			// For Ed25519, the public key is the last 32 bytes of the 64-byte private key
			// Or we can derive it properly
			if len(privBytes) != 64 {
				return fmt.Errorf("invalid private key length: expected 64 bytes, got %d", len(privBytes))
			}

			publicKey := privBytes[32:] // Ed25519 public key is stored as second half

			prettyPrint(map[string]interface{}{
				"file":        filePath,
				"private_key": privHex,
				"public_key":  hex.EncodeToString(publicKey),
				"warning":     "Private key displayed. Ensure terminal output is secure.",
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&filePath, "file", "", "Path to the private key file (required)")

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
	resp, err := httpClient.Do(httpReq)
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

// keystoreCommand creates the keystore command with subcommands
func keystoreCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keystore",
		Short: "Keystore management commands",
		Long:  "Manage encrypted keystore for Ed25519 keys with aliases",
	}

	cmd.AddCommand(
		keystoreInitCommand(),
		keystoreGenCommand(),
		keystoreImportCommand(),
		keystoreListCommand(),
	)

	return cmd
}

// keystoreInitCommand creates the keystore init subcommand
func keystoreInitCommand() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new keystore",
		Long:  "Initialize a new keystore at the specified path",
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				return fmt.Errorf("--path is required")
			}

			// Create the keystore
			ks, err := keystore.New(path)
			if err != nil {
				return fmt.Errorf("failed to initialize keystore: %v", err)
			}

			prettyPrint(map[string]interface{}{
				"success":       true,
				"keystore_path": ks.Path(),
				"message":       "Keystore initialized successfully",
				"note":          "Keys will be encrypted using OS-specific user information",
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "Path to keystore directory (required)")
	cmd.MarkFlagRequired("path")

	return cmd
}

// keystoreGenCommand creates the keystore gen subcommand
func keystoreGenCommand() *cobra.Command {
	var keystorePath string
	var alias string

	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate a new key in the keystore",
		Long:  "Generate a new Ed25519 key pair and store it in the keystore with the given alias",
		RunE: func(cmd *cobra.Command, args []string) error {
			if keystorePath == "" {
				return fmt.Errorf("--keystore is required")
			}
			if alias == "" {
				return fmt.Errorf("--alias is required")
			}

			// Open the keystore
			ks, err := keystore.New(keystorePath)
			if err != nil {
				return fmt.Errorf("failed to open keystore: %v", err)
			}

			// Generate new key
			if err := ks.Create(alias); err != nil {
				return fmt.Errorf("failed to generate key: %v", err)
			}

			// Get the public key for display
			pubKey, err := ks.GetPublicKey(alias)
			if err != nil {
				return fmt.Errorf("failed to get public key: %v", err)
			}

			pubKeyHash, err := ks.PubKeyHash(alias)
			if err != nil {
				return fmt.Errorf("failed to get public key hash: %v", err)
			}

			prettyPrint(map[string]interface{}{
				"success":         true,
				"alias":           alias,
				"public_key":      hex.EncodeToString(pubKey),
				"public_key_hash": hex.EncodeToString(pubKeyHash),
				"keystore":        keystorePath,
				"note":            "Private key is securely stored in the keystore",
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&keystorePath, "keystore", "", "Path to keystore directory (required)")
	cmd.Flags().StringVar(&alias, "alias", "", "Key alias (required)")
	cmd.MarkFlagRequired("keystore")
	cmd.MarkFlagRequired("alias")

	return cmd
}

// keystoreImportCommand creates the keystore import subcommand
func keystoreImportCommand() *cobra.Command {
	var keystorePath string
	var alias string
	var privHex string

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a private key into the keystore",
		Long:  "Import an existing Ed25519 private key (in hex format) into the keystore with the given alias",
		RunE: func(cmd *cobra.Command, args []string) error {
			if keystorePath == "" {
				return fmt.Errorf("--keystore is required")
			}
			if alias == "" {
				return fmt.Errorf("--alias is required")
			}
			if privHex == "" {
				return fmt.Errorf("--priv is required")
			}

			// Open the keystore
			ks, err := keystore.New(keystorePath)
			if err != nil {
				return fmt.Errorf("failed to open keystore: %v", err)
			}

			// Import the key
			if err := ks.Import(alias, privHex); err != nil {
				return fmt.Errorf("failed to import key: %v", err)
			}

			// Get the public key for display
			pubKey, err := ks.GetPublicKey(alias)
			if err != nil {
				return fmt.Errorf("failed to get public key: %v", err)
			}

			pubKeyHash, err := ks.PubKeyHash(alias)
			if err != nil {
				return fmt.Errorf("failed to get public key hash: %v", err)
			}

			prettyPrint(map[string]interface{}{
				"success":         true,
				"alias":           alias,
				"public_key":      hex.EncodeToString(pubKey),
				"public_key_hash": hex.EncodeToString(pubKeyHash),
				"keystore":        keystorePath,
				"message":         "Private key imported and encrypted successfully",
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&keystorePath, "keystore", "", "Path to keystore directory (required)")
	cmd.Flags().StringVar(&alias, "alias", "", "Key alias (required)")
	cmd.Flags().StringVar(&privHex, "priv", "", "Private key in hex format (required)")
	cmd.MarkFlagRequired("keystore")
	cmd.MarkFlagRequired("alias")
	cmd.MarkFlagRequired("priv")

	return cmd
}

// keystoreListCommand creates the keystore list subcommand
func keystoreListCommand() *cobra.Command {
	var keystorePath string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all keys in the keystore",
		Long:  "List all key aliases and their public keys stored in the keystore",
		RunE: func(cmd *cobra.Command, args []string) error {
			if keystorePath == "" {
				return fmt.Errorf("--keystore is required")
			}

			// Open the keystore
			ks, err := keystore.New(keystorePath)
			if err != nil {
				return fmt.Errorf("failed to open keystore: %v", err)
			}

			// List all keys
			entries := ks.List()
			if len(entries) == 0 {
				prettyPrint(map[string]interface{}{
					"keystore": keystorePath,
					"keys":     []interface{}{},
					"message":  "No keys found in keystore",
				})
				return nil
			}

			// Format entries for display
			keys := make([]map[string]interface{}, len(entries))
			for i, entry := range entries {
				keys[i] = map[string]interface{}{
					"alias":           entry.Alias,
					"public_key":      entry.PubKeyHex,
					"public_key_hash": entry.PubKeyHash,
					"created_at":      entry.CreatedAt.Format(time.RFC3339),
				}
			}

			prettyPrint(map[string]interface{}{
				"keystore":  keystorePath,
				"key_count": len(entries),
				"keys":      keys,
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&keystorePath, "keystore", "", "Path to keystore directory (required)")
	cmd.MarkFlagRequired("keystore")

	return cmd
}

// txCommand creates the tx command with subcommands for envelope manipulation
func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tx",
		Short: "Transaction envelope operations",
		Long:  "Commands for signing and submitting transaction envelopes",
	}

	cmd.AddCommand(
		txSignCommand(),
		txSubmitCommand(),
	)

	return cmd
}

// txSignCommand creates the tx sign subcommand
func txSignCommand() *cobra.Command {
	var envelopeB64 string
	var keystorePath string
	var aliases []string

	cmd := &cobra.Command{
		Use:   "sign",
		Short: "Sign an envelope with keystore keys",
		Long:  "Add signatures to a base64-encoded envelope using specified keystore aliases",
		RunE: func(cmd *cobra.Command, args []string) error {
			if envelopeB64 == "" {
				return fmt.Errorf("--envelope is required")
			}
			if keystorePath == "" {
				return fmt.Errorf("--keystore is required")
			}
			if len(aliases) == 0 {
				return fmt.Errorf("at least one --alias is required")
			}

			// Open the keystore
			ks, err := keystore.New(keystorePath)
			if err != nil {
				return fmt.Errorf("failed to open keystore: %v", err)
			}

			// Decode the envelope
			envelope, err := l0api.DecodeEnvelopeBase64(envelopeB64)
			if err != nil {
				return fmt.Errorf("failed to decode envelope: %v", err)
			}

			// Create signers for each alias
			var signers []signer.Signer
			for _, alias := range aliases {
				if !ks.HasKey(alias) {
					return fmt.Errorf("key alias '%s' not found in keystore", alias)
				}
				signers = append(signers, signer.NewKeystoreSigner(ks, alias))
			}

			// Create multi-signer
			multiSigner := signer.NewMultiSigner(signers...)

			// Create envelope builder from existing envelope
			// Note: This approach assumes we can reconstruct a builder from an envelope
			// The actual implementation may need adjustment based on the build package API
			builder, err := l0api.DecodeToEnvelopeBuilder(envelopeB64)
			if err != nil {
				return fmt.Errorf("failed to create envelope builder: %v", err)
			}

			// Sign the envelope
			if err := multiSigner.SignEnvelope(builder); err != nil {
				return fmt.Errorf("failed to sign envelope: %v", err)
			}

			// Encode the signed envelope
			signedEnvelopeB64, err := l0api.EncodeEnvelopeBuilderBase64(builder)
			if err != nil {
				return fmt.Errorf("failed to encode signed envelope: %v", err)
			}

			// Output the signed envelope
			prettyPrint(map[string]interface{}{
				"success":        true,
				"signed_aliases": aliases,
				"keystore":       keystorePath,
				"envelope":       signedEnvelopeB64,
				"note":           "Envelope has been signed with the specified keys",
			})

			return nil
		},
	}

	cmd.Flags().StringVar(&envelopeB64, "envelope", "", "Base64-encoded envelope to sign (required)")
	cmd.Flags().StringVar(&keystorePath, "keystore", "", "Path to keystore directory (required)")
	cmd.Flags().StringArrayVar(&aliases, "alias", []string{}, "Key alias to use for signing (repeatable, required)")
	cmd.MarkFlagRequired("envelope")
	cmd.MarkFlagRequired("keystore")

	return cmd
}

// txSubmitCommand creates the tx submit subcommand
func txSubmitCommand() *cobra.Command {
	var envelopeB64 string
	var l0Endpoint string

	cmd := &cobra.Command{
		Use:   "submit",
		Short: "Submit a signed envelope to L0",
		Long:  "Submit a base64-encoded signed envelope to the Accumulate L0 network",
		RunE: func(cmd *cobra.Command, args []string) error {
			if envelopeB64 == "" {
				return fmt.Errorf("--envelope is required")
			}
			if l0Endpoint == "" {
				return fmt.Errorf("--l0 is required")
			}

			// Decode the envelope
			envelope, err := l0api.DecodeEnvelopeBase64(envelopeB64)
			if err != nil {
				return fmt.Errorf("failed to decode envelope: %v", err)
			}

			// Create L0 client
			config := l0api.DefaultClientConfig(l0Endpoint)
			client, err := l0api.NewClient(config)
			if err != nil {
				return fmt.Errorf("failed to create L0 client: %v", err)
			}
			defer client.Close()

			// Submit the envelope
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			resp, err := client.Submit(ctx, envelope)
			if err != nil {
				return fmt.Errorf("failed to submit envelope: %v", err)
			}

			// Output the result
			result := map[string]interface{}{
				"success":          true,
				"transaction_hash": resp.TransactionHash.String(),
				"l0_endpoint":      l0Endpoint,
			}

			if resp.Simple != nil {
				result["simple"] = resp.Simple
			}
			if resp.Error != nil {
				result["error"] = resp.Error
			}

			prettyPrint(result)

			return nil
		},
	}

	cmd.Flags().StringVar(&envelopeB64, "envelope", "", "Base64-encoded signed envelope to submit (required)")
	cmd.Flags().StringVar(&l0Endpoint, "l0", "", "L0 API endpoint URL (required)")
	cmd.MarkFlagRequired("envelope")
	cmd.MarkFlagRequired("l0")

	return cmd
}

// simulateCommand creates the simulate subcommand
func simulateCommand() *cobra.Command {
	var contract string
	var entry string
	var args []string

	cmd := &cobra.Command{
		Use:   "simulate",
		Short: "Simulate contract execution without state changes",
		Long:  "Preview gas costs, L0 credits, and ACME requirements for a contract function call",
		RunE: func(cmd *cobra.Command, cmdArgs []string) error {
			if contract == "" {
				return fmt.Errorf("--contract is required")
			}
			if entry == "" {
				return fmt.Errorf("--entry is required")
			}

			// Parse key:value arguments
			txArgs := make(map[string]interface{})
			for _, arg := range args {
				parts := strings.SplitN(arg, ":", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid argument format '%s', expected key:value", arg)
				}
				// Try to parse value as number, fallback to string
				if val, err := strconv.ParseFloat(parts[1], 64); err == nil {
					txArgs[parts[0]] = val
				} else if val, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
					txArgs[parts[0]] = val
				} else if val, err := strconv.ParseBool(parts[1]); err == nil {
					txArgs[parts[0]] = val
				} else {
					txArgs[parts[0]] = parts[1]
				}
			}

			req := RPCRequest{
				ID:     1,
				Method: "accumen.simulate",
				Params: map[string]interface{}{
					"contract": contract,
					"entry":    entry,
					"args":     txArgs,
				},
			}

			var resp struct {
				Result *SimulateRPCResult `json:"result"`
				Error  *RPCError          `json:"error"`
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
				"contract":           contract,
				"entry":              entry,
				"args":               txArgs,
				"success":            resp.Result.Success,
				"gas_used":           resp.Result.GasUsed,
				"l0_credits":         resp.Result.L0Credits,
				"acme_cost":          resp.Result.ACME,
				"estimated_dn_bytes": resp.Result.EstimatedDNBytes,
				"events_count":       len(resp.Result.Events),
			}

			if resp.Result.Error != "" {
				result["error"] = resp.Result.Error
			}

			if len(resp.Result.Events) > 0 {
				result["events"] = resp.Result.Events
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

// SimulateRPCResult represents the result of accumen.simulate RPC call
type SimulateRPCResult struct {
	GasUsed          uint64          `json:"gasUsed"`
	Events           []SimulateEvent `json:"events"`
	L0Credits        uint64          `json:"l0Credits"`
	ACME             string          `json:"acme"`
	EstimatedDNBytes uint64          `json:"estimatedDNBytes"`
	Success          bool            `json:"success"`
	Error            string          `json:"error,omitempty"`
}

// SimulateEvent represents an event in the simulate result
type SimulateEvent struct {
	Type string `json:"type"`
	Data string `json:"data"` // hex-encoded
}

// scopeCommand creates the scope command with subcommands
func scopeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scope",
		Short: "Manage contract authority scopes",
		Long:  "Commands for pausing, unpausing, and viewing contract authority scopes on DN",
	}

	cmd.AddCommand(
		scopePauseCommand(),
		scopeUnpauseCommand(),
		scopeShowCommand(),
	)

	return cmd
}

// scopePauseCommand creates the scope pause subcommand
func scopePauseCommand() *cobra.Command {
	var contractFlag string

	cmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a contract",
		Long:  "Pause a contract by updating its authority scope on DN",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contractFlag == "" {
				return fmt.Errorf("--contract is required")
			}

			return setScopePaused(contractFlag, true)
		},
	}

	cmd.Flags().StringVar(&contractFlag, "contract", "", "Contract URL (e.g., acc://example.acme/contract)")
	cmd.MarkFlagRequired("contract")

	return cmd
}

// scopeUnpauseCommand creates the scope unpause subcommand
func scopeUnpauseCommand() *cobra.Command {
	var contractFlag string

	cmd := &cobra.Command{
		Use:   "unpause",
		Short: "Unpause a contract",
		Long:  "Unpause a contract by updating its authority scope on DN",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contractFlag == "" {
				return fmt.Errorf("--contract is required")
			}

			return setScopePaused(contractFlag, false)
		},
	}

	cmd.Flags().StringVar(&contractFlag, "contract", "", "Contract URL (e.g., acc://example.acme/contract)")
	cmd.MarkFlagRequired("contract")

	return cmd
}

// scopeShowCommand creates the scope show subcommand
func scopeShowCommand() *cobra.Command {
	var contractFlag string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show contract authority scope",
		Long:  "Display the current authority scope for a contract from DN",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contractFlag == "" {
				return fmt.Errorf("--contract is required")
			}

			return showScope(contractFlag)
		},
	}

	cmd.Flags().StringVar(&contractFlag, "contract", "", "Contract URL (e.g., acc://example.acme/contract)")
	cmd.MarkFlagRequired("contract")

	return cmd
}

// setScopePaused updates the paused state of a contract's authority scope
func setScopePaused(contractURL string, paused bool) error {
	// Parse contract URL
	contractURLParsed, err := url.Parse(contractURL)
	if err != nil {
		return fmt.Errorf("invalid contract URL: %v", err)
	}

	// Parse DN URL
	dnURLParsed, err := url.Parse(dnEndpoint)
	if err != nil {
		return fmt.Errorf("invalid DN endpoint: %v", err)
	}

	// Create DN client
	client, err := v3.New(dnEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create DN client: %v", err)
	}

	// Update the scope
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := outputs.SetPaused(ctx, client, dnURLParsed, contractURLParsed, paused); err != nil {
		return fmt.Errorf("failed to update contract pause state: %v", err)
	}

	action := "paused"
	if !paused {
		action = "unpaused"
	}

	prettyPrint(map[string]interface{}{
		"success":  true,
		"contract": contractURL,
		"action":   action,
		"message":  fmt.Sprintf("Contract %s successfully", action),
	})

	return nil
}

// showScope displays the current authority scope for a contract
func showScope(contractURL string) error {
	// Create DN client
	client, err := v3.New(dnEndpoint)
	if err != nil {
		return fmt.Errorf("failed to create DN client: %v", err)
	}

	// Fetch the scope
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	scope, err := outputs.FetchFromDN(ctx, client, contractURL)
	if err != nil {
		return fmt.Errorf("failed to fetch authority scope: %v", err)
	}

	// Display the scope
	prettyPrint(map[string]interface{}{
		"contract":   scope.Contract,
		"version":    scope.Version,
		"paused":     scope.Paused,
		"created_at": scope.CreatedAt.Format(time.RFC3339),
		"updated_at": scope.UpdatedAt.Format(time.RFC3339),
		"expires_at": formatTimePtr(scope.ExpiresAt),
		"allowed_operations": map[string]interface{}{
			"write_data":  scope.Allowed.WriteData,
			"send_tokens": scope.Allowed.SendTokens,
			"update_auth": scope.Allowed.UpdateAuth,
		},
	})

	return nil
}

// auditWasmCommand creates the audit-wasm subcommand
func auditWasmCommand() *cobra.Command {
	var wasmPath string

	cmd := &cobra.Command{
		Use:   "audit-wasm",
		Short: "Audit WASM module for determinism",
		Long:  "Analyze a WASM module and report any non-deterministic features or violations",
		RunE: func(cmd *cobra.Command, args []string) error {
			if wasmPath == "" {
				return fmt.Errorf("--wasm is required")
			}

			// Read WASM file
			wasmBytes, err := os.ReadFile(wasmPath)
			if err != nil {
				return fmt.Errorf("failed to read WASM file: %v", err)
			}

			// Run audit
			auditReport := runtime.AuditWASMModule(wasmBytes)

			// Prepare result
			result := map[string]interface{}{
				"file":         wasmPath,
				"size":         len(wasmBytes),
				"passed":       auditReport.Ok,
				"memory_pages": auditReport.MemPages,
				"exports":      auditReport.Exports,
			}

			if len(auditReport.Reasons) > 0 {
				result["violations"] = auditReport.Reasons
			}

			// Add summary
			if auditReport.Ok {
				result["summary"] = "WASM module passed all determinism checks"
			} else {
				result["summary"] = fmt.Sprintf("WASM module failed audit with %d violations", len(auditReport.Reasons))
			}

			prettyPrint(result)

			// Exit with error code if audit failed
			if !auditReport.Ok {
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&wasmPath, "wasm", "", "Path to WASM file to audit (required)")
	cmd.MarkFlagRequired("wasm")

	return cmd
}

// snapshotCommand creates the snapshot management command with subcommands
func snapshotCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage state snapshots",
		Long:  "Commands for creating, importing, exporting, and listing state snapshots",
	}

	cmd.AddCommand(
		snapshotExportCommand(),
		snapshotImportCommand(),
		snapshotListCommand(),
	)

	return cmd
}

// snapshotExportCommand creates the snapshot export subcommand
func snapshotExportCommand() *cobra.Command {
	var outputFile string
	var dataDir string

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export current state to a snapshot file",
		Long:  "Creates a snapshot of the current state and saves it to the specified file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if outputFile == "" {
				return fmt.Errorf("--out is required")
			}

			// Default data directory
			if dataDir == "" {
				dataDir = "data/l1"
			}

			// Load KV store (assuming Badger for persistence)
			kvStore, err := state.NewBadgerStore(dataDir)
			if err != nil {
				return fmt.Errorf("failed to open KV store: %v", err)
			}
			defer kvStore.Close()

			// Create snapshot metadata
			meta := &state.SnapshotMeta{
				Height:  0, // Will be determined from state if available
				Time:    time.Now(),
				AppHash: "manual-export",
			}

			// Write snapshot
			if err := state.WriteSnapshot(outputFile, kvStore, meta); err != nil {
				return fmt.Errorf("failed to write snapshot: %v", err)
			}

			// Report success
			result := map[string]interface{}{
				"output_file": outputFile,
				"height":      meta.Height,
				"num_keys":    meta.NumKeys,
				"time":        meta.Time.Format(time.RFC3339),
			}

			prettyPrint(result)
			return nil
		},
	}

	cmd.Flags().StringVar(&outputFile, "out", "", "Output snapshot file path (required)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "data/l1", "Data directory path")
	cmd.MarkFlagRequired("out")

	return cmd
}

// snapshotImportCommand creates the snapshot import subcommand
func snapshotImportCommand() *cobra.Command {
	var inputFile string
	var dataDir string
	var force bool

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import state from a snapshot file",
		Long:  "Restores state from a snapshot file. WARNING: This will overwrite existing state!",
		RunE: func(cmd *cobra.Command, args []string) error {
			if inputFile == "" {
				return fmt.Errorf("--in is required")
			}

			// Default data directory
			if dataDir == "" {
				dataDir = "data/l1"
			}

			// Safety check - ensure node is stopped
			if !force {
				fmt.Println("WARNING: Importing a snapshot will completely replace the current state.")
				fmt.Println("Please ensure the Accumen node is stopped before proceeding.")
				fmt.Print("Continue? (y/N): ")

				var response string
				fmt.Scanln(&response)
				if strings.ToLower(response) != "y" && strings.ToLower(response) != "yes" {
					fmt.Println("Import cancelled.")
					return nil
				}
			}

			// Load KV store
			kvStore, err := state.NewBadgerStore(dataDir)
			if err != nil {
				return fmt.Errorf("failed to open KV store: %v", err)
			}
			defer kvStore.Close()

			// Restore from snapshot
			if err := state.RestoreFromSnapshot(inputFile, kvStore); err != nil {
				return fmt.Errorf("failed to restore from snapshot: %v", err)
			}

			// Get metadata for reporting
			meta, err := state.GetSnapshotMeta(inputFile)
			if err != nil {
				return fmt.Errorf("failed to read snapshot metadata: %v", err)
			}

			// Report success
			result := map[string]interface{}{
				"input_file": inputFile,
				"height":     meta.Height,
				"num_keys":   meta.NumKeys,
				"time":       meta.Time.Format(time.RFC3339),
				"app_hash":   meta.AppHash,
			}

			prettyPrint(result)
			fmt.Println("State import completed successfully.")
			return nil
		},
	}

	cmd.Flags().StringVar(&inputFile, "in", "", "Input snapshot file path (required)")
	cmd.Flags().StringVar(&dataDir, "data-dir", "data/l1", "Data directory path")
	cmd.Flags().BoolVar(&force, "force", false, "Skip confirmation prompt")
	cmd.MarkFlagRequired("in")

	return cmd
}

// snapshotListCommand creates the snapshot list subcommand
func snapshotListCommand() *cobra.Command {
	var snapshotDir string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List available snapshots",
		Long:  "Shows all available snapshot files with their metadata",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default snapshot directory
			if snapshotDir == "" {
				snapshotDir = "data/l1/snapshots"
			}

			// List snapshots
			snapshots, err := state.ListSnapshots(snapshotDir)
			if err != nil {
				return fmt.Errorf("failed to list snapshots: %v", err)
			}

			if len(snapshots) == 0 {
				fmt.Printf("No snapshots found in %s\n", snapshotDir)
				return nil
			}

			// Prepare results
			result := map[string]interface{}{
				"snapshot_dir": snapshotDir,
				"count":        len(snapshots),
				"snapshots":    make([]map[string]interface{}, 0, len(snapshots)),
			}

			for _, snapshot := range snapshots {
				snapshotData := map[string]interface{}{
					"filename": snapshot.Filename,
					"size":     snapshot.Size,
					"mod_time": snapshot.ModTime.Format(time.RFC3339),
				}

				if snapshot.Meta != nil {
					snapshotData["height"] = snapshot.Meta.Height
					snapshotData["num_keys"] = snapshot.Meta.NumKeys
					snapshotData["time"] = snapshot.Meta.Time.Format(time.RFC3339)
					snapshotData["app_hash"] = snapshot.Meta.AppHash
				}

				result["snapshots"] = append(result["snapshots"].([]map[string]interface{}), snapshotData)
			}

			prettyPrint(result)
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotDir, "snapshot-dir", "data/l1/snapshots", "Snapshot directory path")

	return cmd
}
