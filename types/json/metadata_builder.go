package json

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MetadataArgs contains arguments for building L1→L0 transaction metadata
type MetadataArgs struct {
	ChainID       string
	BlockHeight   uint64
	TxIndex       int
	TxHash        []byte
	AppHash       []byte
	Time          time.Time
	ContractAddr  string
	Entry         string
	Nonce         []byte
	GasUsed       uint64
	GasScheduleID string
	CreditsL0     uint64
	CreditsL1     uint64
	CreditsTotal  uint64
	AcmeBurnt     string
	L0Outputs     []map[string]any
	Events        []EventData
}

// EventData represents an event key-value pair
type EventData struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// MetadataBuilder constructs canonical L1→L0 JSON metadata
type MetadataBuilder struct {
	data         map[string]any
	skipValidate bool
}

// NewMetadataBuilder creates a new metadata builder
func NewMetadataBuilder() *MetadataBuilder {
	return &MetadataBuilder{
		data:         make(map[string]any),
		skipValidate: false,
	}
}

// SetSkipValidation sets whether to skip JSON schema validation
func (mb *MetadataBuilder) SetSkipValidation(skip bool) {
	mb.skipValidate = skip
}

// BuildMetadata constructs the canonical L1→L0 transaction metadata JSON
func (mb *MetadataBuilder) BuildMetadata(args MetadataArgs) ([]byte, error) {
	// Build the canonical structure
	mb.data = map[string]any{
		"version": "1.0.0",
		"chainId": args.ChainID,
		"l1": map[string]any{
			"blockHeight": args.BlockHeight,
			"txIndex":     args.TxIndex,
			"txHash":      hex.EncodeToString(args.TxHash),
			"appHash":     hex.EncodeToString(args.AppHash),
			"timestamp":   args.Time.UTC().Format(time.RFC3339),
		},
		"contract": map[string]any{
			"address": args.ContractAddr,
			"entry":   args.Entry,
			"nonce":   hex.EncodeToString(args.Nonce),
		},
		"execution": map[string]any{
			"gasUsed":       args.GasUsed,
			"gasScheduleId": args.GasScheduleID,
		},
		"credits": map[string]any{
			"l0":    args.CreditsL0,
			"l1":    args.CreditsL1,
			"total": args.CreditsTotal,
		},
		"expense": map[string]any{
			"acmeBurnt": args.AcmeBurnt,
		},
		"l0Outputs": args.L0Outputs,
		"events":    args.Events,
	}

	// Convert to JSON
	jsonData, err := json.Marshal(mb.data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata JSON: %w", err)
	}

	// Validate against schema if not skipped
	if !mb.skipValidate {
		if err := mb.validate(jsonData); err != nil {
			return nil, fmt.Errorf("metadata validation failed: %w", err)
		}
	}

	return jsonData, nil
}

// SetSignature adds the signature to the metadata and returns the final JSON
func (mb *MetadataBuilder) SetSignature(publicKey, signature []byte) ([]byte, error) {
	if mb.data == nil {
		return nil, fmt.Errorf("metadata not built yet - call BuildMetadata first")
	}

	// Add signature to the data
	mb.data["signature"] = map[string]any{
		"publicKey": hex.EncodeToString(publicKey),
		"signature": hex.EncodeToString(signature),
		"algorithm": "ed25519",
	}

	// Re-marshal with signature
	jsonData, err := json.Marshal(mb.data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal signed metadata JSON: %w", err)
	}

	// Validate again if not skipped
	if !mb.skipValidate {
		if err := mb.validate(jsonData); err != nil {
			return nil, fmt.Errorf("signed metadata validation failed: %w", err)
		}
	}

	return jsonData, nil
}

// validate validates JSON against the schema using ajv-cli if available
func (mb *MetadataBuilder) validate(jsonData []byte) error {
	// Check if ajv-cli is available
	if !mb.isAjvAvailable() {
		// No-op if ajv is not present
		return nil
	}

	// Create temporary file for JSON data
	cmd := exec.Command("ajv", "validate", "-s", "schemas/accumen-tx-metadata.schema.json", "-d", "-")
	cmd.Stdin = strings.NewReader(string(jsonData))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("schema validation failed: %s", string(output))
	}

	return nil
}

// isAjvAvailable checks if ajv-cli is available in the system
func (mb *MetadataBuilder) isAjvAvailable() bool {
	_, err := exec.LookPath("ajv")
	return err == nil
}

// GetRawData returns the raw metadata map (for testing purposes)
func (mb *MetadataBuilder) GetRawData() map[string]any {
	return mb.data
}

// Reset clears the builder state
func (mb *MetadataBuilder) Reset() {
	mb.data = make(map[string]any)
}

// ValidateRequiredFields performs basic validation of required fields
func (mb *MetadataBuilder) ValidateRequiredFields() error {
	if mb.data == nil {
		return fmt.Errorf("no metadata built")
	}

	requiredFields := []string{"version", "chainId", "l1", "contract", "execution", "credits", "expense"}
	for _, field := range requiredFields {
		if _, exists := mb.data[field]; !exists {
			return fmt.Errorf("required field missing: %s", field)
		}
	}

	// Check nested required fields
	if l1, ok := mb.data["l1"].(map[string]any); ok {
		l1Required := []string{"blockHeight", "txIndex", "txHash", "appHash", "timestamp"}
		for _, field := range l1Required {
			if _, exists := l1[field]; !exists {
				return fmt.Errorf("required L1 field missing: %s", field)
			}
		}
	} else {
		return fmt.Errorf("l1 section is not properly formatted")
	}

	if contract, ok := mb.data["contract"].(map[string]any); ok {
		contractRequired := []string{"address", "entry", "nonce"}
		for _, field := range contractRequired {
			if _, exists := contract[field]; !exists {
				return fmt.Errorf("required contract field missing: %s", field)
			}
		}
	} else {
		return fmt.Errorf("contract section is not properly formatted")
	}

	return nil
}

// PrettyJSON returns formatted JSON for debugging
func (mb *MetadataBuilder) PrettyJSON() ([]byte, error) {
	if mb.data == nil {
		return nil, fmt.Errorf("no metadata built")
	}

	return json.MarshalIndent(mb.data, "", "  ")
}
