package l0api

import (
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/opendlt/accumulate-accumen/internal/accutil"
)

// SubmissionContext contains metadata about the L1 transaction context
// for embedding cross-links in L0 transactions
type SubmissionContext struct {
	// L1 transaction information
	L1TxHash    [32]byte `json:"l1TxHash"`    // L1 transaction hash
	L1Height    uint64   `json:"l1Height"`    // L1 block height
	L1Index     uint64   `json:"l1Index"`     // Transaction index within L1 block
	ChainID     string   `json:"chainId"`     // L1 chain identifier
	ContractURL string   `json:"contractUrl"` // Contract that triggered this L0 operation
	Timestamp   int64    `json:"timestamp"`   // Unix timestamp of L1 transaction

	// Sequencer information
	SequencerID string `json:"sequencerId"` // Sequencer identifier
}

// CreateSubmissionContext creates a SubmissionContext from L1 transaction details
func CreateSubmissionContext(l1TxHash [32]byte, height, index uint64, chainID, contractURL, sequencerID string) *SubmissionContext {
	return &SubmissionContext{
		L1TxHash:    l1TxHash,
		L1Height:    height,
		L1Index:     index,
		ChainID:     chainID,
		ContractURL: contractURL,
		Timestamp:   time.Now().Unix(),
		SequencerID: sequencerID,
	}
}

// CreateSubmissionContextFromBytes creates a SubmissionContext from a byte slice hash
func CreateSubmissionContextFromBytes(l1TxHashBytes []byte, height, index uint64, chainID, contractURL, sequencerID string) (*SubmissionContext, error) {
	if len(l1TxHashBytes) != 32 {
		return nil, fmt.Errorf("L1 transaction hash must be exactly 32 bytes, got %d", len(l1TxHashBytes))
	}

	var hash [32]byte
	copy(hash[:], l1TxHashBytes)

	return CreateSubmissionContext(hash, height, index, chainID, contractURL, sequencerID), nil
}

// HashL1Transaction creates a transaction hash from L1 transaction data
func HashL1Transaction(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// ExtractSubmissionInfo extracts submission information from a submitted transaction
type SubmissionInfo struct {
	TransactionHash string                     `json:"transactionHash"`
	CrossLink       *accutil.CrossLinkMetadata `json:"crossLink,omitempty"`
	Memo            string                     `json:"memo,omitempty"`
	Success         bool                       `json:"success"`
	Error           string                     `json:"error,omitempty"`
}

// ValidateSubmissionContext validates a submission context
func ValidateSubmissionContext(subCtx *SubmissionContext) error {
	if subCtx == nil {
		return fmt.Errorf("submission context cannot be nil")
	}

	// Check L1 transaction hash
	zeroHash := [32]byte{}
	if subCtx.L1TxHash == zeroHash {
		return fmt.Errorf("L1 transaction hash cannot be zero")
	}

	// Check chain ID
	if subCtx.ChainID == "" {
		return fmt.Errorf("chain ID cannot be empty")
	}

	// Check contract URL if provided
	if subCtx.ContractURL != "" {
		if contractURL, err := accutil.ParseURL(subCtx.ContractURL); err != nil {
			return fmt.Errorf("invalid contract URL: %w", err)
		} else if err := accutil.ValidateContractAddr(contractURL); err != nil {
			return fmt.Errorf("invalid contract address: %w", err)
		}
	}

	return nil
}