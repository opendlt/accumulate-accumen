package outputs

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v3"
	"gitlab.com/accumulatenetwork/accumulate/exp/apiutil"
	"gitlab.com/accumulatenetwork/accumulate/pkg/build"
)

// OutboxItem represents a queued envelope for submission
type OutboxItem struct {
	ID          string    `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	Tries       int       `json:"tries"`
	NextAttempt time.Time `json:"next_attempt"`
	EnvBytes    []byte    `json:"env_bytes"`
	OpSummary   string    `json:"op_summary"`
}

// Outbox manages a persistent queue of envelopes for submission
type Outbox struct {
	db *badger.DB
}

// NewOutbox creates a new outbox with Badger storage
func NewOutbox(dbPath string) (*Outbox, error) {
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable badger logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger database: %w", err)
	}

	return &Outbox{db: db}, nil
}

// Close closes the outbox database
func (o *Outbox) Close() error {
	return o.db.Close()
}

// Enqueue adds an envelope to the submission queue
func (o *Outbox) Enqueue(env *build.EnvelopeBuilder) (string, error) {
	// Build the envelope
	envelope, err := env.Done()
	if err != nil {
		return "", fmt.Errorf("failed to build envelope: %w", err)
	}

	// Serialize envelope
	envBytes, err := envelope.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("failed to marshal envelope: %w", err)
	}

	// Generate unique ID
	id := generateID()

	// Create operation summary
	opSummary := createOpSummary(envelope)

	// Create outbox item
	item := &OutboxItem{
		ID:          id,
		CreatedAt:   time.Now(),
		Tries:       0,
		NextAttempt: time.Now(), // Ready immediately
		EnvBytes:    envBytes,
		OpSummary:   opSummary,
	}

	// Store in database
	return id, o.storeItem(item)
}

// DequeueReady retrieves items that are ready for submission
func (o *Outbox) DequeueReady(now time.Time, limit int) ([]*OutboxItem, error) {
	var items []*OutboxItem

	err := o.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchSize = limit
		it := txn.NewIterator(opts)
		defer it.Close()

		count := 0
		prefix := []byte("outbox:")

		for it.Seek(prefix); it.ValidForPrefix(prefix) && count < limit; it.Next() {
			item := it.Item()

			var outboxItem OutboxItem
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &outboxItem)
			})

			if err != nil {
				continue // Skip corrupted items
			}

			// Check if item is ready for processing
			if outboxItem.NextAttempt.Before(now) || outboxItem.NextAttempt.Equal(now) {
				items = append(items, &outboxItem)
				count++
			}
		}

		return nil
	})

	return items, err
}

// MarkDone removes an item from the queue after successful submission
func (o *Outbox) MarkDone(id string) error {
	return o.db.Update(func(txn *badger.Txn) error {
		key := []byte("outbox:" + id)
		return txn.Delete(key)
	})
}

// MarkRetry updates an item for retry with backoff
func (o *Outbox) MarkRetry(id string, backoff time.Duration) error {
	return o.db.Update(func(txn *badger.Txn) error {
		key := []byte("outbox:" + id)

		// Get current item
		item, err := txn.Get(key)
		if err != nil {
			return fmt.Errorf("item not found: %w", err)
		}

		var outboxItem OutboxItem
		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, &outboxItem)
		})
		if err != nil {
			return fmt.Errorf("failed to unmarshal item: %w", err)
		}

		// Update retry information
		outboxItem.Tries++
		outboxItem.NextAttempt = time.Now().Add(backoff)

		// Store updated item
		data, err := json.Marshal(&outboxItem)
		if err != nil {
			return fmt.Errorf("failed to marshal updated item: %w", err)
		}

		return txn.Set(key, data)
	})
}

// GetStats returns statistics about the outbox
func (o *Outbox) GetStats() (*OutboxStats, error) {
	stats := &OutboxStats{}

	err := o.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // Only need keys for counting
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("outbox:")
		now := time.Now()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			stats.TotalItems++

			// Get item to check if ready
			item := it.Item()
			var outboxItem OutboxItem
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &outboxItem)
			})

			if err != nil {
				continue
			}

			if outboxItem.NextAttempt.Before(now) || outboxItem.NextAttempt.Equal(now) {
				stats.ReadyItems++
			}

			if outboxItem.Tries > 0 {
				stats.RetryItems++
			}
		}

		return nil
	})

	return stats, err
}

// OutboxStats contains statistics about the outbox
type OutboxStats struct {
	TotalItems int `json:"total_items"`
	ReadyItems int `json:"ready_items"`
	RetryItems int `json:"retry_items"`
}

// storeItem stores an outbox item in the database
func (o *Outbox) storeItem(item *OutboxItem) error {
	return o.db.Update(func(txn *badger.Txn) error {
		key := []byte("outbox:" + item.ID)

		data, err := json.Marshal(item)
		if err != nil {
			return fmt.Errorf("failed to marshal item: %w", err)
		}

		return txn.Set(key, data)
	})
}

// generateID generates a unique identifier for outbox items
func generateID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomString(8))
}

// randomString generates a random string of specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[time.Now().UnixNano()%int64(len(charset))]
	}
	return string(b)
}

// createOpSummary creates a human-readable summary of the envelope operations
func createOpSummary(envelope *apiutil.Envelope) string {
	if envelope == nil || len(envelope.Transaction) == 0 {
		return "empty envelope"
	}

	// Simple summary based on transaction type
	// In a real implementation, this would parse the transaction
	// and provide more detailed information
	return fmt.Sprintf("tx with %d bytes", len(envelope.Transaction))
}

// Cleanup performs maintenance operations on the outbox
func (o *Outbox) Cleanup(maxAge time.Duration) error {
	cutoff := time.Now().Add(-maxAge)

	return o.db.Update(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("outbox:")
		var keysToDelete [][]byte

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()

			var outboxItem OutboxItem
			err := item.Value(func(val []byte) error {
				return json.Unmarshal(val, &outboxItem)
			})

			if err != nil {
				// Delete corrupted items
				keysToDelete = append(keysToDelete, item.KeyCopy(nil))
				continue
			}

			// Delete old items that have been retrying for too long
			if outboxItem.CreatedAt.Before(cutoff) && outboxItem.Tries > 10 {
				keysToDelete = append(keysToDelete, item.KeyCopy(nil))
			}
		}

		// Delete marked keys
		for _, key := range keysToDelete {
			if err := txn.Delete(key); err != nil {
				return fmt.Errorf("failed to delete key %s: %w", string(key), err)
			}
		}

		return nil
	})
}