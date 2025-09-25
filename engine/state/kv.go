package state

import (
	"fmt"
	"sync"
)

// KVStore defines the interface for key-value storage operations
type KVStore interface {
	Get(key []byte) ([]byte, error)
	Set(key, value []byte) error
	Delete(key []byte) error
	Exists(key []byte) (bool, error)
	Iterator(prefix []byte) Iterator
	Close() error
}

// Iterator provides iteration over key-value pairs
type Iterator interface {
	Next() bool
	Key() []byte
	Value() []byte
	Error() error
	Close() error
}

// MemoryKVStore is an in-memory implementation of KVStore
type MemoryKVStore struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemoryKVStore creates a new in-memory key-value store
func NewMemoryKVStore() *MemoryKVStore {
	return &MemoryKVStore{
		data: make(map[string][]byte),
	}
}

// Get retrieves a value by key
func (s *MemoryKVStore) Get(key []byte) ([]byte, error) {
	if len(key) == 0 {
		return nil, fmt.Errorf("key cannot be empty")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	value, exists := s.data[string(key)]
	if !exists {
		return nil, nil // Key not found, return nil without error
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(value))
	copy(result, value)
	return result, nil
}

// Set stores a key-value pair
func (s *MemoryKVStore) Set(key, value []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("key cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy to prevent external modification
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)
	s.data[string(key)] = valueCopy

	return nil
}

// Delete removes a key-value pair
func (s *MemoryKVStore) Delete(key []byte) error {
	if len(key) == 0 {
		return fmt.Errorf("key cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, string(key))
	return nil
}

// Exists checks if a key exists in the store
func (s *MemoryKVStore) Exists(key []byte) (bool, error) {
	if len(key) == 0 {
		return false, fmt.Errorf("key cannot be empty")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	_, exists := s.data[string(key)]
	return exists, nil
}

// Iterator returns an iterator for keys with the given prefix
func (s *MemoryKVStore) Iterator(prefix []byte) Iterator {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var keys []string
	prefixStr := string(prefix)

	for key := range s.data {
		if len(prefix) == 0 || (len(key) >= len(prefixStr) && key[:len(prefixStr)] == prefixStr) {
			keys = append(keys, key)
		}
	}

	return &memoryIterator{
		store: s,
		keys:  keys,
		index: -1,
	}
}

// Close closes the store (no-op for memory store)
func (s *MemoryKVStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = nil
	return nil
}

// Size returns the number of key-value pairs in the store
func (s *MemoryKVStore) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.data)
}

// Clear removes all key-value pairs from the store
func (s *MemoryKVStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data = make(map[string][]byte)
	return nil
}

// memoryIterator implements Iterator for MemoryKVStore
type memoryIterator struct {
	store *MemoryKVStore
	keys  []string
	index int
	err   error
}

// Next advances the iterator to the next key-value pair
func (it *memoryIterator) Next() bool {
	if it.err != nil {
		return false
	}

	it.index++
	return it.index < len(it.keys)
}

// Key returns the current key
func (it *memoryIterator) Key() []byte {
	if it.index < 0 || it.index >= len(it.keys) {
		return nil
	}
	return []byte(it.keys[it.index])
}

// Value returns the current value
func (it *memoryIterator) Value() []byte {
	if it.index < 0 || it.index >= len(it.keys) {
		return nil
	}

	key := it.keys[it.index]
	it.store.mu.RLock()
	defer it.store.mu.RUnlock()

	value, exists := it.store.data[key]
	if !exists {
		return nil
	}

	// Return a copy
	result := make([]byte, len(value))
	copy(result, value)
	return result
}

// Error returns any error that occurred during iteration
func (it *memoryIterator) Error() error {
	return it.err
}

// Close closes the iterator
func (it *memoryIterator) Close() error {
	it.keys = nil
	it.store = nil
	return nil
}