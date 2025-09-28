package l0api

import (
	"context"
	"fmt"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3/jsonrpc"
	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"

	"github.com/opendlt/accumulate-accumen/internal/crypto/devsigner"
	"github.com/opendlt/accumulate-accumen/internal/crypto/signer"
)

// Client represents an Accumulate L0 API v3 client
type Client struct {
	endpoint       string
	client         *jsonrpc.Client
	config         *ClientConfig
	signer         signer.Signer
	signerSelector signer.SignerSelector
}

// ClientConfig defines configuration for the L0 API client
type ClientConfig struct {
	// API endpoint URL
	Endpoint string
	// HTTP client timeout
	Timeout time.Duration
	// Maximum retries for failed requests
	MaxRetries int
	// Retry delay
	RetryDelay time.Duration
	// User agent string
	UserAgent string
	// Enable debug logging
	Debug bool
	// DEPRECATED: Sequencer private key for signing (hex-encoded, development only)
	// Use signer parameter in NewClient instead
	SequencerKey string
}

// DefaultClientConfig returns a default client configuration
func DefaultClientConfig(endpoint string) *ClientConfig {
	return &ClientConfig{
		Endpoint:   endpoint,
		Timeout:    30 * time.Second,
		MaxRetries: 3,
		RetryDelay: time.Second,
		UserAgent:  "accumen/1.0",
		Debug:      false,
	}
}

// NewClient creates a new Accumulate L0 API client
func NewClient(config *ClientConfig) (*Client, error) {
	return NewClientWithSigner(config, nil)
}

// NewClientWithSigner creates a new Accumulate L0 API client with a signer
func NewClientWithSigner(config *ClientConfig, sig signer.Signer) (*Client, error) {
	return NewClientWithSignerSelector(config, sig, nil)
}

// NewClientWithSignerSelector creates a new Accumulate L0 API client with a signer selector
func NewClientWithSignerSelector(config *ClientConfig, defaultSigner signer.Signer, selector signer.SignerSelector) (*Client, error) {
	if config == nil {
		return nil, fmt.Errorf("client config cannot be nil")
	}

	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}

	// Create HTTP client with timeout
	// Create JSON-RPC client (HTTP client configuration not available in new API)
	rpcClient := jsonrpc.NewClient(config.Endpoint)

	// Note: HTTP client timeout configuration not available in new API
	_ = config.Timeout

	// If no signer provided but SequencerKey is set, create legacy signer for backward compatibility
	if defaultSigner == nil && config.SequencerKey != "" {
		legacySigner, err := devsigner.NewFromHex(config.SequencerKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create legacy signer from SequencerKey: %w", err)
		}
		defaultSigner = legacySigner.AsNewSigner()
	}

	return &Client{
		endpoint:       config.Endpoint,
		client:         rpcClient,
		config:         config,
		signer:         defaultSigner,
		signerSelector: selector,
	}, nil
}

// Query executes a general query against the Accumulate network
func (c *Client) Query(ctx context.Context, scope *url.URL, query api.Query) (api.Record, error) {
	if query == nil {
		return nil, fmt.Errorf("query cannot be nil")
	}

	var result api.Record
	err := c.executeWithRetry(ctx, func() error {
		var err error
		result, err = c.client.Query(ctx, scope, query)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return result, nil
}

// QueryTransaction queries transaction information using the new API
func (c *Client) QueryTransaction(ctx context.Context, txid []byte) (api.Record, error) {
	if len(txid) == 0 {
		return nil, fmt.Errorf("transaction ID cannot be empty")
	}

	// For now, stub the transaction query until proper TxID construction is available
	// TODO: Implement proper transaction query with new API
	return nil, fmt.Errorf("QueryTransaction not yet implemented for new API")
}

// QueryChain queries chain information using the new API
func (c *Client) QueryChain(ctx context.Context, account *url.URL, chainQuery *api.ChainQuery) (*api.ChainRecord, error) {
	if account == nil {
		return nil, fmt.Errorf("account URL cannot be nil")
	}
	if chainQuery == nil {
		return nil, fmt.Errorf("chain query cannot be nil")
	}

	result, err := c.Query(ctx, account, chainQuery)
	if err != nil {
		return nil, fmt.Errorf("chain query failed: %w", err)
	}

	// Try to cast to ChainRecord
	if chainRecord, ok := result.(*api.ChainRecord); ok {
		return chainRecord, nil
	}

	return nil, fmt.Errorf("unexpected result type: expected *api.ChainRecord, got %T", result)
}

// QueryPending queries pending transactions using the new API
func (c *Client) QueryPending(ctx context.Context, account *url.URL, pendingQuery *api.PendingQuery) (api.Record, error) {
	if account == nil {
		return nil, fmt.Errorf("account URL cannot be nil")
	}
	if pendingQuery == nil {
		return nil, fmt.Errorf("pending query cannot be nil")
	}

	result, err := c.Query(ctx, account, pendingQuery)
	if err != nil {
		return nil, fmt.Errorf("pending query failed: %w", err)
	}

	return result, nil
}

// QueryNetworkStatus queries network status information using the new API
func (c *Client) QueryNetworkStatus(ctx context.Context, opts api.NetworkStatusOptions) (*api.NetworkStatus, error) {
	var result *api.NetworkStatus
	err := c.executeWithRetry(ctx, func() error {
		var err error
		result, err = c.client.NetworkStatus(ctx, opts)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("network status query failed: %w", err)
	}

	return result, nil
}

// Submit submits a transaction envelope to the network using the new API
func (c *Client) Submit(ctx context.Context, envelope *messaging.Envelope, opts api.SubmitOptions) ([]*api.Submission, error) {
	if envelope == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	var result []*api.Submission
	err := c.executeWithRetry(ctx, func() error {
		var err error
		result, err = c.client.Submit(ctx, envelope, opts)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("submit failed: %w", err)
	}

	return result, nil
}

// Validate validates a transaction without submitting it using the new API
func (c *Client) Validate(ctx context.Context, envelope *messaging.Envelope, opts api.ValidateOptions) ([]*api.Submission, error) {
	if envelope == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	var result []*api.Submission
	err := c.executeWithRetry(ctx, func() error {
		var err error
		result, err = c.client.Validate(ctx, envelope, opts)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("validate failed: %w", err)
	}

	return result, nil
}

// Faucet requests credits from the faucet (testnet only) using the new API
func (c *Client) Faucet(ctx context.Context, account *url.URL, opts api.FaucetOptions) (*api.Submission, error) {
	if account == nil {
		return nil, fmt.Errorf("account URL cannot be nil")
	}

	var result *api.Submission
	err := c.executeWithRetry(ctx, func() error {
		var err error
		result, err = c.client.Faucet(ctx, account, opts)
		return err
	})

	if err != nil {
		return nil, fmt.Errorf("faucet request failed: %w", err)
	}

	return result, nil
}

// executeWithRetry executes a function with retry logic
func (c *Client) executeWithRetry(ctx context.Context, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retrying
			select {
			case <-time.After(c.config.RetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		// Don't retry certain types of errors
		if !c.shouldRetry(err) {
			break
		}
	}

	return lastErr
}

// shouldRetry determines if an error should trigger a retry
func (c *Client) shouldRetry(err error) bool {
	// Implement retry logic based on error type
	// For now, retry all errors except context cancellation
	select {
	case <-context.Background().Done():
		return false
	default:
		return true
	}
}

// Close closes the client and releases resources
func (c *Client) Close() error {
	// Close underlying connections if needed
	return nil
}

// GetEndpoint returns the client endpoint
func (c *Client) GetEndpoint() string {
	return c.endpoint
}

// SetTimeout updates the client timeout
func (c *Client) SetTimeout(timeout time.Duration) {
	c.config.Timeout = timeout
}

// SetMaxRetries updates the maximum retry count
func (c *Client) SetMaxRetries(maxRetries int) {
	c.config.MaxRetries = maxRetries
}

// SetRetryDelay updates the retry delay
func (c *Client) SetRetryDelay(delay time.Duration) {
	c.config.RetryDelay = delay
}

// IsHealthy checks if the client connection is healthy
func (c *Client) IsHealthy(ctx context.Context) error {
	// Simple health check by querying network status
	_, err := c.QueryNetworkStatus(ctx, api.NetworkStatusOptions{})
	return err
}

// SubmitEnvelope signs (if signer present) and submits an envelope
func (c *Client) SubmitEnvelope(ctx context.Context, env *build.TransactionBuilder) (string, error) {
	return c.SubmitEnvelopeForContract(ctx, env, nil)
}

// SubmitEnvelopeForContract signs and submits an envelope using contract-specific signer selection
func (c *Client) SubmitEnvelopeForContract(ctx context.Context, env *build.TransactionBuilder, contractURL *url.URL) (string, error) {
	if env == nil {
		return "", fmt.Errorf("envelope cannot be nil")
	}

	// TODO: Implement proper envelope signing and submission for new API
	_ = ctx
	_ = contractURL
	return "", fmt.Errorf("SubmitEnvelopeForContract not yet implemented for new API")
}