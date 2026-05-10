// Package store implements the chunked object storage layer for git-chunked-store.
// It handles reading and writing chunk data to .git/chunked-objects/ with zlib
// compression and content-addressed deduplication.
package store

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DefaultBasePath is the default storage directory relative to the git repo root.
const DefaultBasePath = ".git/chunked-objects"

// Store manages chunked object storage on disk.
// Each chunk is stored at: <basePath>/<sha256_prefix_2>/<sha256_remaining_62>
// The data is zlib-compressed on write and decompressed on read.
// If a chunk already exists, Save is a no-op (deduplication).
type Store struct {
	basePath string
}

// New creates a new Store with the given base directory path.
// If basePath is empty, DefaultBasePath is used.
func New(basePath string) *Store {
	if basePath == "" {
		basePath = DefaultBasePath
	}
	return &Store{basePath: basePath}
}

// oidToPath converts a hex-encoded SHA-256 oid to a filesystem path.
// The path format is: <basePath>/<first 2 hex chars>/<remaining 62 hex chars>
// This mirrors git's loose object layout to avoid single-directory file count issues.
func (s *Store) oidToPath(oid string) string {
	if len(oid) < 3 {
		// Shouldn't happen with valid SHA-256 hashes, but handle gracefully
		return filepath.Join(s.basePath, oid)
	}
	return filepath.Join(s.basePath, oid[:2], oid[2:])
}

// Exists checks whether a chunk with the given oid is already stored.
func (s *Store) Exists(oid string) bool {
	_, err := os.Stat(s.oidToPath(oid))
	return err == nil
}

// Save writes a chunk to storage. The data is zlib-compressed before writing.
// If a chunk with the same oid already exists, it returns nil immediately (deduplication).
func (s *Store) Save(oid string, data []byte) error {
	path := s.oidToPath(oid)

	// Deduplication: skip if already stored
	if s.Exists(oid) {
		return nil
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory for chunk %s: %w", oid, err)
	}

	// Compress with zlib
	compressed, err := zlibCompress(data)
	if err != nil {
		return fmt.Errorf("compressing chunk %s: %w", oid, err)
	}

	// Write to a temp file first, then rename atomically to avoid partial writes
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, compressed, 0644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing chunk %s: %w", oid, err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("renaming chunk %s: %w", oid, err)
	}

	return nil
}

// Load reads and decompresses a chunk from storage by its oid.
func (s *Store) Load(oid string) ([]byte, error) {
	path := s.oidToPath(oid)

	compressed, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading chunk %s: %w", oid, err)
	}

	data, err := zlibDecompress(compressed)
	if err != nil {
		return nil, fmt.Errorf("decompressing chunk %s: %w", oid, err)
	}

	return data, nil
}

// Remove deletes a chunk from storage. This is useful for cleanup operations.
func (s *Store) Remove(oid string) error {
	path := s.oidToPath(oid)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing chunk %s: %w", oid, err)
	}
	return nil
}

// BasePath returns the base storage directory path.
func (s *Store) BasePath() string {
	return s.basePath
}

// zlibCompress compresses data using zlib at the default compression level.
func zlibCompress(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.DefaultCompression)
	if err != nil {
		return nil, fmt.Errorf("creating zlib writer: %w", err)
	}

	if _, err := w.Write(data); err != nil {
		w.Close()
		return nil, fmt.Errorf("zlib write: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("zlib close: %w", err)
	}

	return buf.Bytes(), nil
}

// zlibDecompress decompresses zlib-compressed data.
func zlibDecompress(compressed []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, fmt.Errorf("creating zlib reader: %w", err)
	}
	defer r.Close()

	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("zlib read: %w", err)
	}

	return data, nil
}
