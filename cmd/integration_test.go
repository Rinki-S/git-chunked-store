package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
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

// TestIsPointer tests the pointer file magic header detection.
func TestIsPointer(t *testing.T) {
	tests := []struct {
		name string
		data string
		want bool
	}{
		{"valid pointer header", "version https://git-lfs-chunked/1\noid sha256:abc", true},
		{"empty string", "", false},
		{"random binary", "\x89PNG\r\n\x1a\n", false},
		{"plain text", "not a valid pointer file", false},
		{"similar but different URL", "version https://git-lfs/1\n", false},
		{"partial match", "version https://git-lfs-chunke", false},
		{"just the header", "version https://git-lfs-chunked/1\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPointer([]byte(tt.data))
			if got != tt.want {
				t.Errorf("IsPointer(%q) = %v, want %v", tt.data, got, tt.want)
			}
		})
	}
}

// TestProcessSmudge_Passthrough tests that non-pointer content is returned
// unchanged. This is critical for repositories where .gitattributes has been
// configured but some files haven't been through the clean filter yet.
func TestProcessSmudge_Passthrough(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	tests := []struct {
		name string
		data []byte
	}{
		{"plain text", []byte("hello world")},
		{"binary-like data", []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A}},
		{"empty data", []byte{}},
		{"random binary", append([]byte{0xFF, 0xFE}, []byte("some content")...)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ProcessSmudge(tt.data, s)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !bytes.Equal(result, tt.data) {
				t.Errorf("passthrough mismatch: got %v, want %v", result, tt.data)
			}
		})
	}
}

// TestProcessSmudge_MalformedPointer tests that smudge returns an error
// when given data that starts with the pointer magic header but fails to parse.
func TestProcessSmudge_MalformedPointer(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// Starts with the magic header but is not a valid pointer file
	malformed := []byte("version https://git-lfs-chunked/1\nbroken content here\n")

	_, err := ProcessSmudge(malformed, s)
	if err == nil {
		t.Error("expected error for malformed pointer, got nil")
	}
}

// TestProcessCleanStreaming_SmallFile tests streaming clean with a small file
// that fits within a single chunk.
func TestProcessCleanStreaming_SmallFile(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	original := []byte("hello world, streaming test")

	pointerStr, err := ProcessCleanStreaming(strings.NewReader(string(original)), s)
	if err != nil {
		t.Fatalf("ProcessCleanStreaming failed: %v", err)
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}

	if !bytes.Equal(reconstructed, original) {
		t.Errorf("round-trip mismatch: got %q, want %q", reconstructed, original)
	}
}

// TestProcessCleanStreaming_EmptyFile tests streaming clean with an empty file
// (0 chunks).
func TestProcessCleanStreaming_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	pointerStr, err := ProcessCleanStreaming(strings.NewReader(""), s)
	if err != nil {
		t.Fatalf("ProcessCleanStreaming failed: %v", err)
	}

	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}
	if p.Chunks != 0 {
		t.Errorf("expected 0 chunks for empty file, got %d", p.Chunks)
	}
	if p.Size != 0 {
		t.Errorf("expected size 0, got %d", p.Size)
	}

	reconstructed, err := ProcessSmudge([]byte(pointerStr), s)
	if err != nil {
		t.Fatalf("ProcessSmudge failed: %v", err)
	}
	if len(reconstructed) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(reconstructed))
	}
}

// TestProcessCleanStreaming_MultipleChunks tests streaming clean with data
// larger than 64KB, producing multiple chunks.
func TestProcessCleanStreaming_MultipleChunks(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// 200KB of data: 3 full chunks + 1 partial chunk
	size := 200 * 1024
	original := make([]byte, size)
	for i := range original {
		original[i] = byte(i % 256)
	}

	pointerStr, err := ProcessCleanStreaming(bytes.NewReader(original), s)
	if err != nil {
		t.Fatalf("ProcessCleanStreaming failed: %v", err)
	}

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

// TestProcessCleanStreaming_ExactChunkBoundary tests streaming clean with data
// that is exactly a multiple of the chunk size.
func TestProcessCleanStreaming_ExactChunkBoundary(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	// Exactly 128KB = 2 full chunks, no partial chunk
	size := 2 * DefaultChunkSize
	original := make([]byte, size)
	for i := range original {
		original[i] = byte((i * 7) % 256)
	}

	pointerStr, err := ProcessCleanStreaming(bytes.NewReader(original), s)
	if err != nil {
		t.Fatalf("ProcessCleanStreaming failed: %v", err)
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

// TestProcessCleanStreaming_CommonWithProcessClean verifies that streaming and
// in-memory processing produce identical pointer files for the same input data.
func TestProcessCleanStreaming_CommonWithProcessClean(t *testing.T) {
	// Use two separate stores so chunk deduplication doesn't mask differences
	s1 := store.New(t.TempDir())
	s2 := store.New(t.TempDir())

	original := make([]byte, 3*DefaultChunkSize+500)
	for i := range original {
		original[i] = byte(i % 251)
	}

	pointerInMem, err := ProcessClean(original, s1)
	if err != nil {
		t.Fatalf("ProcessClean failed: %v", err)
	}

	pointerStreaming, err := ProcessCleanStreaming(bytes.NewReader(original), s2)
	if err != nil {
		t.Fatalf("ProcessCleanStreaming failed: %v", err)
	}

	if pointerInMem != pointerStreaming {
		t.Errorf("in-memory and streaming produce different pointer files:\n  in-mem:    %s\n  streaming: %s", pointerInMem, pointerStreaming)
	}
}

// TestProcessCleanStreaming_Sha256Correctness verifies that the streaming
// SHA-256 computation produces the same hash as a single-pass computation.
func TestProcessCleanStreaming_Sha256Correctness(t *testing.T) {
	tmpDir := t.TempDir()
	s := store.New(tmpDir)

	data := make([]byte, DefaultChunkSize+500)
	for i := range data {
		data[i] = byte(i % 251)
	}

	// Expected hash computed in one pass
	expectedHash := sha256.Sum256(data)
	expectedOid := hex.EncodeToString(expectedHash[:])

	pointerStr, err := ProcessCleanStreaming(bytes.NewReader(data), s)
	if err != nil {
		t.Fatalf("ProcessCleanStreaming failed: %v", err)
	}

	p, err := pointer.ParseBytes([]byte(pointerStr))
	if err != nil {
		t.Fatalf("parsing pointer: %v", err)
	}

	if p.Oid != expectedOid {
		t.Errorf("streaming hash mismatch:\n  got  %s\n  want %s", p.Oid, expectedOid)
	}
}
