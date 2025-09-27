package l0api

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/build"

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

// SubmitWithCrossLink submits an envelope with embedded L1→L0 cross-link information
func (c *Client) SubmitWithCrossLink(ctx context.Context, env *build.EnvelopeBuilder, subCtx *SubmissionContext) (*api.SubmitResponse, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	if subCtx == nil {
		return nil, fmt.Errorf("submission context cannot be nil")
	}

	// Add cross-link metadata to the envelope
	if err := accutil.WithCrossLinkDetailed(
		env,
		subCtx.L1TxHash,
		subCtx.L1Height,
		subCtx.L1Index,
		subCtx.ChainID,
		subCtx.ContractURL,
		subCtx.Timestamp,
	); err != nil {
		return nil, fmt.Errorf("failed to add cross-link metadata: %w", err)
	}

	// Submit the envelope using the standard method
	return c.SubmitEnvelopeWithContext(ctx, env, subCtx)
}

// SubmitEnvelopeWithContext submits an envelope with enhanced context and cross-linking
func (c *Client) SubmitEnvelopeWithContext(ctx context.Context, env *build.EnvelopeBuilder, subCtx *SubmissionContext) (*api.SubmitResponse, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	// Add Accumen-specific metadata if context is provided
	if subCtx != nil {
		// Create cross-link metadata
		crossLink := &accutil.CrossLinkMetadata{
			Version:     "1.0",
			L1TxHash:    fmt.Sprintf("%x", subCtx.L1TxHash),
			L1Height:    subCtx.L1Height,
			L1Index:     subCtx.L1Index,
			ChainID:     subCtx.ChainID,
			ContractURL: subCtx.ContractURL,
			Timestamp:   subCtx.Timestamp,
		}

		// Add Accumen metadata with cross-link
		if err := accutil.WithAccumenMetadata(env, subCtx.SequencerID, crossLink); err != nil {
			return nil, fmt.Errorf("failed to add Accumen metadata: %w", err)
		}
	}

	// Sign envelope if signer is configured
	var envelope interface{}
	var err error

	if c.signer != nil {
		envelope, err = env.Sign(c.signer)
		if err != nil {
			return nil, fmt.Errorf("failed to sign envelope: %w", err)
		}
	} else {
		envelope, err = env.Done()
		if err != nil {
			return nil, fmt.Errorf("failed to complete envelope: %w", err)
		}
	}

	// Convert to messaging envelope
	msgEnvelope, ok := envelope.(*build.EnvelopeBuilder)
	if !ok {
		return nil, fmt.Errorf("envelope is not the expected type")
	}

	// Submit to network using the base client method
	return c.Submit(ctx, msgEnvelope)
}

// BatchSubmitWithCrossLinks submits multiple envelopes with cross-link information
func (c *Client) BatchSubmitWithCrossLinks(ctx context.Context, envelopes []*build.EnvelopeBuilder, contexts []*SubmissionContext) ([]*api.SubmitResponse, error) {
	if len(envelopes) != len(contexts) {
		return nil, fmt.Errorf("number of envelopes (%d) must match number of contexts (%d)", len(envelopes), len(contexts))
	}

	responses := make([]*api.SubmitResponse, 0, len(envelopes))
	errors := make([]error, 0)

	for i, env := range envelopes {
		resp, err := c.SubmitWithCrossLink(ctx, env, contexts[i])
		if err != nil {
			errors = append(errors, fmt.Errorf("envelope %d: %w", i, err))
			responses = append(responses, nil)
		} else {
			responses = append(responses, resp)
		}
	}

	if len(errors) > 0 {
		return responses, fmt.Errorf("batch submission had %d errors: %v", len(errors), errors)
	}

	return responses, nil
}

// SubmitL1OriginatedTransaction submits a transaction that originated from an L1 contract execution
func (c *Client) SubmitL1OriginatedTransaction(ctx context.Context, env *build.EnvelopeBuilder, l1TxHash [32]byte, contractURL string, chainID string, height uint64) (*api.SubmitResponse, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	// Create submission context
	subCtx := &SubmissionContext{
		L1TxHash:    l1TxHash,
		L1Height:    height,
		L1Index:     0, // We don't have index information from this call
		ChainID:     chainID,
		ContractURL: contractURL,
		Timestamp:   time.Now().Unix(),
		SequencerID: c.getSequencerID(),
	}

	return c.SubmitWithCrossLink(ctx, env, subCtx)
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

// getSequencerID returns an identifier for this sequencer instance
func (c *Client) getSequencerID() string {
	// Use endpoint as a simple sequencer identifier
	// In a more sophisticated implementation, this could be a configured UUID
	return fmt.Sprintf("accumen@%s", c.endpoint)
}

// SubmitWithMetadata submits an envelope with custom metadata
func (c *Client) SubmitWithMetadata(ctx context.Context, env *build.EnvelopeBuilder, metadata map[string]any) (*api.SubmitResponse, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	if metadata != nil {
		// Add custom metadata
		if err := accutil.WithMetadataJSON(env, metadata); err != nil {
			return nil, fmt.Errorf("failed to add metadata: %w", err)
		}
	}

	// Submit using standard method
	return c.SubmitEnvelopeWithContext(ctx, env, nil)
}

// SubmitWithMemo submits an envelope with a memo field
func (c *Client) SubmitWithMemo(ctx context.Context, env *build.EnvelopeBuilder, memo string) (*api.SubmitResponse, error) {
	if env == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	if memo != "" {
		// Add memo to envelope
		accutil.WithMemo(env, memo)
	}

	// Submit using standard method
	return c.SubmitEnvelopeWithContext(ctx, env, nil)
}

// ExtractSubmissionInfo extracts submission information from a submitted transaction
type SubmissionInfo struct {
	TransactionHash string                     `json:"transactionHash"`
	CrossLink       *accutil.CrossLinkMetadata `json:"crossLink,omitempty"`
	Memo            string                     `json:"memo,omitempty"`
	Success         bool                       `json:"success"`
	Error           string                     `json:"error,omitempty"`
}

// GetSubmissionInfo extracts information from a submit response
func GetSubmissionInfo(resp *api.SubmitResponse) *SubmissionInfo {
	if resp == nil {
		return &SubmissionInfo{
			Success: false,
			Error:   "no response provided",
		}
	}

	info := &SubmissionInfo{
		TransactionHash: fmt.Sprintf("%x", resp.TransactionHash),
		Success:         len(resp.TransactionHash) > 0,
	}

	// TODO: Extract cross-link and memo information from response
	// This would require access to the transaction metadata from the response

	return info
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
