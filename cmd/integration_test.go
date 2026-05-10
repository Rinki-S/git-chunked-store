package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

// TestProcessCleanSmudge_RoundTripSmallFile tests that clean followed by smudge
// reproduces the original data for a small file (single chunk).
func TestProcessCleanSmudge_RoundTripSmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	original := []byte("hello world, this is a small test file")

	pointerStr, err := ProcessClean(original, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}

	if !bytes.Equal(reconstructed, original) {
		t.Errorf("round-trip mismatch:\n  got  %q\n  want %q", reconstructed, original)
	}
}

// TestProcessCleanSmudge_RoundTripEmptyFile tests that an empty file
// produces 0 chunks and smudge restores it to empty.
func TestProcessCleanSmudge_RoundTripEmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	original := []byte{}

	pointerStr, err := ProcessClean(original, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	// Verify pointer file has 0 chunks
	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}
	if p.Chunks != 0 {
		t.Errorf("expected 0 chunks for empty file, got %d", p.Chunks)
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}

	if len(reconstructed) != 0 {
		t.Errorf("expected empty result for empty file, got %d bytes", len(reconstructed))
	}
}

// TestProcessCleanSmudge_RoundTripMultipleChunks tests with data larger
// than the 64KB chunk size, producing multiple chunks.
func TestProcessCleanSmudge_RoundTripMultipleChunks(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// 200KB of data: 3 full chunks + 1 partial chunk
	size := 200 * 1024
	original := make([]byte, size)
	for i := range original {
		original[i] = byte(i % 256)
	}

	pointerStr, err := ProcessClean(original, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	// Verify pointer file has 4 chunks
	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}
	if p.Chunks != 4 {
		t.Errorf("expected 4 chunks, got %d", p.Chunks)
	}
	if p.Size != int64(size) {
		t.Errorf("expected size %d, got %d", size, p.Size)
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}

	if !bytes.Equal(reconstructed, original) {
		t.Errorf("round-trip mismatch: got %d bytes, want %d bytes", len(reconstructed), len(original))
	}
}

// TestProcessCleanSmudge_RoundTripExactChunkBoundary tests with data that
// is exactly a multiple of the chunk size (no partial last chunk).
func TestProcessCleanSmudge_RoundTripExactChunkBoundary(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// Exactly 128KB = 2 full chunks, no partial chunk
	size := 2 * DefaultChunkSize
	original := make([]byte, size)
	for i := range original {
		original[i] = byte((i * 7) % 256)
	}

	pointerStr, err := ProcessClean(original, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}
	if p.Chunks != 2 {
		t.Errorf("expected 2 chunks, got %d", p.Chunks)
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}

	if !bytes.Equal(reconstructed, original) {
		t.Errorf("round-trip mismatch for exact boundary file")
	}
}

// TestProcessCleanSmudge_RoundTripOneByteOver tests with data that is
// one byte over a chunk boundary (3 chunks: 1 full + 1 full + 1 byte).
func TestProcessCleanSmudge_RoundTripOneByteOver(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// 64KB * 2 + 1 byte = 3 chunks
	size := 2*DefaultChunkSize + 1
	original := make([]byte, size)
	original[0] = 0xAA
	original[DefaultChunkSize] = 0xBB
	original[size-1] = 0xCC

	pointerStr, err := ProcessClean(original, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}
	if p.Chunks != 3 {
		t.Errorf("expected 3 chunks, got %d", p.Chunks)
	}
	// Last chunk should be 1 byte
	lastOid := p.ChunkOids[2]
	chunk, err := s.Load(lastOid)
	if err != nil {
		t.Fatalf("loading last chunk: %v", err)
	}
	if len(chunk) != 1 {
		t.Errorf("last chunk size: got %d, want 1", len(chunk))
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}

	if !bytes.Equal(reconstructed, original) {
		t.Errorf("round-trip mismatch for one-byte-over file")
	}
}

// TestProcessClean_Deduplication tests that saving the same file twice
// results in deduplication (same chunks, no extra storage).
func TestProcessClean_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	data := []byte("dedup test content that is unique")

	// Clean the same data twice
	pointer1, err := ProcessClean(data, s)
	if err != nil {
		t.Fatalf("first ProcessClean failed: %v", err)
	}

	pointer2, err := ProcessClean(data, s)
	if err != nil {
		t.Fatalf("second ProcessClean failed: %v", err)
	}

	// Both should produce identical pointer files
	if pointer1 != pointer2 {
		t.Errorf("deduplication should produce same pointer:\n  first:  %s\n  second: %s", pointer1, pointer2)
	}

	// Smudge should still work after dedup
	reconstructed, err := ProcessSmudge([]byte(pointer1), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}
	if !bytes.Equal(reconstructed, data) {
		t.Errorf("round-trip after dedup mismatch")
	}
}

// TestProcessClean_Sha256OidCorrectness verifies that the file oid
// in the pointer file matches the SHA-256 of the original data.
func TestProcessClean_Sha256OidCorrectness(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	data := []byte("verify oid correctness")
	expectedHash := sha256.Sum256(data)
	expectedOid := hex.EncodeToString(expectedHash[:])

	pointerStr, err := ProcessClean(data, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}

	if p.Oid != expectedOid {
		t.Errorf("file oid mismatch:\n  got  %s\n  want %s", p.Oid, expectedOid)
	}
}

// TestProcessClean_Sha256ChunkOidCorrectness verifies that each chunk oid
// in the pointer file matches the SHA-256 of the corresponding chunk data.
func TestProcessClean_Sha256ChunkOidCorrectness(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// Use data that produces exactly 2 chunks
	size := DefaultChunkSize + 100
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 251)
	}

	pointerStr, err := ProcessClean(data, s)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}

	// Load each chunk and verify its hash matches the oid
	for i, oid := range p.ChunkOids {
		chunkData, err := s.Load(oid)
		if err != nil {
			t.Fatalf("loading chunk %d: %v", i, err)
		}
		actualHash := sha256.Sum256(chunkData)
		actualOid := hex.EncodeToString(actualHash[:])
		if actualOid != oid {
			t.Errorf("chunk %d oid mismatch: stored %s, computed %s", i, oid, actualOid)
		}
	}
}

// TestProcessSmudge_ChunkNotFoundError tests that smudge returns an error
// when a referenced chunk is missing from the store.
func TestProcessSmudge_ChunkNotFoundError(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// Create a pointer file referencing a non-existent chunk
	p := &pointer.Pointer{
		Oid:       "aabbccdd" + "0000000000000000000000000000000000000000000000000000",
		Size:      10,
		ChunkSize: DefaultChunkSize,
		Chunks:    1,
		ChunkOids: []string{"11223344" + "0000000000000000000000000000000000000000000000000000"},
	}
	pointerStr := p.Serialize()

	_, err := ProcessSmudge([]byte(pointerStr), s)
	if err == nil {
		t.Error("expected error for missing chunk, got nil")
	}
}

// TestProcessSmudge_InvalidPointer tests that smudge returns an error
// when given an invalid pointer file.
func TestProcessSmudge_InvalidPointer(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	_, err := ProcessSmudge([]byte("not a valid pointer file"), s)
	if err == nil {
		t.Error("expected error for invalid pointer, got nil")
	}
}
