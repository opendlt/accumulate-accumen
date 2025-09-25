package state

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
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