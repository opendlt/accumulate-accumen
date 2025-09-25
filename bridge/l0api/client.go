package l0api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3/jsonrpc"
	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
	"gitlab.com/accumulatenetwork/accumulate/pkg/types/messaging"

	"github.com/opendlt/accumulate-accumen/internal/crypto/devsigner"
	"github.com/opendlt/accumulate-accumen/internal/crypto/signer"
)

// Client represents an Accumulate L0 API v3 client
type Client struct {
	endpoint string
	client   api.Querier
	submitter api.Submitter
	config   *ClientConfig
	signer   signer.Signer
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
	if config == nil {
		return nil, fmt.Errorf("client config cannot be nil")
	}

	if config.Endpoint == "" {
		return nil, fmt.Errorf("endpoint cannot be empty")
	}

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: config.Timeout,
	}

	// Create JSON-RPC client
	rpcClient := jsonrpc.NewClient(config.Endpoint, jsonrpc.WithHTTPClient(httpClient))

	// If no signer provided but SequencerKey is set, create legacy signer for backward compatibility
	if sig == nil && config.SequencerKey != "" {
		legacySigner, err := devsigner.NewFromHex(config.SequencerKey)
		if err != nil {
			return nil, fmt.Errorf("failed to create legacy signer from SequencerKey: %w", err)
		}
		sig = legacySigner.AsNewSigner()
	}

	return &Client{
		endpoint:  config.Endpoint,
		client:    rpcClient,
		submitter: rpcClient,
		config:    config,
		signer:    sig,
	}, nil
}

// Query executes a general query against the Accumulate network
func (c *Client) Query(ctx context.Context, req *api.GeneralQuery) (*api.GeneralQueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("query request cannot be nil")
	}

	var resp api.GeneralQueryResponse
	err := c.executeWithRetry(ctx, func() error {
		return c.client.Query(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	return &resp, nil
}

// QueryTransaction queries transaction information
func (c *Client) QueryTransaction(ctx context.Context, req *api.TransactionQuery) (*api.TransactionRecord, error) {
	if req == nil {
		return nil, fmt.Errorf("transaction query request cannot be nil")
	}

	var resp api.TransactionRecord
	err := c.executeWithRetry(ctx, func() error {
		return c.client.QueryTransaction(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("transaction query failed: %w", err)
	}

	return &resp, nil
}

// QueryChain queries chain information
func (c *Client) QueryChain(ctx context.Context, req *api.ChainQuery) (*api.ChainRecord, error) {
	if req == nil {
		return nil, fmt.Errorf("chain query request cannot be nil")
	}

	var resp api.ChainRecord
	err := c.executeWithRetry(ctx, func() error {
		return c.client.QueryChain(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("chain query failed: %w", err)
	}

	return &resp, nil
}

// QueryPending queries pending transactions
func (c *Client) QueryPending(ctx context.Context, req *api.PendingQuery) (*api.PendingQueryResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("pending query request cannot be nil")
	}

	var resp api.PendingQueryResponse
	err := c.executeWithRetry(ctx, func() error {
		return c.client.QueryPending(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("pending query failed: %w", err)
	}

	return &resp, nil
}

// QueryNetworkStatus queries network status information
func (c *Client) QueryNetworkStatus(ctx context.Context, req *api.NetworkStatusQuery) (*api.NetworkStatus, error) {
	if req == nil {
		return nil, fmt.Errorf("network status query request cannot be nil")
	}

	var resp api.NetworkStatus
	err := c.executeWithRetry(ctx, func() error {
		return c.client.QueryNetworkStatus(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("network status query failed: %w", err)
	}

	return &resp, nil
}

// Submit submits a transaction envelope to the network
func (c *Client) Submit(ctx context.Context, envelope *messaging.Envelope) (*api.SubmitResponse, error) {
	if envelope == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	req := &api.SubmitRequest{
		Envelope: envelope,
	}

	var resp api.SubmitResponse
	err := c.executeWithRetry(ctx, func() error {
		return c.submitter.Submit(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("submit failed: %w", err)
	}

	return &resp, nil
}

// Validate validates a transaction without submitting it
func (c *Client) Validate(ctx context.Context, envelope *messaging.Envelope) (*api.ValidateResponse, error) {
	if envelope == nil {
		return nil, fmt.Errorf("envelope cannot be nil")
	}

	req := &api.ValidateRequest{
		Envelope: envelope,
	}

	var resp api.ValidateResponse
	err := c.executeWithRetry(ctx, func() error {
		return c.submitter.Validate(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("validate failed: %w", err)
	}

	return &resp, nil
}

// Faucet requests credits from the faucet (testnet only)
func (c *Client) Faucet(ctx context.Context, req *api.FaucetRequest) (*api.FaucetResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("faucet request cannot be nil")
	}

	var resp api.FaucetResponse
	err := c.executeWithRetry(ctx, func() error {
		return c.submitter.Faucet(ctx, req, &resp)
	})

	if err != nil {
		return nil, fmt.Errorf("faucet request failed: %w", err)
	}

	return &resp, nil
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
	_, err := c.QueryNetworkStatus(ctx, &api.NetworkStatusQuery{})
	return err
}

// SubmitEnvelope signs (if signer present) and submits an envelope
func (c *Client) SubmitEnvelope(ctx context.Context, env *build.EnvelopeBuilder) (string, error) {
	if env == nil {
		return "", fmt.Errorf("envelope cannot be nil")
	}

	// Sign envelope if signer is configured
	if c.signer != nil {
		// Sign the envelope using the configured signer
		if err := c.signer.SignEnvelope(env); err != nil {
			return "", fmt.Errorf("failed to sign envelope: %w", err)
		}
	} else {
		// Check for legacy sequencer key configuration
		if c.config.SequencerKey != "" {
			// Create legacy development signer for backward compatibility
			legacySigner, err := devsigner.NewFromHex(c.config.SequencerKey)
			if err != nil {
				return "", fmt.Errorf("failed to create legacy signer from sequencer key: %w", err)
			}

			// Sign the envelope
			if err := legacySigner.Sign(env); err != nil {
				return "", fmt.Errorf("failed to sign envelope with legacy signer: %w", err)
			}
		} else {
			// No signing capability available
			return "", fmt.Errorf("no signer configured - envelope signing not available. " +
				"Either configure a signer via NewClientWithSigner or set sequencerKey in config for legacy support")
		}
	}

	// Build the final envelope
	envelope, err := env.Done()
	if err != nil {
		return "", fmt.Errorf("failed to build envelope: %w", err)
	}

	// Submit to network
	resp, err := c.Submit(ctx, envelope)
	if err != nil {
		return "", fmt.Errorf("failed to submit envelope: %w", err)
	}

	// Extract transaction ID from response
	if len(resp.TransactionHash) == 0 {
		return "", fmt.Errorf("no transaction hash returned from submission")
	}

	return resp.TransactionHash.String(), nil
}