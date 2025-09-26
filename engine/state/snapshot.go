package state

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SnapshotMeta contains metadata about a snapshot
type SnapshotMeta struct {
	Height  uint64    `json:"height"`
	Time    time.Time `json:"time"`
	AppHash string    `json:"app_hash"`
	NumKeys uint64    `json:"num_keys"`
}

// SnapshotIterator provides sequential access to key-value pairs from a snapshot
type SnapshotIterator struct {
	file   *os.File
	reader *bufio.Reader
	meta   *SnapshotMeta
	keyNum uint64
	closed bool
}

// Next returns the next key-value pair, or (nil, nil, io.EOF) when done
func (it *SnapshotIterator) Next() ([]byte, []byte, error) {
	if it.closed || it.keyNum >= it.meta.NumKeys {
		return nil, nil, io.EOF
	}

	// Read key length
	var keyLen uint32
	if err := binary.Read(it.reader, binary.LittleEndian, &keyLen); err != nil {
		return nil, nil, fmt.Errorf("failed to read key length: %w", err)
	}

	// Read key
	key := make([]byte, keyLen)
	if _, err := io.ReadFull(it.reader, key); err != nil {
		return nil, nil, fmt.Errorf("failed to read key: %w", err)
	}

	// Read value length
	var valueLen uint32
	if err := binary.Read(it.reader, binary.LittleEndian, &valueLen); err != nil {
		return nil, nil, fmt.Errorf("failed to read value length: %w", err)
	}

	// Read value
	value := make([]byte, valueLen)
	if _, err := io.ReadFull(it.reader, value); err != nil {
		return nil, nil, fmt.Errorf("failed to read value: %w", err)
	}

	it.keyNum++
	return key, value, nil
}

// Meta returns the snapshot metadata
func (it *SnapshotIterator) Meta() *SnapshotMeta {
	return it.meta
}

// Close closes the iterator and underlying file
func (it *SnapshotIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	return it.file.Close()
}

// WriteSnapshot writes a snapshot of the KV store to the specified path
func WriteSnapshot(path string, kv KVStore, meta *SnapshotMeta) error {
	// Create the snapshot file
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create snapshot file: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	defer writer.Flush()

	// Count total keys first to set in metadata
	var keyCount uint64
	err = kv.Iterate(func(key, value []byte) error {
		keyCount++
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to count keys: %w", err)
	}

	// Update metadata with key count
	meta.NumKeys = keyCount

	// Write metadata header as JSON
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write metadata length followed by metadata
	if err := binary.Write(writer, binary.LittleEndian, uint32(len(metaBytes))); err != nil {
		return fmt.Errorf("failed to write metadata length: %w", err)
	}
	if _, err := writer.Write(metaBytes); err != nil {
		return fmt.Errorf("failed to write metadata: %w", err)
	}

	// Write key-value pairs
	return kv.Iterate(func(key, value []byte) error {
		// Write key length and key
		if err := binary.Write(writer, binary.LittleEndian, uint32(len(key))); err != nil {
			return fmt.Errorf("failed to write key length: %w", err)
		}
		if _, err := writer.Write(key); err != nil {
			return fmt.Errorf("failed to write key: %w", err)
		}

		// Write value length and value
		if err := binary.Write(writer, binary.LittleEndian, uint32(len(value))); err != nil {
			return fmt.Errorf("failed to write value length: %w", err)
		}
		if _, err := writer.Write(value); err != nil {
			return fmt.Errorf("failed to write value: %w", err)
		}

		return nil
	})
}

// ReadSnapshot reads a snapshot from the specified path and returns metadata and iterator
func ReadSnapshot(path string) (*SnapshotMeta, *SnapshotIterator, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open snapshot file: %w", err)
	}

	reader := bufio.NewReader(file)

	// Read metadata length
	var metaLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &metaLen); err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("failed to read metadata length: %w", err)
	}

	// Read metadata
	metaBytes := make([]byte, metaLen)
	if _, err := io.ReadFull(reader, metaBytes); err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var meta SnapshotMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		file.Close()
		return nil, nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	iterator := &SnapshotIterator{
		file:   file,
		reader: reader,
		meta:   &meta,
		keyNum: 0,
		closed: false,
	}

	return &meta, iterator, nil
}

// SnapshotInfo contains metadata about a snapshot file
type SnapshotInfo struct {
	Path     string        `json:"path"`
	Filename string        `json:"filename"`
	Size     int64         `json:"size"`
	ModTime  time.Time     `json:"mod_time"`
	Meta     *SnapshotMeta `json:"meta,omitempty"`
}

// ListSnapshots returns a list of snapshot files in the given directory
func ListSnapshots(dirPath string) ([]SnapshotInfo, error) {
	// Ensure directory exists
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return []SnapshotInfo{}, nil
	}

	// Read directory contents
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read snapshot directory %s: %w", dirPath, err)
	}

	var snapshots []SnapshotInfo

	for _, entry := range entries {
		// Only consider .snap files
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".snap") {
			continue
		}

		fullPath := filepath.Join(dirPath, entry.Name())

		// Get file info
		info, err := entry.Info()
		if err != nil {
			continue // Skip files we can't stat
		}

		snapshotInfo := SnapshotInfo{
			Path:     fullPath,
			Filename: entry.Name(),
			Size:     info.Size(),
			ModTime:  info.ModTime(),
		}

		// Try to read metadata (non-fatal if it fails)
		if meta, _, err := ReadSnapshot(fullPath); err == nil {
			snapshotInfo.Meta = meta
		}

		snapshots = append(snapshots, snapshotInfo)
	}

	// Sort by modification time (newest first)
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].ModTime.After(snapshots[j].ModTime)
	})

	return snapshots, nil
}

// PruneOld removes old snapshot files, keeping only the specified number of recent snapshots
func PruneOld(dirPath string, retain int) error {
	if retain <= 0 {
		return fmt.Errorf("retain count must be positive, got %d", retain)
	}

	// Get list of snapshots
	snapshots, err := ListSnapshots(dirPath)
	if err != nil {
		return fmt.Errorf("failed to list snapshots: %w", err)
	}

	// If we have fewer snapshots than the retain count, nothing to prune
	if len(snapshots) <= retain {
		return nil
	}

	// Remove old snapshots (keep the first 'retain' snapshots, which are the newest)
	toRemove := snapshots[retain:]

	var removedCount int
	var lastErr error

	for _, snapshot := range toRemove {
		if err := os.Remove(snapshot.Path); err != nil {
			lastErr = fmt.Errorf("failed to remove snapshot %s: %w", snapshot.Path, err)
			continue // Continue trying to remove other snapshots
		}
		removedCount++
	}

	if lastErr != nil {
		return fmt.Errorf("pruning completed with errors (removed %d of %d): %w",
			removedCount, len(toRemove), lastErr)
	}

	return nil
}

// GetSnapshotMeta reads only the metadata from a snapshot file without loading the full snapshot
func GetSnapshotMeta(path string) (*SnapshotMeta, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open snapshot file: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	// Read metadata length
	var metaLen uint32
	if err := binary.Read(reader, binary.LittleEndian, &metaLen); err != nil {
		return nil, fmt.Errorf("failed to read metadata length: %w", err)
	}

	// Read metadata
	metaBytes := make([]byte, metaLen)
	if _, err := io.ReadFull(reader, metaBytes); err != nil {
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var meta SnapshotMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &meta, nil
}

// RestoreFromSnapshot restores a KV store from a snapshot
func RestoreFromSnapshot(path string, kv KVStore) error {
	meta, iterator, err := ReadSnapshot(path)
	if err != nil {
		return fmt.Errorf("failed to read snapshot: %w", err)
	}
	defer iterator.Close()

	// Clear existing data
	if err := ClearKVStore(kv); err != nil {
		return fmt.Errorf("failed to clear KV store: %w", err)
	}

	// Restore key-value pairs
	var restoredCount uint64
	for {
		key, value, err := iterator.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read snapshot entry: %w", err)
		}

		if err := kv.Put(string(key), value); err != nil {
			return fmt.Errorf("failed to restore key %s: %w", string(key), err)
		}

		restoredCount++
	}

	if restoredCount != meta.NumKeys {
		return fmt.Errorf("restored key count mismatch: expected %d, got %d",
			meta.NumKeys, restoredCount)
	}

	return nil
}

// ClearKVStore removes all keys from a KV store
func ClearKVStore(kv KVStore) error {
	// Collect all keys first to avoid modifying during iteration
	var keys []string
	err := kv.Iterate(func(key, value []byte) error {
		keys = append(keys, string(key))
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to collect keys: %w", err)
	}

	// Delete all keys
	for _, key := range keys {
		if err := kv.Delete(key); err != nil {
			return fmt.Errorf("failed to delete key %s: %w", key, err)
		}
	}

	return nil
}