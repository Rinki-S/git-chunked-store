package chunker

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestSplit_EmptyData(t *testing.T) {
	result := Split([]byte{}, 64*1024)
	if result != nil {
		t.Errorf("expected nil for empty data, got %v", result)
	}
}

func TestSplit_SmallerThanChunkSize(t *testing.T) {
	data := []byte("hello")
	result := Split(data, 64*1024)
	if len(result) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result))
	}
	if string(result[0]) != "hello" {
		t.Errorf("unexpected chunk content: got %q, want %q", result[0], "hello")
	}
}

func TestSplit_ExactChunkSize(t *testing.T) {
	data := make([]byte, 64*1024) // exactly one chunk
	result := Split(data, 64*1024)
	if len(result) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result))
	}
	if len(result[0]) != 64*1024 {
		t.Errorf("chunk size mismatch: got %d, want %d", len(result[0]), 64*1024)
	}
}

func TestSplit_MultipleChunksLastSmaller(t *testing.T) {
	// 2 full chunks + 1 partial chunk
	size := 64*1024*2 + 100
	data := make([]byte, size)
	// fill with recognizable pattern
	for i := range data {
		data[i] = byte(i % 256)
	}

	result := Split(data, 64*1024)
	if len(result) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result))
	}
	if len(result[0]) != 64*1024 {
		t.Errorf("first chunk size: got %d, want %d", len(result[0]), 64*1024)
	}
	if len(result[1]) != 64*1024 {
		t.Errorf("second chunk size: got %d, want %d", len(result[1]), 64*1024)
	}
	if len(result[2]) != 100 {
		t.Errorf("last chunk size: got %d, want %d", len(result[2]), 100)
	}
}

func TestSplit_MultipleChunksExact(t *testing.T) {
	// exactly 3 full chunks
	size := 64 * 1024 * 3
	data := make([]byte, size)

	result := Split(data, 64*1024)
	if len(result) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result))
	}
	for i, chunk := range result {
		if len(chunk) != 64*1024 {
			t.Errorf("chunk %d size: got %d, want %d", i, len(chunk), 64*1024)
		}
	}
}

func TestSplit_ChunkSizeOne(t *testing.T) {
	data := []byte("abc")
	result := Split(data, 1)
	if len(result) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(result))
	}
	for i, chunk := range result {
		if len(chunk) != 1 {
			t.Errorf("chunk %d: got length %d, want 1", i, len(chunk))
		}
	}
	if string(result[0]) != "a" || string(result[1]) != "b" || string(result[2]) != "c" {
		t.Error("chunk content mismatch")
	}
}

func TestSplit_DefaultChunkSize(t *testing.T) {
	// chunkSize <= 0 should default to DefaultChunkSize
	data := []byte("hello")
	result := Split(data, 0)
	if len(result) != 1 {
		t.Fatalf("expected 1 chunk with default size, got %d", len(result))
	}
	if string(result[0]) != "hello" {
		t.Errorf("unexpected chunk content: got %q, want %q", result[0], "hello")
	}

	result2 := Split(data, -1)
	if len(result2) != 1 {
		t.Fatalf("expected 1 chunk with default size (negative), got %d", len(result2))
	}
}

func TestSplit_DataIntegrity(t *testing.T) {
	// Verify that concatenating all chunks reproduces original data
	original := make([]byte, 64*1024*2+500)
	for i := range original {
		original[i] = byte(i % 256)
	}

	chunks := Split(original, 64*1024)
	var reconstructed []byte
	for _, c := range chunks {
		reconstructed = append(reconstructed, c...)
	}

	if len(reconstructed) != len(original) {
		t.Errorf("reconstructed length mismatch: got %d, want %d", len(reconstructed), len(original))
	}
	for i := range original {
		if reconstructed[i] != original[i] {
			t.Errorf("byte mismatch at index %d: got %d, want %d", i, reconstructed[i], original[i])
			break
		}
	}
}

func TestHash_EmptyData(t *testing.T) {
	expected := sha256.Sum256([]byte{})
	expectedHex := hex.EncodeToString(expected[:])

	result := Hash([]byte{})
	if result != expectedHex {
		t.Errorf("hash mismatch: got %q, want %q", result, expectedHex)
	}
}

func TestHash_KnownValue(t *testing.T) {
	data := []byte("hello")
	expected := sha256.Sum256(data)
	expectedHex := hex.EncodeToString(expected[:])

	result := Hash(data)
	if result != expectedHex {
		t.Errorf("hash mismatch: got %q, want %q", result, expectedHex)
	}
}

func TestHash_Consistency(t *testing.T) {
	data := []byte("deterministic content")
	first := Hash(data)
	second := Hash(data)
	if first != second {
		t.Errorf("hash should be deterministic: got %q then %q", first, second)
	}
}

func TestHash_Length(t *testing.T) {
	result := Hash([]byte("test"))
	// SHA-256 hex string is 64 characters
	if len(result) != 64 {
		t.Errorf("hash length: got %d, want 64", len(result))
	}
}

func TestSplitWithHash_EmptyData(t *testing.T) {
	result := SplitWithHash([]byte{}, 64*1024)
	if result != nil {
		t.Errorf("expected nil for empty data, got %v", result)
	}
}

func TestSplitWithHash_SingleChunk(t *testing.T) {
	data := []byte("hello world")
	result := SplitWithHash(data, 64*1024)
	if len(result) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(result))
	}

	chunk := result[0]
	if chunk.Index != 0 {
		t.Errorf("index: got %d, want 0", chunk.Index)
	}
	if string(chunk.Data) != "hello world" {
		t.Errorf("data: got %q, want %q", chunk.Data, "hello world")
	}
	expectedOid := Hash([]byte("hello world"))
	if chunk.Oid != expectedOid {
		t.Errorf("oid: got %q, want %q", chunk.Oid, expectedOid)
	}
}

func TestSplitWithHash_MultipleChunks(t *testing.T) {
	size := 64*1024 + 500
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	result := SplitWithHash(data, 64*1024)
	if len(result) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(result))
	}

	// Verify indices
	if result[0].Index != 0 {
		t.Errorf("chunk 0 index: got %d, want 0", result[0].Index)
	}
	if result[1].Index != 1 {
		t.Errorf("chunk 1 index: got %d, want 1", result[1].Index)
	}

	// Verify chunk sizes
	if len(result[0].Data) != 64*1024 {
		t.Errorf("chunk 0 size: got %d, want %d", len(result[0].Data), 64*1024)
	}
	if len(result[1].Data) != 500 {
		t.Errorf("chunk 1 size: got %d, want 500", len(result[1].Data))
	}

	// Verify OIDs match individual Hash calls
	if result[0].Oid != Hash(data[:64*1024]) {
		t.Errorf("chunk 0 oid mismatch")
	}
	if result[1].Oid != Hash(data[64*1024:]) {
		t.Errorf("chunk 1 oid mismatch")
	}
}

func TestSplitWithHash_Reconstruction(t *testing.T) {
	original := make([]byte, 64*1024*3+1234)
	for i := range original {
		original[i] = byte(i % 256)
	}

	chunks := SplitWithHash(original, 64*1024)
	var reconstructed []byte
	for _, c := range chunks {
		reconstructed = append(reconstructed, c.Data...)
	}

	if len(reconstructed) != len(original) {
		t.Fatalf("length mismatch: got %d, want %d", len(reconstructed), len(original))
	}
	for i := range original {
		if reconstructed[i] != original[i] {
			t.Errorf("byte mismatch at index %d", i)
			break
		}
	}
}
