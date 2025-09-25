package sequencer

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/opendlt/accumulate-accumen/internal/logz"
	"github.com/opendlt/accumulate-accumen/types/l1"
)

// Badger key prefixes for mempool storage
var (
	prefixPending = []byte("mp:pending:")    // Pending transactions queue
	prefixIndex   = []byte("mp:index:")      // Hash to sequence number index
	prefixSeq     = []byte("mp:seq")         // Current sequence counter
)

// PersistentMempool provides a Badger-backed persistent transaction queue
type PersistentMempool struct {
	db     *badger.DB
	logger *logz.Logger
}

// TxRecord represents a transaction record with metadata
type TxRecord struct {
	Tx        l1.Tx `json:"tx" cbor:"tx"`
	Hash      []byte `json:"hash" cbor:"hash"`
	Sequence  uint64 `json:"sequence" cbor:"sequence"`
	Timestamp int64  `json:"timestamp" cbor:"timestamp"`
}

// NewPersistentMempool creates a new persistent mempool with Badger storage
func NewPersistentMempool(dbPath string) (*PersistentMempool, error) {
	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil // Disable badger's default logging to avoid spam

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open Badger database at %s: %w", dbPath, err)
	}

	mp := &PersistentMempool{
		db:     db,
		logger: logz.New(logz.INFO, "persistent-mempool"),
	}

	return mp, nil
}

// Close closes the persistent mempool and releases resources
func (mp *PersistentMempool) Close() error {
	mp.logger.Info("Closing persistent mempool")
	return mp.db.Close()
}

// Add adds a transaction to the persistent mempool and returns its hash
func (mp *PersistentMempool) Add(tx l1.Tx) ([32]byte, error) {
	// Validate transaction first
	if err := tx.Validate(); err != nil {
		return [32]byte{}, fmt.Errorf("invalid transaction: %w", err)
	}

	hash := tx.Hash()

	// Check if transaction already exists
	exists, err := mp.exists(hash[:])
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to check transaction existence: %w", err)
	}
	if exists {
		return hash, fmt.Errorf("transaction already exists in mempool")
	}

	// Get next sequence number and store transaction
	err = mp.db.Update(func(txn *badger.Txn) error {
		// Get and increment sequence number
		seq, err := mp.getNextSequence(txn)
		if err != nil {
			return fmt.Errorf("failed to get sequence number: %w", err)
		}

		// Create transaction record
		record := TxRecord{
			Tx:        tx,
			Hash:      hash[:],
			Sequence:  seq,
			Timestamp: time.Now().UnixNano(),
		}

		// Serialize record using CBOR
		data, err := record.marshal()
		if err != nil {
			return fmt.Errorf("failed to marshal transaction record: %w", err)
		}

		// Store in pending queue with sequence as key
		pendingKey := mp.makePendingKey(seq)
		if err := txn.Set(pendingKey, data); err != nil {
			return fmt.Errorf("failed to store transaction in pending queue: %w", err)
		}

		// Store hash index for fast lookup
		indexKey := mp.makeIndexKey(hash[:])
		seqBytes := make([]byte, 8)
		binary.BigEndian.PutUint64(seqBytes, seq)
		if err := txn.Set(indexKey, seqBytes); err != nil {
			return fmt.Errorf("failed to store transaction index: %w", err)
		}

		mp.logger.Debug("Added transaction to persistent mempool: hash=%x seq=%d", hash[:8], seq)
		return nil
	})

	if err != nil {
		return [32]byte{}, err
	}

	return hash, nil
}

// Next retrieves the next n pending transactions from the queue
func (mp *PersistentMempool) Next(n int) ([]l1.Tx, error) {
	if n <= 0 {
		return nil, fmt.Errorf("count must be positive, got %d", n)
	}

	var transactions []l1.Tx

	err := mp.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefixPending
		it := txn.NewIterator(opts)
		defer it.Close()

		// Start from the beginning and collect up to n transactions
		for it.Rewind(); it.Valid() && len(transactions) < n; it.Next() {
			item := it.Item()

			// Get transaction record
			var record TxRecord
			err := item.Value(func(val []byte) error {
				return record.unmarshal(val)
			})
			if err != nil {
				mp.logger.Info("Warning: Failed to unmarshal transaction record: %v", err)
				continue
			}

			transactions = append(transactions, record.Tx)
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve transactions from mempool: %w", err)
	}

	mp.logger.Debug("Retrieved %d transactions from persistent mempool", len(transactions))
	return transactions, nil
}

// Remove removes a transaction from the persistent mempool by hash
func (mp *PersistentMempool) Remove(hash [32]byte) error {
	return mp.db.Update(func(txn *badger.Txn) error {
		// Get sequence number from index
		indexKey := mp.makeIndexKey(hash[:])
		item, err := txn.Get(indexKey)
		if err != nil {
			if err == badger.ErrKeyNotFound {
				return fmt.Errorf("transaction not found in mempool")
			}
			return fmt.Errorf("failed to lookup transaction index: %w", err)
		}

		var seq uint64
		err = item.Value(func(val []byte) error {
			if len(val) != 8 {
				return fmt.Errorf("invalid sequence data length: %d", len(val))
			}
			seq = binary.BigEndian.Uint64(val)
			return nil
		})
		if err != nil {
			return fmt.Errorf("failed to read sequence number: %w", err)
		}

		// Remove from pending queue
		pendingKey := mp.makePendingKey(seq)
		if err := txn.Delete(pendingKey); err != nil {
			return fmt.Errorf("failed to remove transaction from pending queue: %w", err)
		}

		// Remove from index
		if err := txn.Delete(indexKey); err != nil {
			return fmt.Errorf("failed to remove transaction from index: %w", err)
		}

		mp.logger.Debug("Removed transaction from persistent mempool: hash=%x seq=%d", hash[:8], seq)
		return nil
	})
}

// Replay iterates through all pending transactions and calls the provided function
func (mp *PersistentMempool) Replay(fn func(l1.Tx) error) error {
	mp.logger.Info("Starting mempool replay")

	var count int
	var errors int

	err := mp.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefixPending
		it := txn.NewIterator(opts)
		defer it.Close()

		// Iterate through all pending transactions in sequence order
		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			// Get transaction record
			var record TxRecord
			err := item.Value(func(val []byte) error {
				return record.unmarshal(val)
			})
			if err != nil {
				mp.logger.Info("Warning: Failed to unmarshal transaction record during replay: %v", err)
				errors++
				continue
			}

			// Call replay function
			if err := fn(record.Tx); err != nil {
				mp.logger.Info("Warning: Replay function failed for transaction %x: %v",
					record.Hash[:8], err)
				errors++
				continue
			}

			count++
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to replay mempool transactions: %w", err)
	}

	mp.logger.Info("Mempool replay completed: replayed=%d errors=%d", count, errors)
	return nil
}

// Count returns the number of pending transactions in the mempool
func (mp *PersistentMempool) Count() (int, error) {
	var count int

	err := mp.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefixPending
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			count++
		}

		return nil
	})

	return count, err
}

// exists checks if a transaction with the given hash exists in the mempool
func (mp *PersistentMempool) exists(hash []byte) (bool, error) {
	var exists bool

	err := mp.db.View(func(txn *badger.Txn) error {
		indexKey := mp.makeIndexKey(hash)
		_, err := txn.Get(indexKey)
		if err == nil {
			exists = true
		} else if err != badger.ErrKeyNotFound {
			return err
		}
		return nil
	})

	return exists, err
}

// getNextSequence gets and increments the sequence counter
func (mp *PersistentMempool) getNextSequence(txn *badger.Txn) (uint64, error) {
	// Get current sequence
	var seq uint64
	item, err := txn.Get(prefixSeq)
	if err != nil {
		if err == badger.ErrKeyNotFound {
			seq = 0 // Start from 0 if no sequence exists
		} else {
			return 0, err
		}
	} else {
		err = item.Value(func(val []byte) error {
			if len(val) != 8 {
				return fmt.Errorf("invalid sequence data length: %d", len(val))
			}
			seq = binary.BigEndian.Uint64(val)
			return nil
		})
		if err != nil {
			return 0, err
		}
	}

	// Increment and store
	nextSeq := seq + 1
	seqBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBytes, nextSeq)

	if err := txn.Set(prefixSeq, seqBytes); err != nil {
		return 0, err
	}

	return nextSeq, nil
}

// makePendingKey creates a key for the pending queue
func (mp *PersistentMempool) makePendingKey(seq uint64) []byte {
	key := make([]byte, len(prefixPending)+8)
	copy(key, prefixPending)
	binary.BigEndian.PutUint64(key[len(prefixPending):], seq)
	return key
}

// makeIndexKey creates a key for the hash index
func (mp *PersistentMempool) makeIndexKey(hash []byte) []byte {
	key := make([]byte, len(prefixIndex)+len(hash))
	copy(key, prefixIndex)
	copy(key[len(prefixIndex):], hash)
	return key
}

// marshal serializes a transaction record using CBOR
func (record *TxRecord) marshal() ([]byte, error) {
	return record.Tx.MarshalCBOR()
}

// unmarshal deserializes a transaction record from CBOR data
func (record *TxRecord) unmarshal(data []byte) error {
	return record.Tx.UnmarshalCBOR(data)
}