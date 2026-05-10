package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

// pointerMagicHeader is the prefix that identifies a chunked-store pointer file.
// Any data starting with this string is treated as a pointer file; all other
// data is passed through unchanged. This mirrors git-lfs's approach to detecting
// pointer files and ensures raw binary content never triggers parse errors.
const pointerMagicHeader = "version https://git-lfs-chunked/"

// IsPointer checks whether data starts with the chunked-store pointer file
// header. This is used by the smudge filter to decide whether to reconstruct
// a file from chunks or pass it through as-is.
func IsPointer(data []byte) bool {
	return bytes.HasPrefix(data, []byte(pointerMagicHeader))
}

// ProcessSmudge is the core logic of the smudge filter, extracted for testability.
// It handles two cases:
//   - If the input is a pointer file (starts with the magic header), it parses
//     the pointer, loads chunks from the store, concatenates them, and verifies
//     the full-file SHA-256 hash for data integrity.
//   - If the input is NOT a pointer file, it returns the data unchanged. This
//     allows the smudge filter to gracefully handle files that haven't been
//     clean-filtered yet — for example, after running setup on a repo that
//     already contains binary files tracked by .gitattributes.
func ProcessSmudge(data []byte, s *store.Store) ([]byte, error) {
	// Graceful passthrough: if the input doesn't look like a pointer file,
	// return it unchanged. This is critical for repositories where .gitattributes
	// has been configured but some files haven't been through the clean filter yet.
	if !IsPointer(data) {
		return data, nil
	}

	p, err := pointer.ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parsing pointer file: %w", err)
	}

	// Empty file: 0 chunks, verify oid matches SHA-256 of empty data
	if p.Chunks == 0 {
		if p.Oid != emptySHA256 {
			return nil, fmt.Errorf("integrity check failed: file oid %q does not match expected empty file hash %q", p.Oid, emptySHA256)
		}
		return []byte{}, nil
	}

	var buf bytes.Buffer
	for i, oid := range p.ChunkOids {
		chunk, err := s.Load(oid)
		if err != nil {
			return nil, fmt.Errorf("loading chunk %d (%s): %w", i, oid, err)
		}
		buf.Write(chunk)
	}

	// Verify full-file SHA-256 integrity
	reconstructed := buf.Bytes()
	fileHash := sha256.Sum256(reconstructed)
	fileOid := hex.EncodeToString(fileHash[:])
	if fileOid != p.Oid {
		return nil, fmt.Errorf("integrity check failed: reconstructed file hash %q does not match pointer oid %q (file may be corrupted)", fileOid, p.Oid)
	}

	return reconstructed, nil
}

// RunSmudge reads data from stdin, determines whether it's a pointer file,
// and either reconstructs the original file content or passes the data
// through unchanged.
// This command is invoked by git's smudge filter when running `git checkout`.
func RunSmudge() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	// Fast path: if this is clearly not a pointer file, skip the git directory
	// lookup entirely and pass data through. This avoids spawning a git subprocess
	// for every raw binary file in the repository.
	if !IsPointer(data) {
		if _, err := os.Stdout.Write(data); err != nil {
			return fmt.Errorf("writing to stdout: %w", err)
		}
		return nil
	}

	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	s := store.New(gitDir + "/chunked-objects")

	result, err := ProcessSmudge(data, s)
	if err != nil {
		return err
	}

	if _, err := os.Stdout.Write(result); err != nil {
		return fmt.Errorf("writing to stdout: %w", err)
	}

	return nil
}

// emptySHA256 is the SHA-256 hash of an empty byte slice,
// precomputed to avoid recalculating it for every empty file.
const emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
