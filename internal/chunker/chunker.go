// Package chunker provides file chunking and hashing functionality
// for the git-chunked-store backend.
package chunker

import (
	"crypto/sha256"
	"encoding/hex"
)

// DefaultChunkSize is the default chunk size: 64KB.
const DefaultChunkSize = 64 * 1024 // 65536 bytes

// Chunk represents a single data chunk along with its SHA-256 hash.
type Chunk struct {
	Index int    // zero-based position in the original file
	Oid   string // hex-encoded SHA-256 of the chunk data
	Data  []byte // raw chunk bytes
}

// Split splits data into chunks of the given size.
// The last chunk may be smaller than chunkSize.
// If data is empty (len == 0), it returns an empty slice (no chunks).
// If chunkSize <= 0, it defaults to DefaultChunkSize.
func Split(data []byte, chunkSize int) [][]byte {
	if len(data) == 0 {
		return nil
	}
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	var chunks [][]byte
	for offset := 0; offset < len(data); offset += chunkSize {
		end := offset + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunks = append(chunks, data[offset:end])
	}
	return chunks
}

// SplitWithHash splits data into chunks and computes SHA-256 for each.
// This is a convenience function combining Split and Hash.
func SplitWithHash(data []byte, chunkSize int) []Chunk {
	raw := Split(data, chunkSize)
	if len(raw) == 0 {
		return nil
	}

	chunks := make([]Chunk, len(raw))
	for i, d := range raw {
		chunks[i] = Chunk{
			Index: i,
			Oid:   Hash(d),
			Data:  d,
		}
	}
	return chunks
}

// Hash computes the SHA-256 hash of the given data and returns
// the hex-encoded string representation.
func Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
