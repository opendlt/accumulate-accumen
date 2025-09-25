package l0api

import (
	"context"
	"fmt"
	"time"

	"gitlab.com/accumulatenetwork/accumulate/pkg/api/v3"
	"gitlab.com/accumulatenetwork/accumulate/pkg/url"
	"gitlab.com/accumulatenetwork/accumulate/protocol"
)

// Querier provides methods to query the Accumulate L0 network
type Querier struct {
	client *Client
	config *QueryConfig
}

// QueryConfig defines configuration for querying operations
type QueryConfig struct {
	// Maximum number of records to return in a single query
	MaxRecords int
	// Default timeout for queries
	Timeout time.Duration
	// Whether to include pending transactions
	IncludePending bool
}

// DefaultQueryConfig returns a default query configuration
func DefaultQueryConfig() *QueryConfig {
	return &QueryConfig{
		MaxRecords:     100,
		Timeout:        30 * time.Second,
		IncludePending: false,
	}
}

// NewQuerier creates a new Accumulate L0 querier
func NewQuerier(client *Client, config *QueryConfig) *Querier {
	if config == nil {
		config = DefaultQueryConfig()
	}

	return &Querier{
		client: client,
		config: config,
	}
}

// QueryAccount queries account information by URL
func (q *Querier) QueryAccount(ctx context.Context, accountURL *url.URL) (*api.AccountRecord, error) {
	if accountURL == nil {
		return nil, fmt.Errorf("account URL cannot be nil")
	}

	req := &api.GeneralQuery{
		Url: accountURL,
	}

	resp, err := q.client.Query(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query account %s: %w", accountURL, err)
	}

	account, ok := resp.Record.(*api.AccountRecord)
	if !ok {
		return nil, fmt.Errorf("unexpected record type for account %s", accountURL)
	}

	return account, nil
}

// QueryTransaction queries transaction information by hash
func (q *Querier) QueryTransaction(ctx context.Context, txHash []byte) (*api.TransactionRecord, error) {
	if len(txHash) == 0 {
		return nil, fmt.Errorf("transaction hash cannot be empty")
	}

	req := &api.TransactionQuery{
		TxHash: txHash,
	}

	resp, err := q.client.QueryTransaction(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query transaction %x: %w", txHash, err)
	}

	return resp, nil
}

// QueryDirectory queries directory information by URL
func (q *Querier) QueryDirectory(ctx context.Context, directoryURL *url.URL) (*api.DirectoryRecord, error) {
	if directoryURL == nil {
		return nil, fmt.Errorf("directory URL cannot be nil")
	}

	req := &api.GeneralQuery{
		Url: directoryURL,
		Range: &api.RangeOptions{
			Count: uint64(q.config.MaxRecords),
		},
	}

	resp, err := q.client.Query(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query directory %s: %w", directoryURL, err)
	}

	directory, ok := resp.Record.(*api.DirectoryRecord)
	if !ok {
		return nil, fmt.Errorf("unexpected record type for directory %s", directoryURL)
	}

	return directory, nil
}

// QueryChain queries chain information by URL
func (q *Querier) QueryChain(ctx context.Context, chainURL *url.URL, start, count uint64) (*api.ChainRecord, error) {
	if chainURL == nil {
		return nil, fmt.Errorf("chain URL cannot be nil")
	}

	req := &api.ChainQuery{
		Url: chainURL,
		Range: &api.RangeOptions{
			Start: start,
			Count: count,
		},
	}

	resp, err := q.client.QueryChain(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query chain %s: %w", chainURL, err)
	}

	return resp, nil
}

// QueryPendingTransactions queries pending transactions for an account
func (q *Querier) QueryPendingTransactions(ctx context.Context, accountURL *url.URL) ([]*api.TransactionRecord, error) {
	if accountURL == nil {
		return nil, fmt.Errorf("account URL cannot be nil")
	}

	req := &api.PendingQuery{
		Url: accountURL,
		Range: &api.RangeOptions{
			Count: uint64(q.config.MaxRecords),
		},
	}

	resp, err := q.client.QueryPending(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query pending transactions for %s: %w", accountURL, err)
	}

	return resp.Records, nil
}

// QueryNetworkStatus queries the network status information
func (q *Querier) QueryNetworkStatus(ctx context.Context, partition string) (*api.NetworkStatus, error) {
	req := &api.NetworkStatusQuery{
		Partition: partition,
	}

	resp, err := q.client.QueryNetworkStatus(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to query network status for partition %s: %w", partition, err)
	}

	return resp, nil
}

// QueryTokenAccount queries token account information
func (q *Querier) QueryTokenAccount(ctx context.Context, tokenURL *url.URL) (*protocol.TokenAccount, error) {
	record, err := q.QueryAccount(ctx, tokenURL)
	if err != nil {
		return nil, err
	}

	tokenAccount, ok := record.Account.(*protocol.TokenAccount)
	if !ok {
		return nil, fmt.Errorf("account %s is not a token account", tokenURL)
	}

	return tokenAccount, nil
}

// QueryDataAccount queries data account information
func (q *Querier) QueryDataAccount(ctx context.Context, dataURL *url.URL) (*protocol.DataAccount, error) {
	record, err := q.QueryAccount(ctx, dataURL)
	if err != nil {
		return nil, err
	}

	dataAccount, ok := record.Account.(*protocol.DataAccount)
	if !ok {
		return nil, fmt.Errorf("account %s is not a data account", dataURL)
	}

	return dataAccount, nil
}

// QueryKeyBook queries key book information
func (q *Querier) QueryKeyBook(ctx context.Context, keyBookURL *url.URL) (*protocol.KeyBook, error) {
	record, err := q.QueryAccount(ctx, keyURL)
	if err != nil {
		return nil, err
	}

	keyBook, ok := record.Account.(*protocol.KeyBook)
	if !ok {
		return nil, fmt.Errorf("account %s is not a key book", keyBookURL)
	}

	return keyBook, nil
}

// QueryCredits queries credit balance for an account
func (q *Querier) QueryCredits(ctx context.Context, accountURL *url.URL) (uint64, error) {
	record, err := q.QueryAccount(ctx, accountURL)
	if err != nil {
		return 0, err
	}

	// Extract credit balance based on account type
	switch acc := record.Account.(type) {
	case *protocol.LiteTokenAccount:
		return acc.CreditBalance, nil
	case *protocol.TokenAccount:
		return acc.CreditBalance, nil
	default:
		return 0, fmt.Errorf("account %s does not have a credit balance", accountURL)
	}
}

// ValidateURL validates an Accumulate URL format
func (q *Querier) ValidateURL(urlStr string) (*url.URL, error) {
	accURL, err := url.Parse(urlStr)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}

	return accURL, nil
}