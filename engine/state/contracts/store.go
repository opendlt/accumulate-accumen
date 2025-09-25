package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/opendlt/accumulate-accumen/engine/state"
)

// ContractMeta contains metadata about a deployed contract
type ContractMeta struct {
	Address   string    `json:"address"`
	WASMHash  string    `json:"wasm_hash"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	BytesPath string    `json:"bytes_path,omitempty"` // Only for Badger backend
}

// Store manages contract WASM modules and metadata
type Store struct {
	kv       state.KVStore
	wasmDir  string // For Badger backend only
	inMemory bool
}

// NewStore creates a new contract store
func NewStore(kv state.KVStore, dataDir string) *Store {
	store := &Store{
		kv: kv,
	}

	// Check if we're using Badger backend by testing the type
	if _, isBadger := kv.(*state.BadgerStore); isBadger {
		store.wasmDir = filepath.Join(dataDir, "wasm")
		store.inMemory = false
		// Ensure wasm directory exists
		os.MkdirAll(store.wasmDir, 0755)
	} else {
		store.inMemory = true
	}

	return store
}

// SaveModule stores a WASM module with the given contract address
func (s *Store) SaveModule(addr string, wasm []byte) ([32]byte, error) {
	// Validate contract address format
	if !strings.HasPrefix(addr, "acc://") {
		return [32]byte{}, fmt.Errorf("invalid contract address format: %s", addr)
	}

	// Validate WASM size (1-2 MB limit)
	const maxSize = 2 * 1024 * 1024 // 2 MB
	if len(wasm) == 0 {
		return [32]byte{}, fmt.Errorf("WASM module cannot be empty")
	}
	if len(wasm) > maxSize {
		return [32]byte{}, fmt.Errorf("WASM module too large: %d bytes (max: %d)", len(wasm), maxSize)
	}

	// Compute hash
	hash := sha256.Sum256(wasm)
	hashStr := hex.EncodeToString(hash[:])

	// Create metadata
	meta := ContractMeta{
		Address:   addr,
		WASMHash:  hashStr,
		Version:   1,
		CreatedAt: time.Now().UTC(),
	}

	var wasmBytes []byte
	if s.inMemory {
		// Store WASM bytes directly in KV store
		wasmBytes = wasm
	} else {
		// Store WASM bytes as file and keep path in metadata
		wasmPath := filepath.Join(s.wasmDir, hashStr+".wasm")
		if err := os.WriteFile(wasmPath, wasm, 0644); err != nil {
			return [32]byte{}, fmt.Errorf("failed to write WASM file: %w", err)
		}
		meta.BytesPath = wasmPath
	}

	// Store metadata
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return [32]byte{}, fmt.Errorf("failed to marshal contract metadata: %w", err)
	}

	metaKey := "contract:meta:" + addr
	if err := s.kv.Put(metaKey, metaBytes); err != nil {
		return [32]byte{}, fmt.Errorf("failed to store contract metadata: %w", err)
	}

	// Store WASM bytes if in-memory
	if s.inMemory {
		wasmKey := "contract:wasm:" + addr
		if err := s.kv.Put(wasmKey, wasmBytes); err != nil {
			return [32]byte{}, fmt.Errorf("failed to store WASM bytes: %w", err)
		}
	}

	return hash, nil
}

// GetModule retrieves a WASM module by contract address
func (s *Store) GetModule(addr string) ([]byte, [32]byte, error) {
	// Get metadata first
	metaKey := "contract:meta:" + addr
	metaBytes, err := s.kv.Get(metaKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			return nil, [32]byte{}, fmt.Errorf("contract not found: %s", addr)
		}
		return nil, [32]byte{}, fmt.Errorf("failed to get contract metadata: %w", err)
	}

	var meta ContractMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, [32]byte{}, fmt.Errorf("failed to unmarshal contract metadata: %w", err)
	}

	// Parse hash
	hashBytes, err := hex.DecodeString(meta.WASMHash)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("invalid WASM hash in metadata: %w", err)
	}
	var hash [32]byte
	copy(hash[:], hashBytes)

	var wasmBytes []byte
	if s.inMemory {
		// Load from KV store
		wasmKey := "contract:wasm:" + addr
		wasmBytes, err = s.kv.Get(wasmKey)
		if err != nil {
			return nil, [32]byte{}, fmt.Errorf("failed to get WASM bytes from store: %w", err)
		}
	} else {
		// Load from file
		if meta.BytesPath == "" {
			return nil, [32]byte{}, fmt.Errorf("WASM bytes path not found in metadata")
		}
		wasmBytes, err = os.ReadFile(meta.BytesPath)
		if err != nil {
			return nil, [32]byte{}, fmt.Errorf("failed to read WASM file %s: %w", meta.BytesPath, err)
		}
	}

	// Verify hash
	computedHash := sha256.Sum256(wasmBytes)
	if computedHash != hash {
		return nil, [32]byte{}, fmt.Errorf("WASM hash mismatch for contract %s", addr)
	}

	return wasmBytes, hash, nil
}

// ListModules returns metadata for all deployed contracts
func (s *Store) ListModules() ([]ContractMeta, error) {
	iter, err := s.kv.Iterator("contract:meta:")
	if err != nil {
		return nil, fmt.Errorf("failed to create iterator: %w", err)
	}
	defer iter.Close()

	var modules []ContractMeta
	for iter.Valid() {
		metaBytes, err := iter.Value()
		if err != nil {
			return nil, fmt.Errorf("failed to get iterator value: %w", err)
		}

		var meta ContractMeta
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			// Skip invalid entries but continue
			iter.Next()
			continue
		}

		// Don't expose internal file paths in listings
		if !s.inMemory {
			meta.BytesPath = ""
		}

		modules = append(modules, meta)
		iter.Next()
	}

	return modules, nil
}

// HasModule checks if a contract is deployed
func (s *Store) HasModule(addr string) (bool, error) {
	metaKey := "contract:meta:" + addr
	return s.kv.Has(metaKey)
}

// DeleteModule removes a contract (for testing/admin purposes)
func (s *Store) DeleteModule(addr string) error {
	// Get metadata to find file path if using Badger
	if !s.inMemory {
		metaKey := "contract:meta:" + addr
		metaBytes, err := s.kv.Get(metaKey)
		if err == nil {
			var meta ContractMeta
			if err := json.Unmarshal(metaBytes, &meta); err == nil && meta.BytesPath != "" {
				// Try to remove file, but don't fail if it doesn't exist
				os.Remove(meta.BytesPath)
			}
		}
	}

	// Remove metadata
	metaKey := "contract:meta:" + addr
	if err := s.kv.Delete(metaKey); err != nil {
		return fmt.Errorf("failed to delete contract metadata: %w", err)
	}

	// Remove WASM bytes if in-memory
	if s.inMemory {
		wasmKey := "contract:wasm:" + addr
		if err := s.kv.Delete(wasmKey); err != nil {
			// Don't fail if WASM bytes key doesn't exist
			return nil
		}
	}

	return nil
}

// GetModuleHash returns just the hash of a deployed contract
func (s *Store) GetModuleHash(addr string) ([32]byte, error) {
	metaKey := "contract:meta:" + addr
	metaBytes, err := s.kv.Get(metaKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			return [32]byte{}, fmt.Errorf("contract not found: %s", addr)
		}
		return [32]byte{}, fmt.Errorf("failed to get contract metadata: %w", err)
	}

	var meta ContractMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return [32]byte{}, fmt.Errorf("failed to unmarshal contract metadata: %w", err)
	}

	hashBytes, err := hex.DecodeString(meta.WASMHash)
	if err != nil {
		return [32]byte{}, fmt.Errorf("invalid WASM hash in metadata: %w", err)
	}

	var hash [32]byte
	copy(hash[:], hashBytes)
	return hash, nil
}