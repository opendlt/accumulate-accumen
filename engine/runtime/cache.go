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
	Module   wazero.CompiledModule
	Metadata *ModuleMetadata
	Hash     [32]byte
	ByteSize uint64
}

// ModuleMetadata contains information about a WASM module's capabilities
type ModuleMetadata struct {
	MemoryPages     uint32            `json:"memory_pages"`
	MaxMemoryPages  uint32            `json:"max_memory_pages"`
	Exports         map[string]string `json:"exports"` // name -> type (func, global, etc.)
	Imports         map[string]string `json:"imports"` // name -> type
	HasFloatOps     bool              `json:"has_float_ops"`
	IsValid         bool              `json:"is_valid"`
	ValidationError string            `json:"validation_error,omitempty"`
}

// ModuleCache provides LRU caching for compiled WASM modules with determinism validation
type ModuleCache struct {
	mu        sync.RWMutex
	cache     map[[32]byte]*cacheEntry
	lruList   *lruNode
	maxSize   int
	runtime   wazero.Runtime
	config    *RuntimeConfig
	hitCount  uint64
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

	// Run comprehensive audit on first Prepare
	auditReport := AuditWASMModule(moduleBytes)
	if !auditReport.Ok {
		return PreparedModule{}, hash, fmt.Errorf("WASM audit failed: %s", strings.Join(auditReport.Reasons, "; "))
	}

	// Convert audit report to metadata
	metadata := &ModuleMetadata{
		MemoryPages:    auditReport.MemPages,
		MaxMemoryPages: auditReport.MemPages,
		Exports:        make(map[string]string),
		Imports:        make(map[string]string),
		HasFloatOps:    false, // Audit already rejected float ops
		IsValid:        auditReport.Ok,
	}

	// Populate exports from audit
	for _, exportName := range auditReport.Exports {
		metadata.Exports[exportName] = "func"
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

// Note: Module validation is now handled by the audit system in audit.go

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
