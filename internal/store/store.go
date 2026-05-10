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
	"strconv"
	"sync/atomic"
)

// DefaultBasePath is the default storage directory relative to the git repo root.
const DefaultBasePath = ".git/chunked-objects"

// ValidOIDLength is the expected length of a hex-encoded SHA-256 hash.
const ValidOIDLength = 64

// tmpFileCounter is used to generate unique temporary file names when
// multiple processes write the same chunk concurrently.
var tmpFileCounter atomic.Int64

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

// validateOid checks that an oid is a valid hex-encoded SHA-256 hash.
// This prevents path traversal attacks (e.g., "../../etc/passwd") and
// ensures the oid is safe to use in filesystem paths.
func validateOid(oid string) error {
	if len(oid) != ValidOIDLength {
		return fmt.Errorf("invalid oid length: expected %d chars, got %d", ValidOIDLength, len(oid))
	}
	// Verify all characters are valid lowercase hex digits.
	// This rejects any path traversal attempts containing /, ., etc.
	for i, c := range oid {
		if !isHexDigit(c) {
			return fmt.Errorf("invalid oid character at position %d: %q (oid must be lowercase hex)", i, c)
		}
	}
	return nil
}

// isHexDigit returns true if c is a valid lowercase hex digit (0-9, a-f).
func isHexDigit(c rune) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}

// oidToPath converts a hex-encoded SHA-256 oid to a filesystem path.
// The path format is: <basePath>/<first 2 hex chars>/<remaining 62 hex chars>
// This mirrors git's loose object layout to avoid single-directory file count issues.
// The oid must be validated before calling this method.
func (s *Store) oidToPath(oid string) string {
	return filepath.Join(s.basePath, oid[:2], oid[2:])
}

// Exists checks whether a chunk with the given oid is already stored.
func (s *Store) Exists(oid string) (bool, error) {
	if err := validateOid(oid); err != nil {
		return false, fmt.Errorf("validating oid: %w", err)
	}
	_, err := os.Stat(s.oidToPath(oid))
	return err == nil, nil
}

// Save writes a chunk to storage. The data is zlib-compressed before writing.
// If a chunk with the same oid already exists, it returns nil immediately (deduplication).
// The oid must be a valid hex-encoded SHA-256 hash of the data for content-addressed
// storage to work correctly.
func (s *Store) Save(oid string, data []byte) error {
	if err := validateOid(oid); err != nil {
		return fmt.Errorf("validating oid: %w", err)
	}

	path := s.oidToPath(oid)

	// Deduplication: skip if already stored
	exists, err := s.Exists(oid)
	if err != nil {
		return fmt.Errorf("checking existence for chunk %s: %w", oid, err)
	}
	if exists {
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

	// Write to a temp file first, then rename atomically to avoid partial writes.
	// Use a unique suffix (process ID + monotonic counter) to avoid conflicts
	// when multiple processes write the same chunk concurrently.
	tmpPath := path + ".tmp." + strconv.Itoa(os.Getpid()) + "." + strconv.FormatInt(tmpFileCounter.Add(1), 10)
	if err := os.WriteFile(tmpPath, compressed, 0644); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("writing chunk %s: %w", oid, err)
	}

	// Atomic rename. If another process already wrote the same chunk, the target
	// file exists and Rename will fail on POSIX systems. Since content-addressed
	// storage guarantees identical content, this is a success case, not an error.
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		// Check if the target file now exists — if so, another process won the race
		// and we can safely treat this as success (the content is identical).
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return fmt.Errorf("renaming chunk %s: %w", oid, err)
	}

	return nil
}

// Load reads and decompresses a chunk from storage by its oid.
func (s *Store) Load(oid string) ([]byte, error) {
	if err := validateOid(oid); err != nil {
		return nil, fmt.Errorf("validating oid: %w", err)
	}

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

// List returns the OIDs of all chunks currently stored in the store.
// It walks the basePath directory and reconstructs OIDs from the
// two-level directory structure (<2-hex-prefix>/<62-hex-suffix>).
// This is used by the gc command to find unreferenced chunks.
func (s *Store) List() ([]string, error) {
	var oids []string

	entries, err := os.ReadDir(s.basePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading store directory: %w", err)
	}

	for _, prefixEntry := range entries {
		if !prefixEntry.IsDir() {
			continue
		}

		prefix := prefixEntry.Name()
		// The prefix directory should be exactly 2 lowercase hex chars
		if len(prefix) != 2 {
			continue
		}

		subDirPath := filepath.Join(s.basePath, prefix)
		subEntries, err := os.ReadDir(subDirPath)
		if err != nil {
			return nil, fmt.Errorf("reading subdirectory %s: %w", prefix, err)
		}

		for _, chunkEntry := range subEntries {
			if chunkEntry.IsDir() {
				continue
			}

			suffix := chunkEntry.Name()
			oid := prefix + suffix

			// Only include valid OIDs (64-char lowercase hex)
			if err := validateOid(oid); err != nil {
				continue
			}

			oids = append(oids, oid)
		}
	}

	return oids, nil
}

// Delete removes a chunk file from storage by its oid.
// This is used by the gc command to remove unreferenced chunks.
func (s *Store) Delete(oid string) error {
	if err := validateOid(oid); err != nil {
		return fmt.Errorf("validating oid: %w", err)
	}

	path := s.oidToPath(oid)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting chunk %s: %w", oid, err)
	}
	return nil
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
