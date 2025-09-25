package json

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMetadataBuilder_BuildMetadata(t *testing.T) {
	builder := NewMetadataBuilder()

	// Set skip validation since we might not have ajv installed
	builder.SetSkipValidation(true)

	// Create sample arguments
	args := MetadataArgs{
		ChainID:       "accumen-testnet-1",
		BlockHeight:   12345,
		TxIndex:       2,
		TxHash:        []byte{0x01, 0x02, 0x03, 0x04, 0x05},
		AppHash:       []byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE},
		Time:          time.Date(2024, 1, 15, 12, 30, 45, 0, time.UTC),
		ContractAddr:  "acc://contract.acme",
		Entry:         "transfer",
		Nonce:         []byte{0x11, 0x22, 0x33},
		GasUsed:       150000,
		GasScheduleID: "v1.0.0",
		CreditsL0:     225,
		CreditsL1:     50,
		CreditsTotal:  275,
		AcmeBurnt:     "0.00275",
		L0Outputs: []map[string]any{
			{
				"type":    "write_data",
				"account": "acc://target.acme",
				"data":    "0x48656c6c6f",
			},
		},
		Events: []EventData{
			{Key: "transfer.from", Value: "acc://alice.acme"},
			{Key: "transfer.to", Value: "acc://bob.acme"},
		},
	}

	// Build metadata
	jsonData, err := builder.BuildMetadata(args)
	if err != nil {
		t.Fatalf("Failed to build metadata: %v", err)
	}

	// Parse the JSON back to verify structure
	var result map[string]any
	if err := json.Unmarshal(jsonData, &result); err != nil {
		t.Fatalf("Failed to unmarshal result: %v", err)
	}

	// Test required top-level fields
	testRequiredField(t, result, "version", "1.0.0")
	testRequiredField(t, result, "chainId", "accumen-testnet-1")

	// Test L1 section
	l1, ok := result["l1"].(map[string]any)
	if !ok {
		t.Fatal("L1 section missing or invalid")
	}
	testRequiredField(t, l1, "blockHeight", float64(12345)) // JSON numbers become float64
	testRequiredField(t, l1, "txIndex", float64(2))
	testRequiredField(t, l1, "txHash", "0102030405")
	testRequiredField(t, l1, "appHash", "aabbccddee")
	testRequiredField(t, l1, "timestamp", "2024-01-15T12:30:45Z")

	// Test contract section
	contract, ok := result["contract"].(map[string]any)
	if !ok {
		t.Fatal("Contract section missing or invalid")
	}
	testRequiredField(t, contract, "address", "acc://contract.acme")
	testRequiredField(t, contract, "entry", "transfer")
	testRequiredField(t, contract, "nonce", "112233")

	// Test execution section
	execution, ok := result["execution"].(map[string]any)
	if !ok {
		t.Fatal("Execution section missing or invalid")
	}
	testRequiredField(t, execution, "gasUsed", float64(150000))
	testRequiredField(t, execution, "gasScheduleId", "v1.0.0")

	// Test credits section
	credits, ok := result["credits"].(map[string]any)
	if !ok {
		t.Fatal("Credits section missing or invalid")
	}
	testRequiredField(t, credits, "l0", float64(225))
	testRequiredField(t, credits, "l1", float64(50))
	testRequiredField(t, credits, "total", float64(275))

	// Test expense section
	expense, ok := result["expense"].(map[string]any)
	if !ok {
		t.Fatal("Expense section missing or invalid")
	}
	testRequiredField(t, expense, "acmeBurnt", "0.00275")

	// Test L0 outputs
	l0Outputs, ok := result["l0Outputs"].([]any)
	if !ok {
		t.Fatal("L0Outputs section missing or invalid")
	}
	if len(l0Outputs) != 1 {
		t.Fatalf("Expected 1 L0 output, got %d", len(l0Outputs))
	}

	// Test events
	events, ok := result["events"].([]any)
	if !ok {
		t.Fatal("Events section missing or invalid")
	}
	if len(events) != 2 {
		t.Fatalf("Expected 2 events, got %d", len(events))
	}
}

func TestMetadataBuilder_SetSignature(t *testing.T) {
	builder := NewMetadataBuilder()
	builder.SetSkipValidation(true)

	// Build basic metadata first
	args := MetadataArgs{
		ChainID:       "test-chain",
		BlockHeight:   1,
		TxIndex:       0,
		TxHash:        []byte{0x01},
		AppHash:       []byte{0x02},
		Time:          time.Now().UTC(),
		ContractAddr:  "acc://test.acme",
		Entry:         "test",
		Nonce:         []byte{0x03},
		GasUsed:       1000,
		GasScheduleID: "v1.0.0",
		CreditsL0:     10,
		CreditsL1:     5,
		CreditsTotal:  15,
		AcmeBurnt:     "0.00015",
		L0Outputs:     []map[string]any{},
		Events:        []EventData{},
	}

	_, err := builder.BuildMetadata(args)
	if err != nil {
		t.Fatalf("Failed to build metadata: %v", err)
	}

	// Add signature
	publicKey := []byte{0xAA, 0xBB, 0xCC}
	signature := []byte{0x11, 0x22, 0x33, 0x44}

	signedJSON, err := builder.SetSignature(publicKey, signature)
	if err != nil {
		t.Fatalf("Failed to set signature: %v", err)
	}

	// Parse and verify signature was added
	var result map[string]any
	if err := json.Unmarshal(signedJSON, &result); err != nil {
		t.Fatalf("Failed to unmarshal signed result: %v", err)
	}

	sig, ok := result["signature"].(map[string]any)
	if !ok {
		t.Fatal("Signature section missing or invalid")
	}

	testRequiredField(t, sig, "publicKey", "aabbcc")
	testRequiredField(t, sig, "signature", "11223344")
	testRequiredField(t, sig, "algorithm", "ed25519")
}

func TestMetadataBuilder_ValidateRequiredFields(t *testing.T) {
	builder := NewMetadataBuilder()
	builder.SetSkipValidation(true)

	// Test validation before building
	err := builder.ValidateRequiredFields()
	if err == nil {
		t.Fatal("Expected validation error for empty metadata")
	}

	// Build minimal metadata
	args := MetadataArgs{
		ChainID:       "test",
		BlockHeight:   1,
		TxIndex:       0,
		TxHash:        []byte{0x01},
		AppHash:       []byte{0x02},
		Time:          time.Now(),
		ContractAddr:  "test",
		Entry:         "test",
		Nonce:         []byte{0x03},
		GasUsed:       100,
		GasScheduleID: "v1.0.0",
		CreditsL0:     10,
		CreditsL1:     5,
		CreditsTotal:  15,
		AcmeBurnt:     "0.00015",
		L0Outputs:     []map[string]any{},
		Events:        []EventData{},
	}

	_, err = builder.BuildMetadata(args)
	if err != nil {
		t.Fatalf("Failed to build metadata: %v", err)
	}

	// Now validation should pass
	err = builder.ValidateRequiredFields()
	if err != nil {
		t.Fatalf("Validation failed for complete metadata: %v", err)
	}
}

func TestMetadataBuilder_PrettyJSON(t *testing.T) {
	builder := NewMetadataBuilder()
	builder.SetSkipValidation(true)

	args := MetadataArgs{
		ChainID:       "test",
		BlockHeight:   1,
		TxIndex:       0,
		TxHash:        []byte{0x01},
		AppHash:       []byte{0x02},
		Time:          time.Now(),
		ContractAddr:  "test",
		Entry:         "test",
		Nonce:         []byte{0x03},
		GasUsed:       100,
		GasScheduleID: "v1.0.0",
		CreditsL0:     10,
		CreditsL1:     5,
		CreditsTotal:  15,
		AcmeBurnt:     "0.00015",
		L0Outputs:     []map[string]any{},
		Events:        []EventData{},
	}

	_, err := builder.BuildMetadata(args)
	if err != nil {
		t.Fatalf("Failed to build metadata: %v", err)
	}

	prettyJSON, err := builder.PrettyJSON()
	if err != nil {
		t.Fatalf("Failed to get pretty JSON: %v", err)
	}

	// Just verify we got some formatted JSON
	if len(prettyJSON) == 0 {
		t.Fatal("Pretty JSON is empty")
	}

	// Should contain newlines for formatting
	if !contains(prettyJSON, '\n') {
		t.Fatal("Pretty JSON doesn't appear to be formatted")
	}
}

func TestMetadataBuilder_Reset(t *testing.T) {
	builder := NewMetadataBuilder()
	builder.SetSkipValidation(true)

	// Build metadata
	args := MetadataArgs{
		ChainID:       "test",
		BlockHeight:   1,
		TxIndex:       0,
		TxHash:        []byte{0x01},
		AppHash:       []byte{0x02},
		Time:          time.Now(),
		ContractAddr:  "test",
		Entry:         "test",
		Nonce:         []byte{0x03},
		GasUsed:       100,
		GasScheduleID: "v1.0.0",
		CreditsL0:     10,
		CreditsL1:     5,
		CreditsTotal:  15,
		AcmeBurnt:     "0.00015",
		L0Outputs:     []map[string]any{},
		Events:        []EventData{},
	}

	_, err := builder.BuildMetadata(args)
	if err != nil {
		t.Fatalf("Failed to build metadata: %v", err)
	}

	// Reset and verify state is cleared
	builder.Reset()

	err = builder.ValidateRequiredFields()
	if err == nil {
		t.Fatal("Expected validation error after reset")
	}
}

// Helper function to test required fields
func testRequiredField(t *testing.T, data map[string]any, field string, expected any) {
	t.Helper()

	actual, exists := data[field]
	if !exists {
		t.Fatalf("Required field '%s' missing", field)
	}

	if actual != expected {
		t.Fatalf("Field '%s': expected %v, got %v", field, expected, actual)
	}
}

// Helper function to check if byte slice contains a byte
func contains(data []byte, b byte) bool {
	for _, v := range data {
		if v == b {
			return true
		}
	}
	return false
}