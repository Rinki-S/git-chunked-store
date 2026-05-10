package store

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Helper: compute SHA-256 hex of data
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestNew_DefaultBasePath(t *testing.T) {
	s := New("")
	if s.BasePath() != DefaultBasePath {
		t.Errorf("expected default basePath %q, got %q", DefaultBasePath, s.BasePath())
	}
}

func TestNew_CustomBasePath(t *testing.T) {
	s := New("/tmp/my-chunks")
	if s.BasePath() != "/tmp/my-chunks" {
		t.Errorf("expected basePath %q, got %q", "/tmp/my-chunks", s.BasePath())
	}
}

func TestOidToPath(t *testing.T) {
	s := New("/repo/.git/chunked-objects")

	oid := "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
	// SHA-256 hex is 64 chars; first 2 = "a1", remaining = "b2c3..."
	expected := filepath.Join("/repo/.git/chunked-objects", "a1", "b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")

	got := s.oidToPath(oid)
	if got != expected {
		t.Errorf("oidToPath(%q) = %q, want %q", oid, got, expected)
	}
}

func TestOidToPath_ShortOid(t *testing.T) {
	s := New("/repo/.git/chunked-objects")

	// Oid shorter than 3 chars — should fall back to flat path
	shortOid := "ab"
	got := s.oidToPath(shortOid)
	expected := filepath.Join("/repo/.git/chunked-objects", shortOid)
	if got != expected {
		t.Errorf("oidToPath(%q) = %q, want %q", shortOid, got, expected)
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte("hello, this is a test chunk")
	oid := sha256Hex(data)

	// Save
	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Verify file exists on disk
	if !s.Exists(oid) {
		t.Fatal("Exists returned false after Save")
	}

	// Load and verify
	loaded, err := s.Load(oid)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !bytes.Equal(loaded, data) {
		t.Errorf("Load returned wrong data: got %q, want %q", loaded, data)
	}
}

func TestSave_Deduplication(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte("deduplicated content")
	oid := sha256Hex(data)

	// Save twice — second call should succeed without error
	if err := s.Save(oid, data); err != nil {
		t.Fatalf("first Save failed: %v", err)
	}
	if err := s.Save(oid, data); err != nil {
		t.Fatalf("second Save (dedup) failed: %v", err)
	}

	// Data should still be correct
	loaded, err := s.Load(oid)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	if !bytes.Equal(loaded, data) {
		t.Errorf("data mismatch after dedup: got %q, want %q", loaded, data)
	}
}

func TestSave_CreatesDirectoryStructure(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte("check dirs")
	oid := sha256Hex(data)

	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// The parent directory (first 2 hex chars) should exist
	prefix := oid[:2]
	dirPath := filepath.Join(tmpDir, prefix)
	info, err := os.Stat(dirPath)
	if err != nil {
		t.Fatalf("directory %q not found: %v", dirPath, err)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", dirPath)
	}
}

func TestSave_EmptyData(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte{}
	oid := sha256Hex(data) // SHA-256 of empty string

	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save of empty data failed: %v", err)
	}

	loaded, err := s.Load(oid)
	if err != nil {
		t.Fatalf("Load of empty data failed: %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("expected empty data, got %d bytes", len(loaded))
	}
}

func TestLoad_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	_, err := s.Load("nonexistent0000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Fatal("expected error for non-existent oid, got nil")
	}
}

func TestExists_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	if s.Exists("doesnotexist000000000000000000000000000000000000000000000000000") {
		t.Error("Exists returned true for non-existent oid")
	}
}

func TestRemove(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte("to be removed")
	oid := sha256Hex(data)

	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if !s.Exists(oid) {
		t.Fatal("chunk should exist after Save")
	}

	if err := s.Remove(oid); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if s.Exists(oid) {
		t.Error("chunk should not exist after Remove")
	}
}

func TestRemove_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	// Removing a non-existent oid should not error
	err := s.Remove("nonexistent000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Errorf("Remove of non-existent oid returned error: %v", err)
	}
}

func TestSave_CompressedOnDisk(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte("compress me, compress me, compress me, compress me")
	oid := sha256Hex(data)

	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Read the raw file from disk to verify it's zlib-compressed
	path := s.oidToPath(oid)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading raw file: %v", err)
	}

	// Compressed data should be smaller than original for repetitive content
	if len(raw) >= len(data) {
		t.Errorf("compressed data (%d bytes) should be smaller than original (%d bytes)", len(raw), len(data))
	}

	// Verify it's a valid zlib stream by decompressing
	r, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("raw file is not valid zlib: %v", err)
	}
	decompressed, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatalf("zlib decompression failed: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Error("decompressed data does not match original")
	}
}

func TestMultipleChunks(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	// Simulate saving multiple different chunks
	chunks := [][]byte{
		[]byte("chunk number one"),
		[]byte("chunk number two is a bit longer than the first"),
		[]byte("and the third chunk"),
		[]byte{}, // empty chunk
	}

	oids := make([]string, len(chunks))
	for i, data := range chunks {
		oids[i] = sha256Hex(data)
		if err := s.Save(oids[i], data); err != nil {
			t.Fatalf("Save chunk %d failed: %v", i, err)
		}
	}

	// Verify each chunk individually
	for i, data := range chunks {
		if !s.Exists(oids[i]) {
			t.Errorf("chunk %d should exist", i)
		}
		loaded, err := s.Load(oids[i])
		if err != nil {
			t.Errorf("Load chunk %d failed: %v", i, err)
		}
		if !bytes.Equal(loaded, data) {
			t.Errorf("chunk %d: got %q, want %q", i, loaded, data)
		}
	}
}

func TestSave_LargeData(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	// Create data larger than 64KB to test compression effectiveness
	data := make([]byte, 200*1024) // 200KB of zeros
	oid := sha256Hex(data)

	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save large data failed: %v", err)
	}

	loaded, err := s.Load(oid)
	if err != nil {
		t.Fatalf("Load large data failed: %v", err)
	}
	if !bytes.Equal(loaded, data) {
		t.Errorf("large data mismatch: got %d bytes, want %d bytes", len(loaded), len(data))
	}

	// Verify compression ratio — zeros compress very well
	path := s.oidToPath(oid)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat chunk file: %v", err)
	}
	compressedSize := fi.Size()
	if compressedSize >= int64(len(data)) {
		t.Errorf("compressed size %d should be much smaller than original %d", compressedSize, len(data))
	}
}

func TestZlibCompress(t *testing.T) {
	data := []byte("test data for compression")

	compressed, err := zlibCompress(data)
	if err != nil {
		t.Fatalf("zlibCompress failed: %v", err)
	}
	if len(compressed) == 0 {
		t.Error("zlibCompress returned empty result")
	}

	// Verify round-trip
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("creating zlib reader: %v", err)
	}
	decompressed, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		t.Fatalf("zlib decompression: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Errorf("round-trip mismatch: got %q, want %q", decompressed, data)
	}
}

func TestZlibDecompress(t *testing.T) {
	data := []byte("data to decompress")

	// Manually compress first
	var buf bytes.Buffer
	w, _ := zlib.NewWriterLevel(&buf, zlib.DefaultCompression)
	w.Write(data)
	w.Close()

	decompressed, err := zlibDecompress(buf.Bytes())
	if err != nil {
		t.Fatalf("zlibDecompress failed: %v", err)
	}
	if !bytes.Equal(decompressed, data) {
		t.Errorf("decompress mismatch: got %q, want %q", decompressed, data)
	}
}

func TestZlibDecompress_InvalidData(t *testing.T) {
	_, err := zlibDecompress([]byte("this is not zlib data"))
	if err == nil {
		t.Error("expected error for invalid zlib data, got nil")
	}
}

func TestZlibRoundTrip_Empty(t *testing.T) {
	data := []byte{}

	compressed, err := zlibCompress(data)
	if err != nil {
		t.Fatalf("zlibCompress empty data failed: %v", err)
	}

	decompressed, err := zlibDecompress(compressed)
	if err != nil {
		t.Fatalf("zlibDecompress empty data failed: %v", err)
	}
	if len(decompressed) != 0 {
		t.Errorf("expected empty result, got %d bytes", len(decompressed))
	}
}

func TestSave_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	s := New(tmpDir)

	data := []byte("atomic write test")
	oid := sha256Hex(data)

	// After a successful save, no .tmp file should remain
	if err := s.Save(oid, data); err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	tmpPath := s.oidToPath(oid) + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Errorf(".tmp file should not exist after successful Save: %s", tmpPath)
	}
}
