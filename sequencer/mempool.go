package sequencer

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/opendlt/accumulate-accumen/bridge/pricing"
)

// Transaction represents a transaction in the mempool
type Transaction struct {
	ID         string    `json:"id"`
	From       string    `json:"from"`
	To         string    `json:"to"`
	Data       []byte    `json:"data"`
	GasLimit   uint64    `json:"gas_limit"`
	GasPrice   uint64    `json:"gas_price"`
	Nonce      uint64    `json:"nonce"`
	Signature  []byte    `json:"signature"`
	Hash       []byte    `json:"hash"`
	Size       uint64    `json:"size"`
	ReceivedAt time.Time `json:"received_at"`
	Priority   uint64    `json:"priority"`
}

// Mempool manages pending transactions for the sequencer
type Mempool struct {
	mu        sync.RWMutex
	config    MempoolConfig
	creditMgr *pricing.CreditManager

	// Transaction storage
	transactions map[string]*Transaction   // by tx hash
	byAccount    map[string][]*Transaction // by account address
	byPriority   []*Transaction            // sorted by priority

	// Metrics
	totalAdded   uint64
	totalRemoved uint64
	totalSize    uint64

	// Background processing
	running       bool
	stopChan      chan struct{}
	cleanupTicker *time.Ticker
}

// MempoolStats contains mempool statistics
type MempoolStats struct {
	TotalTransactions int            `json:"total_transactions"`
	TotalSize         uint64         `json:"total_size"`
	AverageGasPrice   uint64         `json:"average_gas_price"`
	PendingByAccount  map[string]int `json:"pending_by_account"`
	TotalAdded        uint64         `json:"total_added"`
	TotalRemoved      uint64         `json:"total_removed"`
	OldestTransaction time.Time      `json:"oldest_transaction"`
}

// NewMempool creates a new mempool instance
func NewMempool(config MempoolConfig, creditMgr *pricing.CreditManager) *Mempool {
	return &Mempool{
		config:       config,
		creditMgr:    creditMgr,
		transactions: make(map[string]*Transaction),
		byAccount:    make(map[string][]*Transaction),
		byPriority:   make([]*Transaction, 0),
		stopChan:     make(chan struct{}),
	}
}

// Start starts the mempool background processing
func (mp *Mempool) Start(ctx context.Context) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if mp.running {
		return fmt.Errorf("mempool is already running")
	}

	mp.running = true
	mp.cleanupTicker = time.NewTicker(time.Minute)

	// Start cleanup routine
	go mp.cleanupRoutine(ctx)

	return nil
}

// Stop stops the mempool background processing
func (mp *Mempool) Stop() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if !mp.running {
		return nil
	}

	mp.running = false
	close(mp.stopChan)

	if mp.cleanupTicker != nil {
		mp.cleanupTicker.Stop()
	}

	return nil
}

// AddTransaction adds a transaction to the mempool
func (mp *Mempool) AddTransaction(tx *Transaction) error {
	if tx == nil {
		return fmt.Errorf("transaction cannot be nil")
	}

	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Check if transaction already exists
	if _, exists := mp.transactions[tx.ID]; exists {
		return fmt.Errorf("transaction %s already exists", tx.ID)
	}

	// Validate transaction
	if err := mp.validateTransaction(tx); err != nil {
		return fmt.Errorf("transaction validation failed: %w", err)
	}

	// Check mempool limits
	if err := mp.checkLimits(tx); err != nil {
		return fmt.Errorf("mempool limits exceeded: %w", err)
	}

	// Calculate transaction priority
	tx.Priority = mp.calculatePriority(tx)

	// Add to transaction storage
	mp.transactions[tx.ID] = tx
	mp.byAccount[tx.From] = append(mp.byAccount[tx.From], tx)
	mp.totalSize += tx.Size
	mp.totalAdded++

	// Insert into priority-sorted list
	mp.insertByPriority(tx)

	return nil
}

// RemoveTransaction removes a transaction from the mempool
func (mp *Mempool) RemoveTransaction(txID string) error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	tx, exists := mp.transactions[txID]
	if !exists {
		return fmt.Errorf("transaction %s not found", txID)
	}

	// Remove from main storage
	delete(mp.transactions, txID)
	mp.totalSize -= tx.Size
	mp.totalRemoved++

	// Remove from account index
	accountTxs := mp.byAccount[tx.From]
	for i, accountTx := range accountTxs {
		if accountTx.ID == txID {
			mp.byAccount[tx.From] = append(accountTxs[:i], accountTxs[i+1:]...)
			break
		}
	}

	// Remove from priority list
	for i, priorityTx := range mp.byPriority {
		if priorityTx.ID == txID {
			mp.byPriority = append(mp.byPriority[:i], mp.byPriority[i+1:]...)
			break
		}
	}

	// Clean up empty account entries
	if len(mp.byAccount[tx.From]) == 0 {
		delete(mp.byAccount, tx.From)
	}

	return nil
}

// GetTransaction retrieves a transaction by ID
func (mp *Mempool) GetTransaction(txID string) (*Transaction, error) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	tx, exists := mp.transactions[txID]
	if !exists {
		return nil, fmt.Errorf("transaction %s not found", txID)
	}

	return tx, nil
}

// GetTransactionsByAccount retrieves all transactions for an account
func (mp *Mempool) GetTransactionsByAccount(account string) ([]*Transaction, error) {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	txs, exists := mp.byAccount[account]
	if !exists {
		return []*Transaction{}, nil
	}

	// Return a copy to prevent external modification
	result := make([]*Transaction, len(txs))
	copy(result, txs)

	return result, nil
}

// GetTopTransactions retrieves the top N transactions by priority
func (mp *Mempool) GetTopTransactions(count int) []*Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if count <= 0 {
		return []*Transaction{}
	}

	if count > len(mp.byPriority) {
		count = len(mp.byPriority)
	}

	// Return a copy of the top transactions
	result := make([]*Transaction, count)
	copy(result, mp.byPriority[:count])

	return result
}

// GetAllTransactions retrieves all transactions in the mempool
func (mp *Mempool) GetAllTransactions() []*Transaction {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	result := make([]*Transaction, 0, len(mp.transactions))
	for _, tx := range mp.transactions {
		result = append(result, tx)
	}

	return result
}

// GetStats returns mempool statistics
func (mp *Mempool) GetStats() *MempoolStats {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	stats := &MempoolStats{
		TotalTransactions: len(mp.transactions),
		TotalSize:         mp.totalSize,
		PendingByAccount:  make(map[string]int),
		TotalAdded:        mp.totalAdded,
		TotalRemoved:      mp.totalRemoved,
	}

	// Calculate average gas price and oldest transaction
	var totalGasPrice uint64
	var oldestTime time.Time = time.Now()

	for account, txs := range mp.byAccount {
		stats.PendingByAccount[account] = len(txs)
	}

	for _, tx := range mp.transactions {
		totalGasPrice += tx.GasPrice
		if tx.ReceivedAt.Before(oldestTime) {
			oldestTime = tx.ReceivedAt
		}
	}

	if len(mp.transactions) > 0 {
		stats.AverageGasPrice = totalGasPrice / uint64(len(mp.transactions))
		stats.OldestTransaction = oldestTime
	}

	return stats
}

// Size returns the current number of transactions in the mempool
func (mp *Mempool) Size() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.transactions)
}

// validateTransaction validates a transaction before adding to mempool
func (mp *Mempool) validateTransaction(tx *Transaction) error {
	// Check transaction size
	if tx.Size > mp.config.MaxTxSize {
		return fmt.Errorf("transaction size %d exceeds limit %d", tx.Size, mp.config.MaxTxSize)
	}

	// Check gas price limit
	if tx.GasPrice < mp.config.PriceLimit {
		return fmt.Errorf("gas price %d below limit %d", tx.GasPrice, mp.config.PriceLimit)
	}

	// Check required fields
	if tx.From == "" {
		return fmt.Errorf("transaction missing sender")
	}

	if len(tx.Hash) == 0 {
		return fmt.Errorf("transaction missing hash")
	}

	if len(tx.Signature) == 0 {
		return fmt.Errorf("transaction missing signature")
	}

	return nil
}

// checkLimits checks if adding the transaction would exceed mempool limits
func (mp *Mempool) checkLimits(tx *Transaction) error {
	// Check global size limit
	if len(mp.transactions) >= mp.config.MaxSize {
		return fmt.Errorf("mempool at capacity: %d transactions", len(mp.transactions))
	}

	// Check account limit
	accountTxs := mp.byAccount[tx.From]
	if len(accountTxs) >= mp.config.AccountLimit {
		return fmt.Errorf("account %s has %d pending transactions (limit: %d)",
			tx.From, len(accountTxs), mp.config.AccountLimit)
	}

	// Check global transaction limit
	if len(mp.transactions) >= mp.config.GlobalLimit {
		return fmt.Errorf("global transaction limit %d reached", mp.config.GlobalLimit)
	}

	return nil
}

// calculatePriority calculates transaction priority for ordering
func (mp *Mempool) calculatePriority(tx *Transaction) uint64 {
	if !mp.config.EnablePriority {
		return tx.GasPrice
	}

	// Priority = gas_price * gas_limit / transaction_size
	// This favors higher paying and more gas-efficient transactions
	priority := tx.GasPrice * tx.GasLimit / tx.Size

	// Apply time-based boost for older transactions
	age := time.Since(tx.ReceivedAt)
	ageBoost := uint64(age.Minutes()) // 1 point per minute

	return priority + ageBoost
}

// insertByPriority inserts a transaction into the priority-sorted list
func (mp *Mempool) insertByPriority(tx *Transaction) {
	// Find insertion point using binary search
	i := sort.Search(len(mp.byPriority), func(i int) bool {
		return mp.byPriority[i].Priority < tx.Priority
	})

	// Insert at position i
	mp.byPriority = append(mp.byPriority, nil)
	copy(mp.byPriority[i+1:], mp.byPriority[i:])
	mp.byPriority[i] = tx
}

// cleanupRoutine periodically removes expired transactions
func (mp *Mempool) cleanupRoutine(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-mp.stopChan:
			return
		case <-mp.cleanupTicker.C:
			mp.cleanup()
		}
	}
}

// cleanup removes expired and low-priority transactions
func (mp *Mempool) cleanup() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	now := time.Now()
	expiredTxs := make([]string, 0)

	// Find expired transactions
	for txID, tx := range mp.transactions {
		if now.Sub(tx.ReceivedAt) > mp.config.TTL {
			expiredTxs = append(expiredTxs, txID)
		}
	}

	// Remove expired transactions
	for _, txID := range expiredTxs {
		tx := mp.transactions[txID]

		// Remove from main storage
		delete(mp.transactions, txID)
		mp.totalSize -= tx.Size
		mp.totalRemoved++

		// Remove from account index
		accountTxs := mp.byAccount[tx.From]
		for i, accountTx := range accountTxs {
			if accountTx.ID == txID {
				mp.byAccount[tx.From] = append(accountTxs[:i], accountTxs[i+1:]...)
				break
			}
		}

		// Remove from priority list
		for i, priorityTx := range mp.byPriority {
			if priorityTx.ID == txID {
				mp.byPriority = append(mp.byPriority[:i], mp.byPriority[i+1:]...)
				break
			}
		}
	}

	// Clean up empty account entries
	for account, txs := range mp.byAccount {
		if len(txs) == 0 {
			delete(mp.byAccount, account)
		}
	}
}

// Flush removes all transactions from the mempool
func (mp *Mempool) Flush() error {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.transactions = make(map[string]*Transaction)
	mp.byAccount = make(map[string][]*Transaction)
	mp.byPriority = make([]*Transaction, 0)
	mp.totalSize = 0

	return nil
}

// HasTransaction checks if a transaction exists in the mempool
func (mp *Mempool) HasTransaction(txID string) bool {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	_, exists := mp.transactions[txID]
	return exists
}

// UpdatePriorities recalculates priorities for all transactions
func (mp *Mempool) UpdatePriorities() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Recalculate priorities
	for _, tx := range mp.transactions {
		tx.Priority = mp.calculatePriority(tx)
	}

	// Re-sort priority list
	sort.Slice(mp.byPriority, func(i, j int) bool {
		return mp.byPriority[i].Priority > mp.byPriority[j].Priority
	})
}
