// Package pointer implements serialization and deserialization of
// the git-chunked-store pointer file format.
package pointer

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Version is the pointer file format version string.
const Version = "https://git-lfs-chunked/1"

// DefaultChunkSize is the default chunk size used in pointer files.
const DefaultChunkSize = 65536

// Pointer represents a deserialized pointer file.
type Pointer struct {
	Oid       string   // hex-encoded SHA-256 of the full file
	Size      int64    // total file size in bytes
	ChunkSize int      // chunk size in bytes (typically 65536)
	Chunks    int      // number of chunks
	ChunkOids []string // SHA-256 oids of each chunk (hex-encoded, without "sha256:" prefix)
}

// Serialize produces the pointer file text from a Pointer.
// The format is:
//
//	version https://git-lfs-chunked/1
//	oid sha256:<oid>
//	size <size>
//	chunk-size <chunk-size>
//	chunks <n>
//	chunk-oids sha256:<oid1> sha256:<oid2> ...
//
// For empty files (0 chunks), the chunk-oids line is omitted.
func (p *Pointer) Serialize() string {
	chunkSize := p.ChunkSize
	if chunkSize <= 0 {
		chunkSize = DefaultChunkSize
	}

	var b strings.Builder
	fmt.Fprintf(&b, "version %s\n", Version)
	fmt.Fprintf(&b, "oid sha256:%s\n", p.Oid)
	fmt.Fprintf(&b, "size %d\n", p.Size)
	fmt.Fprintf(&b, "chunk-size %d\n", chunkSize)
	fmt.Fprintf(&b, "chunks %d\n", p.Chunks)

	if p.Chunks > 0 {
		b.WriteString("chunk-oids")
		for _, oid := range p.ChunkOids {
			fmt.Fprintf(&b, " sha256:%s", oid)
		}
		b.WriteString("\n")
	}

	return b.String()
}

// Parse reads a pointer file from the given reader and returns a Pointer.
// It returns an error if the format is invalid or missing required fields.
func Parse(r io.Reader) (*Pointer, error) {
	scanner := bufio.NewScanner(r)

	p := &Pointer{}
	hasVersion := false

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip blank lines
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			return nil, fmt.Errorf("invalid pointer line: %q", line)
		}

		key := parts[0]
		value := parts[1]

		switch key {
		case "version":
			hasVersion = true
			if value != Version {
				return nil, fmt.Errorf("unsupported pointer version: %q", value)
			}
		case "oid":
			oid, err := parseOid(value)
			if err != nil {
				return nil, fmt.Errorf("invalid oid: %w", err)
			}
			p.Oid = oid
		case "size":
			size, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid size %q: %w", value, err)
			}
			p.Size = size
		case "chunk-size":
			cs, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid chunk-size %q: %w", value, err)
			}
			p.ChunkSize = cs
		case "chunks":
			n, err := strconv.Atoi(value)
			if err != nil {
				return nil, fmt.Errorf("invalid chunks %q: %w", value, err)
			}
			p.Chunks = n
		case "chunk-oids":
			if value == "" {
				// empty chunk-oids line (shouldn't happen per spec, but be safe)
				p.ChunkOids = nil
			} else {
				tokenParts := strings.Fields(value)
				oids := make([]string, len(tokenParts))
				for i, part := range tokenParts {
					oid, err := parseOid(part)
					if err != nil {
						return nil, fmt.Errorf("invalid chunk-oid at index %d: %w", i, err)
					}
					oids[i] = oid
				}
				p.ChunkOids = oids
			}
		default:
			// Ignore unknown keys for forward compatibility
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading pointer: %w", err)
	}

	// Validate required fields
	if !hasVersion {
		return nil, fmt.Errorf("missing required field: version")
	}
	if p.Oid == "" {
		return nil, fmt.Errorf("missing required field: oid")
	}
	if p.ChunkSize <= 0 {
		p.ChunkSize = DefaultChunkSize
	}
	if p.Chunks < 0 {
		return nil, fmt.Errorf("invalid chunks count: %d", p.Chunks)
	}
	if len(p.ChunkOids) != p.Chunks {
		return nil, fmt.Errorf("chunks count %d does not match chunk-oids count %d", p.Chunks, len(p.ChunkOids))
	}

	return p, nil
}

// ParseBytes parses a pointer file from a byte slice.
func ParseBytes(data []byte) (*Pointer, error) {
	return Parse(strings.NewReader(string(data)))
}

// parseOid extracts the hex hash from a "sha256:<hex>" format,
// or returns the value as-is if no prefix is present.
func parseOid(value string) (string, error) {
	if strings.HasPrefix(value, "sha256:") {
		rest := value[len("sha256:"):]
		if rest == "" {
			return "", fmt.Errorf("empty sha256 hash")
		}
		return rest, nil
	}
	// Allow plain hex hash without prefix for robustness
	return value, nil
}
