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

// VersionMeta contains metadata about a specific contract version
type VersionMeta struct {
	Version   int       `json:"version"`
	WASMHash  string    `json:"wasm_hash"`
	Path      string    `json:"path,omitempty"` // File path for Badger backend
	CreatedAt time.Time `json:"created_at"`
	Active    bool      `json:"active"`
}

// ContractVersions contains all versions of a contract
type ContractVersions struct {
	Address        string        `json:"address"`
	ActiveVersion  int           `json:"active_version"`
	Versions       []VersionMeta `json:"versions"`
	LatestVersion  int           `json:"latest_version"`
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

// SaveVersion stores a new version of a WASM module for the given contract address
func (s *Store) SaveVersion(addr string, wasm []byte) (int, [32]byte, error) {
	// Validate contract address format
	if !strings.HasPrefix(addr, "acc://") {
		return 0, [32]byte{}, fmt.Errorf("invalid contract address format: %s", addr)
	}

	// Validate WASM size (1-2 MB limit)
	const maxSize = 2 * 1024 * 1024 // 2 MB
	if len(wasm) == 0 {
		return 0, [32]byte{}, fmt.Errorf("WASM module cannot be empty")
	}
	if len(wasm) > maxSize {
		return 0, [32]byte{}, fmt.Errorf("WASM module too large: %d bytes (max: %d)", len(wasm), maxSize)
	}

	// Compute hash
	hash := sha256.Sum256(wasm)
	hashStr := hex.EncodeToString(hash[:])

	// Get existing versions or create new
	versionsKey := "contract:versions:" + addr
	var versions ContractVersions

	versionsBytes, err := s.kv.Get(versionsKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			// First version of this contract
			versions = ContractVersions{
				Address:       addr,
				ActiveVersion: 0, // No active version yet
				Versions:      []VersionMeta{},
				LatestVersion: 0,
			}
		} else {
			return 0, [32]byte{}, fmt.Errorf("failed to get contract versions: %w", err)
		}
	} else {
		if err := json.Unmarshal(versionsBytes, &versions); err != nil {
			return 0, [32]byte{}, fmt.Errorf("failed to unmarshal contract versions: %w", err)
		}
	}

	// Check if this hash already exists
	for _, v := range versions.Versions {
		if v.WASMHash == hashStr {
			return v.Version, hash, fmt.Errorf("WASM module with this hash already exists as version %d", v.Version)
		}
	}

	// Create new version
	newVersion := versions.LatestVersion + 1
	versionMeta := VersionMeta{
		Version:   newVersion,
		WASMHash:  hashStr,
		CreatedAt: time.Now().UTC(),
		Active:    false, // New versions start inactive
	}

	// Store WASM bytes
	if s.inMemory {
		// Store WASM bytes directly in KV store
		wasmKey := fmt.Sprintf("contract:wasm:%s:v%d", addr, newVersion)
		if err := s.kv.Put(wasmKey, wasm); err != nil {
			return 0, [32]byte{}, fmt.Errorf("failed to store WASM bytes: %w", err)
		}
	} else {
		// Store WASM bytes as file
		wasmPath := filepath.Join(s.wasmDir, fmt.Sprintf("%s_v%d.wasm", hashStr, newVersion))
		if err := os.WriteFile(wasmPath, wasm, 0644); err != nil {
			return 0, [32]byte{}, fmt.Errorf("failed to write WASM file: %w", err)
		}
		versionMeta.Path = wasmPath
	}

	// Add new version to list
	versions.Versions = append(versions.Versions, versionMeta)
	versions.LatestVersion = newVersion

	// Store updated versions
	updatedVersionsBytes, err := json.Marshal(versions)
	if err != nil {
		return 0, [32]byte{}, fmt.Errorf("failed to marshal contract versions: %w", err)
	}

	if err := s.kv.Put(versionsKey, updatedVersionsBytes); err != nil {
		return 0, [32]byte{}, fmt.Errorf("failed to store contract versions: %w", err)
	}

	return newVersion, hash, nil
}

// ActivateVersion sets a specific version as the active version for a contract
func (s *Store) ActivateVersion(addr string, version int) error {
	versionsKey := "contract:versions:" + addr
	versionsBytes, err := s.kv.Get(versionsKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			return fmt.Errorf("contract not found: %s", addr)
		}
		return fmt.Errorf("failed to get contract versions: %w", err)
	}

	var versions ContractVersions
	if err := json.Unmarshal(versionsBytes, &versions); err != nil {
		return fmt.Errorf("failed to unmarshal contract versions: %w", err)
	}

	// Find the version to activate
	var targetVersion *VersionMeta
	for i := range versions.Versions {
		if versions.Versions[i].Version == version {
			targetVersion = &versions.Versions[i]
			break
		}
	}

	if targetVersion == nil {
		return fmt.Errorf("version %d not found for contract %s", version, addr)
	}

	// Deactivate all versions
	for i := range versions.Versions {
		versions.Versions[i].Active = false
	}

	// Activate target version
	targetVersion.Active = true
	versions.ActiveVersion = version

	// Store updated versions
	updatedVersionsBytes, err := json.Marshal(versions)
	if err != nil {
		return fmt.Errorf("failed to marshal contract versions: %w", err)
	}

	if err := s.kv.Put(versionsKey, updatedVersionsBytes); err != nil {
		return fmt.Errorf("failed to store contract versions: %w", err)
	}

	return nil
}

// ActiveVersionMeta returns metadata for the currently active version of a contract
func (s *Store) ActiveVersionMeta(addr string) (*VersionMeta, error) {
	versionsKey := "contract:versions:" + addr
	versionsBytes, err := s.kv.Get(versionsKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			return nil, fmt.Errorf("contract not found: %s", addr)
		}
		return nil, fmt.Errorf("failed to get contract versions: %w", err)
	}

	var versions ContractVersions
	if err := json.Unmarshal(versionsBytes, &versions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contract versions: %w", err)
	}

	if versions.ActiveVersion == 0 {
		return nil, fmt.Errorf("no active version for contract %s", addr)
	}

	// Find active version
	for _, v := range versions.Versions {
		if v.Version == versions.ActiveVersion && v.Active {
			return &v, nil
		}
	}

	return nil, fmt.Errorf("active version %d not found for contract %s", versions.ActiveVersion, addr)
}

// GetVersionModule retrieves a specific version of a WASM module
func (s *Store) GetVersionModule(addr string, version int) ([]byte, [32]byte, error) {
	versionsKey := "contract:versions:" + addr
	versionsBytes, err := s.kv.Get(versionsKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			return nil, [32]byte{}, fmt.Errorf("contract not found: %s", addr)
		}
		return nil, [32]byte{}, fmt.Errorf("failed to get contract versions: %w", err)
	}

	var versions ContractVersions
	if err := json.Unmarshal(versionsBytes, &versions); err != nil {
		return nil, [32]byte{}, fmt.Errorf("failed to unmarshal contract versions: %w", err)
	}

	// Find the requested version
	var targetVersion *VersionMeta
	for _, v := range versions.Versions {
		if v.Version == version {
			targetVersion = &v
			break
		}
	}

	if targetVersion == nil {
		return nil, [32]byte{}, fmt.Errorf("version %d not found for contract %s", version, addr)
	}

	// Parse hash
	hashBytes, err := hex.DecodeString(targetVersion.WASMHash)
	if err != nil {
		return nil, [32]byte{}, fmt.Errorf("invalid WASM hash in version metadata: %w", err)
	}
	var hash [32]byte
	copy(hash[:], hashBytes)

	var wasmBytes []byte
	if s.inMemory {
		// Load from KV store
		wasmKey := fmt.Sprintf("contract:wasm:%s:v%d", addr, version)
		wasmBytes, err = s.kv.Get(wasmKey)
		if err != nil {
			return nil, [32]byte{}, fmt.Errorf("failed to get WASM bytes from store: %w", err)
		}
	} else {
		// Load from file
		if targetVersion.Path == "" {
			return nil, [32]byte{}, fmt.Errorf("WASM file path not found in version metadata")
		}
		wasmBytes, err = os.ReadFile(targetVersion.Path)
		if err != nil {
			return nil, [32]byte{}, fmt.Errorf("failed to read WASM file %s: %w", targetVersion.Path, err)
		}
	}

	// Verify hash
	computedHash := sha256.Sum256(wasmBytes)
	if computedHash != hash {
		return nil, [32]byte{}, fmt.Errorf("WASM hash mismatch for contract %s version %d", addr, version)
	}

	return wasmBytes, hash, nil
}

// ListVersions returns all versions for a contract
func (s *Store) ListVersions(addr string) (*ContractVersions, error) {
	versionsKey := "contract:versions:" + addr
	versionsBytes, err := s.kv.Get(versionsKey)
	if err != nil {
		if err == state.ErrKeyNotFound {
			return nil, fmt.Errorf("contract not found: %s", addr)
		}
		return nil, fmt.Errorf("failed to get contract versions: %w", err)
	}

	var versions ContractVersions
	if err := json.Unmarshal(versionsBytes, &versions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal contract versions: %w", err)
	}

	// Don't expose internal file paths
	if !s.inMemory {
		for i := range versions.Versions {
			versions.Versions[i].Path = ""
		}
	}

	return &versions, nil
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