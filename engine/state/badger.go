package state

import (
	"fmt"

	"github.com/dgraph-io/badger/v4"
)

// BadgerStore provides persistent key-value storage using Badger database
type BadgerStore struct {
	db *badger.DB
}

// NewBadgerStore creates a new BadgerStore instance with optimized settings for Windows
func NewBadgerStore(path string) (*BadgerStore, error) {
	opts := badger.DefaultOptions(path)

	// Optimize for Windows file I/O
	opts = opts.WithSyncWrites(false)      // Async writes for better performance
	opts = opts.WithCompression(badger.None) // Disable compression
	opts = opts.WithValueLogFileSize(16 << 20) // 16MB value log files
	opts = opts.WithNumMemtables(2)        // Reduce memory usage
	opts = opts.WithNumLevelZeroTables(2)  // Reduce file handles
	opts = opts.WithNumLevelZeroTablesStall(3)
	opts = opts.WithValueLogMaxEntries(100000) // Smaller value log entries
	opts = opts.WithLogger(nil)            // Disable badger logging

	// Enable value log garbage collection
	opts = opts.WithValueLogGC(true)

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
func (s *BadgerStore) Get(key string) ([]byte, error) {
	var value []byte
	err := s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
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
		return nil, fmt.Errorf("failed to get key %s: %w", key, err)
	}

	return value, nil
}

// Put stores a key-value pair
func (s *BadgerStore) Put(key string, value []byte) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), value)
	})

	if err != nil {
		return fmt.Errorf("failed to put key %s: %w", key, err)
	}

	return nil
}

// Delete removes a key-value pair
func (s *BadgerStore) Delete(key string) error {
	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})

	if err != nil {
		return fmt.Errorf("failed to delete key %s: %w", key, err)
	}

	return nil
}

// Has checks if a key exists
func (s *BadgerStore) Has(key string) (bool, error) {
	exists := false
	err := s.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get([]byte(key))
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
		return false, fmt.Errorf("failed to check key %s: %w", key, err)
	}

	return exists, nil
}

// Iterator returns an iterator for keys with the given prefix
func (s *BadgerStore) Iterator(prefix string) (Iterator, error) {
	txn := s.db.NewTransaction(false) // Read-only transaction
	opts := badger.DefaultIteratorOptions
	opts.Prefix = []byte(prefix)

	iter := txn.NewIterator(opts)

	return &badgerIterator{
		iter: iter,
		txn:  txn,
	}, nil
}

// Close closes the database
func (s *BadgerStore) Close() error {
	return s.db.Close()
}

// runGC runs periodic garbage collection on value logs
func (s *BadgerStore) runGC() {
	ticker := badger.NewTicker(5 * 60) // Every 5 minutes
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

// Key returns the current key
func (i *badgerIterator) Key() string {
	return string(i.iter.Item().Key())
}

// Value returns the current value
func (i *badgerIterator) Value() ([]byte, error) {
	return i.iter.Item().ValueCopy(nil)
}

// Next moves the iterator to the next key
func (i *badgerIterator) Next() {
	i.iter.Next()
}

// Close closes the iterator and transaction
func (i *badgerIterator) Close() {
	i.iter.Close()
	i.txn.Discard()
}