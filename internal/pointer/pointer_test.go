package pointer

import (
	"strings"
	"testing"
)

func TestSerialize_NormalFile(t *testing.T) {
	p := &Pointer{
		Oid:       "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		Size:      1048576,
		ChunkSize: 65536,
		Chunks:    2,
		ChunkOids: []string{
			"1111111111111111111111111111111111111111111111111111111111111111",
			"2222222222222222222222222222222222222222222222222222222222222222",
		},
	}

	result := p.Serialize()

	expected := "version https://git-lfs-chunked/1\n" +
		"oid sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n" +
		"size 1048576\n" +
		"chunk-size 65536\n" +
		"chunks 2\n" +
		"chunk-oids sha256:1111111111111111111111111111111111111111111111111111111111111111 sha256:2222222222222222222222222222222222222222222222222222222222222222\n"

	if result != expected {
		t.Errorf("serialize mismatch:\ngot:\n%s\nwant:\n%s", result, expected)
	}
}

func TestSerialize_EmptyFile(t *testing.T) {
	p := &Pointer{
		Oid:       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Size:      0,
		ChunkSize: 65536,
		Chunks:    0,
		ChunkOids: nil,
	}

	result := p.Serialize()

	expected := "version https://git-lfs-chunked/1\n" +
		"oid sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\n" +
		"size 0\n" +
		"chunk-size 65536\n" +
		"chunks 0\n"

	if result != expected {
		t.Errorf("serialize mismatch:\ngot:\n%s\nwant:\n%s", result, expected)
	}
}

func TestSerialize_SingleChunk(t *testing.T) {
	p := &Pointer{
		Oid:       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Size:      100,
		ChunkSize: 65536,
		Chunks:    1,
		ChunkOids: []string{
			"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
	}

	result := p.Serialize()

	expected := "version https://git-lfs-chunked/1\n" +
		"oid sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"size 100\n" +
		"chunk-size 65536\n" +
		"chunks 1\n" +
		"chunk-oids sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"

	if result != expected {
		t.Errorf("serialize mismatch:\ngot:\n%s\nwant:\n%s", result, expected)
	}
}

func TestSerialize_DefaultChunkSize(t *testing.T) {
	p := &Pointer{
		Oid:       "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		Size:      100,
		ChunkSize: 0, // should default to 65536
		Chunks:    1,
		ChunkOids: []string{"aaa"},
	}

	result := p.Serialize()

	if !strings.Contains(result, "chunk-size 65536\n") {
		t.Errorf("expected default chunk-size 65536, got:\n%s", result)
	}
}

func TestParse_NormalFile(t *testing.T) {
	input := "version https://git-lfs-chunked/1\n" +
		"oid sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n" +
		"size 1048576\n" +
		"chunk-size 65536\n" +
		"chunks 2\n" +
		"chunk-oids sha256:1111111111111111111111111111111111111111111111111111111111111111 sha256:2222222222222222222222222222222222222222222222222222222222222222\n"

	p, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Oid != "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" {
		t.Errorf("oid: got %q, want expected", p.Oid)
	}
	if p.Size != 1048576 {
		t.Errorf("size: got %d, want 1048576", p.Size)
	}
	if p.ChunkSize != 65536 {
		t.Errorf("chunk-size: got %d, want 65536", p.ChunkSize)
	}
	if p.Chunks != 2 {
		t.Errorf("chunks: got %d, want 2", p.Chunks)
	}
	if len(p.ChunkOids) != 2 {
		t.Fatalf("chunk-oids count: got %d, want 2", len(p.ChunkOids))
	}
	if p.ChunkOids[0] != "1111111111111111111111111111111111111111111111111111111111111111" {
		t.Errorf("chunk-oids[0]: got %q", p.ChunkOids[0])
	}
	if p.ChunkOids[1] != "2222222222222222222222222222222222222222222222222222222222222222" {
		t.Errorf("chunk-oids[1]: got %q", p.ChunkOids[1])
	}
}

func TestParse_EmptyFile(t *testing.T) {
	input := "version https://git-lfs-chunked/1\n" +
		"oid sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855\n" +
		"size 0\n" +
		"chunk-size 65536\n" +
		"chunks 0\n"

	p, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Oid != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Errorf("oid: got %q", p.Oid)
	}
	if p.Size != 0 {
		t.Errorf("size: got %d, want 0", p.Size)
	}
	if p.Chunks != 0 {
		t.Errorf("chunks: got %d, want 0", p.Chunks)
	}
	if p.ChunkOids != nil {
		t.Errorf("chunk-oids: expected nil, got %v", p.ChunkOids)
	}
}

func TestParse_SingleChunk(t *testing.T) {
	input := "version https://git-lfs-chunked/1\n" +
		"oid sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n" +
		"size 100\n" +
		"chunk-size 65536\n" +
		"chunks 1\n" +
		"chunk-oids sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n"

	p, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Chunks != 1 {
		t.Errorf("chunks: got %d, want 1", p.Chunks)
	}
	if len(p.ChunkOids) != 1 {
		t.Fatalf("chunk-oids count: got %d, want 1", len(p.ChunkOids))
	}
	if p.ChunkOids[0] != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Errorf("chunk-oids[0]: got %q", p.ChunkOids[0])
	}
}

func TestParse_BlankLinesAreIgnored(t *testing.T) {
	input := "\n" +
		"version https://git-lfs-chunked/1\n" +
		"\n" +
		"oid sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n" +
		"size 50\n" +
		"chunk-size 65536\n" +
		"chunks 1\n" +
		"chunk-oids sha256:1111111111111111111111111111111111111111111111111111111111111111\n" +
		"\n"

	p, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Oid != "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" {
		t.Errorf("oid: got %q", p.Oid)
	}
	if p.Chunks != 1 {
		t.Errorf("chunks: got %d, want 1", p.Chunks)
	}
}

func TestParse_UnknownKeysIgnored(t *testing.T) {
	input := "version https://git-lfs-chunked/1\n" +
		"oid sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n" +
		"size 50\n" +
		"chunk-size 65536\n" +
		"chunks 1\n" +
		"x-custom-field some-value\n" +
		"chunk-oids sha256:1111111111111111111111111111111111111111111111111111111111111111\n"

	p, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Chunks != 1 {
		t.Errorf("chunks: got %d, want 1", p.Chunks)
	}
}

func TestParse_Errors(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"missing version", "oid sha256:abc\nsize 10\nchunk-size 65536\nchunks 0\n"},
		{"wrong version", "version https://git-lfs.github.com/1\noid sha256:abc\nsize 10\nchunk-size 65536\nchunks 0\n"},
		{"missing oid", "version https://git-lfs-chunked/1\nsize 10\nchunk-size 65536\nchunks 0\n"},
		{"empty sha256 prefix", "version https://git-lfs-chunked/1\noid sha256:\nsize 10\nchunk-size 65536\nchunks 0\n"},
		{"invalid size", "version https://git-lfs-chunked/1\noid sha256:abc\nsize notanumber\nchunk-size 65536\nchunks 0\n"},
		{"invalid chunk-size", "version https://git-lfs-chunked/1\noid sha256:abc\nsize 10\nchunk-size notanumber\nchunks 0\n"},
		{"invalid chunks", "version https://git-lfs-chunked/1\noid sha256:abc\nsize 10\nchunk-size 65536\nchunks notanumber\n"},
		{"chunks mismatch", "version https://git-lfs-chunked/1\noid sha256:abc\nsize 10\nchunk-size 65536\nchunks 2\nchunk-oids sha256:aaa\n"},
		{"malformed line", "version https://git-lfs-chunked/1\njustakeywithnovalue\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tt.input))
			if err == nil {
				t.Errorf("expected error for %s, got nil", tt.name)
			}
		})
	}
}

func TestRoundTrip_NormalFile(t *testing.T) {
	original := &Pointer{
		Oid:       "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		Size:      2097152,
		ChunkSize: 65536,
		Chunks:    3,
		ChunkOids: []string{
			"a111111111111111111111111111111111111111111111111111111111111111",
			"b222222222222222222222222222222222222222222222222222222222222222",
			"c333333333333333333333333333333333333333333333333333333333333333",
		},
	}

	serialized := original.Serialize()
	parsed, err := Parse(strings.NewReader(serialized))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if parsed.Oid != original.Oid {
		t.Errorf("oid: got %q, want %q", parsed.Oid, original.Oid)
	}
	if parsed.Size != original.Size {
		t.Errorf("size: got %d, want %d", parsed.Size, original.Size)
	}
	if parsed.ChunkSize != original.ChunkSize {
		t.Errorf("chunk-size: got %d, want %d", parsed.ChunkSize, original.ChunkSize)
	}
	if parsed.Chunks != original.Chunks {
		t.Errorf("chunks: got %d, want %d", parsed.Chunks, original.Chunks)
	}
	if len(parsed.ChunkOids) != len(original.ChunkOids) {
		t.Fatalf("chunk-oids count: got %d, want %d", len(parsed.ChunkOids), len(original.ChunkOids))
	}
	for i, oid := range original.ChunkOids {
		if parsed.ChunkOids[i] != oid {
			t.Errorf("chunk-oids[%d]: got %q, want %q", i, parsed.ChunkOids[i], oid)
		}
	}
}

func TestRoundTrip_EmptyFile(t *testing.T) {
	original := &Pointer{
		Oid:       "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		Size:      0,
		ChunkSize: 65536,
		Chunks:    0,
		ChunkOids: nil,
	}

	serialized := original.Serialize()
	parsed, err := Parse(strings.NewReader(serialized))
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}

	if parsed.Oid != original.Oid {
		t.Errorf("oid: got %q, want %q", parsed.Oid, original.Oid)
	}
	if parsed.Size != original.Size {
		t.Errorf("size: got %d, want %d", parsed.Size, original.Size)
	}
	if parsed.Chunks != 0 {
		t.Errorf("chunks: got %d, want 0", parsed.Chunks)
	}
	if parsed.ChunkOids != nil {
		t.Errorf("chunk-oids: expected nil, got %v", parsed.ChunkOids)
	}
}

func TestParseBytes(t *testing.T) {
	input := []byte("version https://git-lfs-chunked/1\n" +
		"oid sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n" +
		"size 100\n" +
		"chunk-size 65536\n" +
		"chunks 1\n" +
		"chunk-oids sha256:1111111111111111111111111111111111111111111111111111111111111111\n")

	p, err := ParseBytes(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Oid != "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890" {
		t.Errorf("oid: got %q", p.Oid)
	}
	if p.Chunks != 1 {
		t.Errorf("chunks: got %d, want 1", p.Chunks)
	}
}

func TestParse_DefaultChunkSize(t *testing.T) {
	// Pointer file without chunk-size line — should default to 65536
	input := "version https://git-lfs-chunked/1\n" +
		"oid sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890\n" +
		"size 100\n" +
		"chunks 1\n" +
		"chunk-oids sha256:1111111111111111111111111111111111111111111111111111111111111111\n"

	p, err := Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ChunkSize != DefaultChunkSize {
		t.Errorf("chunk-size: got %d, want default %d", p.ChunkSize, DefaultChunkSize)
	}
}

func TestParseOid_WithPrefix(t *testing.T) {
	oid, err := parseOid("sha256:abcdef1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oid != "abcdef1234567890" {
		t.Errorf("got %q, want %q", oid, "abcdef1234567890")
	}
}

func TestParseOid_WithoutPrefix(t *testing.T) {
	oid, err := parseOid("abcdef1234567890")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if oid != "abcdef1234567890" {
		t.Errorf("got %q, want %q", oid, "abcdef1234567890")
	}
}

func TestParseOid_EmptySha256(t *testing.T) {
	_, err := parseOid("sha256:")
	if err == nil {
		t.Error("expected error for empty sha256 hash, got nil")
	}
}
