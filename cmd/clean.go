package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"git-chunked-store/internal/chunker"
	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

// DefaultChunkSize is the chunk size used by the clean filter (64KB).
const DefaultChunkSize = 64 * 1024

// ProcessClean is the core logic of the clean filter that operates on
// data already in memory. It splits data into chunks, saves each chunk
// via the store, and returns the serialized pointer file string.
//
// For large files, prefer ProcessCleanStreaming which reads from an io.Reader
// and avoids holding the entire file in memory at once.
func ProcessClean(data []byte, s *store.Store) (string, error) {
	// Compute full file SHA-256 as the file oid
	fileHash := sha256.Sum256(data)
	fileOid := hex.EncodeToString(fileHash[:])
	fileSize := int64(len(data))

	// Split data into 64KB chunks
	rawChunks := chunker.Split(data, DefaultChunkSize)

	// Compute chunk oids
	chunkOids := make([]string, 0, len(rawChunks))
	for _, ch := range rawChunks {
		oid := chunker.Hash(ch)
		chunkOids = append(chunkOids, oid)
	}

	// Save each chunk (deduplication is handled inside Store.Save)
	for i, ch := range rawChunks {
		if err := s.Save(chunkOids[i], ch); err != nil {
			return "", fmt.Errorf("saving chunk %d (%s): %w", i, chunkOids[i], err)
		}
	}

	// Build and serialize the pointer file
	p := &pointer.Pointer{
		Oid:       fileOid,
		Size:      fileSize,
		ChunkSize: DefaultChunkSize,
		Chunks:    len(chunkOids),
		ChunkOids: chunkOids,
	}

	return p.Serialize(), nil
}

// ProcessCleanStreaming reads data from an io.Reader in 64KB chunks, computes
// the full-file SHA-256 hash incrementally, saves each chunk to the store,
// and returns the serialized pointer file string.
//
// Unlike ProcessClean, it never holds the entire file in memory at once.
// Peak memory usage is approximately 2 × 64KB (one read buffer + one chunk copy
// being compressed). This makes it suitable for files larger than available RAM.
func ProcessCleanStreaming(r io.Reader, s *store.Store) (string, error) {
	fileHash := sha256.New()
	var fileSize int64
	var chunkOids []string
	buf := make([]byte, DefaultChunkSize)

	for chunkIndex := 0; ; chunkIndex++ {
		n, readErr := io.ReadFull(r, buf)

		// No bytes read — either EOF (empty or exhausted stream) or a real error
		if n == 0 {
			if readErr != nil && readErr != io.EOF {
				return "", fmt.Errorf("reading input: %w", readErr)
			}
			break
		}

		// Make a copy of the chunk data since buf will be overwritten
		// in the next iteration. This copy is at most 64KB, so total
		// peak memory is ~128KB regardless of file size.
		chunk := make([]byte, n)
		copy(chunk, buf[:n])

		// Update running hash for the full file
		fileHash.Write(chunk)
		fileSize += int64(n)

		// Compute chunk hash
		chunkHash := sha256.Sum256(chunk)
		chunkOid := hex.EncodeToString(chunkHash[:])
		chunkOids = append(chunkOids, chunkOid)

		// Save chunk to store (deduplication is handled inside Store.Save)
		if err := s.Save(chunkOid, chunk); err != nil {
			return "", fmt.Errorf("saving chunk %d (%s): %w", chunkIndex, chunkOid, err)
		}

		// io.ReadFull returns io.ErrUnexpectedEOF when fewer bytes than
		// buf size were read before hitting EOF. This is the normal end-of-file
		// case for the last partial chunk.
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}

		// Any other read error is unexpected
		if readErr != nil {
			return "", fmt.Errorf("reading input: %w", readErr)
		}
	}

	// Finalize the file hash (SHA-256 of the entire concatenated content)
	fileOid := hex.EncodeToString(fileHash.Sum(nil))

	p := &pointer.Pointer{
		Oid:       fileOid,
		Size:      fileSize,
		ChunkSize: DefaultChunkSize,
		Chunks:    len(chunkOids),
		ChunkOids: chunkOids,
	}

	return p.Serialize(), nil
}

// RunClean reads file content from stdin, splits it into chunks, stores each
// chunk in the chunked-objects store, and writes a pointer file to stdout.
// This command is invoked by git's clean filter when running `git add`.
//
// It uses ProcessCleanStreaming to avoid loading the entire file into memory,
// making it suitable for arbitrarily large files.
func RunClean() error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	s := store.New(gitDir + "/chunked-objects")

	result, err := ProcessCleanStreaming(os.Stdin, s)
	if err != nil {
		return err
	}

	fmt.Print(result)
	return nil
}

// findGitDir returns the path to the .git directory by running
// `git rev-parse --git-dir`. This works correctly whether the user
// is at the repo root or in a subdirectory.
func findGitDir() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-dir: %w", err)
	}
	// Trim trailing newline
	return strings.TrimRight(string(out), "\n"), nil
}
