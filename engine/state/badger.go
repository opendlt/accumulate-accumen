package state

import (
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
)

// BadgerStore provides persistent key-value storage using Badger database
type BadgerStore struct {
	db *badger.DB
}

// NewBadgerStore creates a new BadgerStore instance with optimized settings for Windows
func NewBadgerStore(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path)

	// Optimize for Windows file I/O
	opts = opts.WithSyncWrites(false)          // Async writes for better performance
	opts = opts.WithCompression(options.None)  // Disable compression
	opts = opts.WithValueLogFileSize(16 << 20) // 16MB value log files
	opts = opts.WithNumMemtables(2)            // Reduce memory usage
	opts = opts.WithNumLevelZeroTables(2)      // Reduce file handles
	opts = opts.WithNumLevelZeroTablesStall(3)
	opts = opts.WithValueLogMaxEntries(100000) // Smaller value log entries
	opts = opts.WithLogger(nil)                // Disable badger logging

	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open badger database: %w", err)
	}

	store := &BadgerStore{
		db: db,
	}

	// Start background garbage collection
	go store.runGC()

	return store, nil
}

// Get retrieves a value by key
func (s *BadgerStore) Get(key []byte) ([]byte, error) {
	var value []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(key)
		if err != nil {
			return err
		}

		value, err = item.ValueCopy(nil)
		return err
	})

	if err == badger.ErrKeyNotFound {
		return nil, ErrKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", string(key), err)
	}

	return value, nil
}

// Set stores a key-value pair
func (s *BadgerStore) Set(key, value []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, value)
	})

	if err != nil {
		return fmt.Errorf("failed to set key %s: %w", string(key), err)
	}

	return nil
}

// Put is an alias for Set to match the interface
func (s *BadgerStore) Put(key, value []byte) error {
	return s.Set(key, value)
}

// Delete removes a key-value pair
func (s *BadgerStore) Delete(key []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(key)
	})

	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", string(key), err)
	}

	return nil
}

// Has checks if a key exists (alias for Exists)
func (s *BadgerStore) Has(key []byte) (bool, error) {
	return s.Exists(key)
}

// Exists checks if a key exists
func (s *BadgerStore) Exists(key []byte) (bool, error) {
	exists := false
	err := s.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)
		if err == badger.ErrKeyNotFound {
			return nil
		}
		if err != nil {
			return err
		}
		exists = true
		return nil
	})

	if err != nil {
		return false, fmt.Errorf("failed to check key %s: %w", string(key), err)
	}

	return exists, nil
}

// Iterator returns an iterator for keys with the given prefix
func (s *BadgerStore) Iterator(prefix []byte) (Iterator, error) {
	txn := s.db.NewTransaction(false) // Read-only transaction
	opts := badger.DefaultIteratorOptions
	opts.Prefix = prefix

	iter := txn.NewIterator(opts)
	iter.Rewind() // Position at first key

	return &badgerIterator{
		iter: iter,
		txn:  txn,
	}, nil
}

// Iterate calls the function for each key-value pair with the given prefix
func (s *BadgerStore) Iterate(fn func(key, value []byte) error) error {
	return s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		iter := txn.NewIterator(opts)
		defer iter.Close()

		for iter.Rewind(); iter.Valid(); iter.Next() {
			item := iter.Item()
			key := item.Key()

			value, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}

			if err := fn(key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// Close closes the database
func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// runGC runs periodic garbage collection on value logs
func (s *BadgerStore) runGC() {
	ticker := time.NewTicker(5 * time.Minute) // Every 5 minutes
	defer ticker.Stop()

	for range ticker.C {
		// Run GC with 0.5 discard ratio
		err := s.db.RunValueLogGC(0.5)
		if err != nil && err != badger.ErrNoRewrite {
			// Log error if we had logging enabled
			continue
		}
	}
}

// badgerIterator implements Iterator interface for Badger
type badgerIterator struct {
	iter *badger.Iterator
	txn  *badger.Txn
}

// Valid returns true if the iterator is positioned at a valid key-value pair
func (i *badgerIterator) Valid() bool {
	return i.iter.Valid()
}

// Next moves the iterator to the next key-value pair
func (i *badgerIterator) Next() bool {
	if !i.iter.Valid() {
		return false
	}
	i.iter.Next()
	return i.iter.Valid()
}

// Key returns the current key
func (i *badgerIterator) Key() []byte {
	if !i.iter.Valid() {
		return nil
	}
	return i.iter.Item().Key()
}

// Value returns the current value
func (i *badgerIterator) Value() []byte {
	if !i.iter.Valid() {
		return nil
	}
	value, err := i.iter.Item().ValueCopy(nil)
	if err != nil {
		return nil
	}
	return value
}

// Error returns any error that occurred during iteration
func (i *badgerIterator) Error() error {
	// badger iterators don't have error state like this
	return nil
}

// Close closes the iterator and transaction
func (i *badgerIterator) Close() error {
	i.iter.Close()
	i.txn.Discard()
	return nil
}
