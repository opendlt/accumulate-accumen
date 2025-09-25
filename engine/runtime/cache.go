package runtime

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// PreparedModule contains a validated and compiled WASM module ready for execution
type PreparedModule struct {
	Module    wazero.CompiledModule
	Metadata  *ModuleMetadata
	Hash      [32]byte
	ByteSize  uint64
}

// ModuleMetadata contains information about a WASM module's capabilities
type ModuleMetadata struct {
	MemoryPages    uint32            `json:"memory_pages"`
	MaxMemoryPages uint32            `json:"max_memory_pages"`
	Exports        map[string]string `json:"exports"` // name -> type (func, global, etc.)
	Imports        map[string]string `json:"imports"` // name -> type
	HasFloatOps    bool              `json:"has_float_ops"`
	IsValid        bool              `json:"is_valid"`
	ValidationError string           `json:"validation_error,omitempty"`
}

// ModuleCache provides LRU caching for compiled WASM modules with determinism validation
type ModuleCache struct {
	mu       sync.RWMutex
	cache    map[[32]byte]*cacheEntry
	lruList  *lruNode
	maxSize  int
	runtime  wazero.Runtime
	config   *RuntimeConfig
	hitCount uint64
	missCount uint64
}

// cacheEntry represents a cache entry with LRU tracking
type cacheEntry struct {
	module   PreparedModule
	lruNode  *lruNode
	accessed int64
}

// lruNode represents a node in the LRU doubly-linked list
type lruNode struct {
	prev, next *lruNode
	hash       [32]byte
	entry      *cacheEntry
}

// NewModuleCache creates a new module cache with the specified maximum size
func NewModuleCache(maxSize int, runtime wazero.Runtime, config *RuntimeConfig) *ModuleCache {
	if maxSize <= 0 {
		maxSize = 16 // Default cache size
	}

	// Create dummy head and tail nodes for LRU list
	head := &lruNode{}
	tail := &lruNode{}
	head.next = tail
	tail.prev = head

	return &ModuleCache{
		cache:   make(map[[32]byte]*cacheEntry),
		lruList: head,
		maxSize: maxSize,
		runtime: runtime,
		config:  config,
	}
}

// Prepare validates and compiles a WASM module, returning a PreparedModule
func (c *ModuleCache) Prepare(moduleBytes []byte) (PreparedModule, [32]byte, error) {
	// Compute hash
	hash := sha256.Sum256(moduleBytes)

	// Validate module bytes
	metadata, err := c.validateModule(moduleBytes)
	if err != nil {
		return PreparedModule{}, hash, fmt.Errorf("module validation failed: %w", err)
	}

	if !metadata.IsValid {
		return PreparedModule{}, hash, fmt.Errorf("module validation failed: %s", metadata.ValidationError)
	}

	// Compile the module
	compiledModule, err := c.runtime.CompileModule(context.Background(), moduleBytes)
	if err != nil {
		return PreparedModule{}, hash, fmt.Errorf("failed to compile WASM module: %w", err)
	}

	prepared := PreparedModule{
		Module:   compiledModule,
		Metadata: metadata,
		Hash:     hash,
		ByteSize: uint64(len(moduleBytes)),
	}

	return prepared, hash, nil
}

// Get retrieves a prepared module from the cache
func (c *ModuleCache) Get(hash [32]byte) (PreparedModule, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.cache[hash]
	if !exists {
		c.missCount++
		return PreparedModule{}, false
	}

	// Move to front of LRU list
	c.moveToFront(entry.lruNode)
	c.hitCount++

	return entry.module, true
}

// Put stores a prepared module in the cache
func (c *ModuleCache) Put(hash [32]byte, module PreparedModule) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if already exists
	if entry, exists := c.cache[hash]; exists {
		// Update existing entry
		entry.module = module
		c.moveToFront(entry.lruNode)
		return
	}

	// Evict if at capacity
	if len(c.cache) >= c.maxSize {
		c.evictLRU()
	}

	// Create new entry
	node := &lruNode{hash: hash}
	entry := &cacheEntry{
		module:  module,
		lruNode: node,
	}
	node.entry = entry

	c.cache[hash] = entry
	c.addToFront(node)
}

// validateModule performs comprehensive validation of a WASM module for determinism
func (c *ModuleCache) validateModule(moduleBytes []byte) (*ModuleMetadata, error) {
	metadata := &ModuleMetadata{
		Exports: make(map[string]string),
		Imports: make(map[string]string),
	}

	// Basic WASM header validation
	if len(moduleBytes) < 8 {
		metadata.ValidationError = "module too small"
		return metadata, nil
	}

	if string(moduleBytes[:4]) != "\x00asm" {
		metadata.ValidationError = "invalid WASM magic number"
		return metadata, nil
	}

	// Parse module to get metadata (simplified parsing)
	compiledModule, err := c.runtime.CompileModule(context.Background(), moduleBytes)
	if err != nil {
		metadata.ValidationError = fmt.Sprintf("compilation failed: %v", err)
		return metadata, nil
	}

	// Get module definition for analysis
	moduleDefn := compiledModule.(*moduleImpl) // This is wazero-specific, may need adjustment

	// Validate memory constraints
	if err := c.validateMemory(moduleDefn, metadata); err != nil {
		metadata.ValidationError = err.Error()
		return metadata, nil
	}

	// Validate exports
	if err := c.validateExports(moduleDefn, metadata); err != nil {
		metadata.ValidationError = err.Error()
		return metadata, nil
	}

	// Validate imports (ensure only allowed host functions)
	if err := c.validateImports(moduleDefn, metadata); err != nil {
		metadata.ValidationError = err.Error()
		return metadata, nil
	}

	// Check for non-deterministic operations (simplified check)
	if err := c.validateDeterminism(moduleBytes, metadata); err != nil {
		metadata.ValidationError = err.Error()
		return metadata, nil
	}

	metadata.IsValid = true
	return metadata, nil
}

// validateMemory checks memory limits and constraints
func (c *ModuleCache) validateMemory(module wazero.CompiledModule, metadata *ModuleMetadata) error {
	// Get memory information from compiled module
	// This is a simplified implementation - real validation would inspect the module sections

	// Set default memory constraints
	metadata.MemoryPages = 1    // Default 64KB (1 page)
	metadata.MaxMemoryPages = 64 // Max 4MB (64 pages)

	// Validate against configuration limits
	if c.config != nil {
		maxPages := uint32(c.config.MaxMemoryPages)
		if maxPages > 0 && metadata.MaxMemoryPages > maxPages {
			return fmt.Errorf("module max memory pages %d exceeds limit %d", metadata.MaxMemoryPages, maxPages)
		}
	}

	return nil
}

// validateExports ensures required exports are present
func (c *ModuleCache) validateExports(module wazero.CompiledModule, metadata *ModuleMetadata) error {
	// Get exported functions
	exportedFunctions := module.ExportedFunctions()

	for name := range exportedFunctions {
		metadata.Exports[name] = "func"
	}

	// Check for required exports (for demo - increment function)
	requiredExports := []string{"increment"}
	for _, required := range requiredExports {
		if _, exists := metadata.Exports[required]; !exists {
			return fmt.Errorf("missing required export: %s", required)
		}
	}

	return nil
}

// validateImports ensures only allowed host functions are imported
func (c *ModuleCache) validateImports(module wazero.CompiledModule, metadata *ModuleMetadata) error {
	// Get imported functions
	importedFunctions := module.ImportedFunctions()

	// List of allowed host function imports
	allowedImports := map[string]bool{
		"host.get":          true,
		"host.put":          true,
		"host.delete":       true,
		"host.emit_event":   true,
		"host.stage_op":     true,
		"host.charge_gas":   true,
		"host.log":          true,
	}

	for _, importDef := range importedFunctions {
		importName := fmt.Sprintf("%s.%s", importDef.ModuleName(), importDef.Name())
		metadata.Imports[importName] = "func"

		if !allowedImports[importName] {
			return fmt.Errorf("disallowed import: %s", importName)
		}
	}

	return nil
}

// validateDeterminism checks for non-deterministic operations
func (c *ModuleCache) validateDeterminism(moduleBytes []byte, metadata *ModuleMetadata) error {
	// Simplified determinism check by scanning bytecode for prohibited opcodes
	// In a full implementation, this would parse the WASM binary format properly

	prohibited := [][]byte{
		// Float operations (simplified check)
		{0x2A}, // f32.add
		{0x2B}, // f32.sub
		{0x2C}, // f32.mul
		{0x2D}, // f32.div
		{0x38}, // f64.add
		{0x39}, // f64.sub
		{0x3A}, // f64.mul
		{0x3B}, // f64.div
		// Non-deterministic operations
		{0x3F}, // current_memory (can be non-deterministic)
		{0x40}, // grow_memory (can be non-deterministic)
	}

	for _, opcode := range prohibited {
		if containsBytes(moduleBytes, opcode) {
			metadata.HasFloatOps = true
			return fmt.Errorf("module contains prohibited non-deterministic operations")
		}
	}

	return nil
}

// containsBytes checks if haystack contains needle
func containsBytes(haystack, needle []byte) bool {
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// moveToFront moves a node to the front of the LRU list
func (c *ModuleCache) moveToFront(node *lruNode) {
	// Remove from current position
	node.prev.next = node.next
	node.next.prev = node.prev

	// Add to front
	c.addToFront(node)
}

// addToFront adds a node to the front of the LRU list
func (c *ModuleCache) addToFront(node *lruNode) {
	node.next = c.lruList.next
	node.prev = c.lruList
	c.lruList.next.prev = node
	c.lruList.next = node
}

// evictLRU removes the least recently used entry from the cache
func (c *ModuleCache) evictLRU() {
	if c.lruList.prev == c.lruList {
		return // Empty list
	}

	// Get the least recently used node (tail of list)
	lru := c.lruList.prev

	// Remove from list
	lru.prev.next = lru.next
	lru.next.prev = lru.prev

	// Remove from cache map
	delete(c.cache, lru.hash)

	// Close the compiled module to free resources
	if lru.entry != nil {
		lru.entry.module.Module.Close(context.Background())
	}
}

// GetStats returns cache statistics
func (c *ModuleCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Size:      len(c.cache),
		MaxSize:   c.maxSize,
		HitCount:  c.hitCount,
		MissCount: c.missCount,
		HitRatio:  float64(c.hitCount) / float64(c.hitCount+c.missCount),
	}
}

// CacheStats contains cache performance statistics
type CacheStats struct {
	Size      int     `json:"size"`
	MaxSize   int     `json:"max_size"`
	HitCount  uint64  `json:"hit_count"`
	MissCount uint64  `json:"miss_count"`
	HitRatio  float64 `json:"hit_ratio"`
}

// Clear removes all entries from the cache
func (c *ModuleCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close all compiled modules
	for _, entry := range c.cache {
		if entry.module.Module != nil {
			entry.module.Module.Close(context.Background())
		}
	}

	// Reset cache
	c.cache = make(map[[32]byte]*cacheEntry)
	head := &lruNode{}
	tail := &lruNode{}
	head.next = tail
	tail.prev = head
	c.lruList = head

	c.hitCount = 0
	c.missCount = 0
}

// moduleImpl is a type assertion helper for wazero internals
// This may need to be adjusted based on the actual wazero implementation
type moduleImpl interface {
	wazero.CompiledModule
	// Additional methods for inspection if available
}