// Package cmd implements the CLI commands for git-chunked-store.
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

// ProcessClean is the core logic of the clean filter, extracted for testability.
// It splits data into chunks, saves each chunk via the store, and returns
// the serialized pointer file string.
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

// RunClean reads file content from stdin, splits it into chunks, stores each
// chunk in the chunked-objects store, and writes a pointer file to stdout.
// This command is invoked by git's clean filter when running `git add`.
func RunClean() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
	}

	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	s := store.New(gitDir + "/chunked-objects")

	result, err := ProcessClean(data, s)
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
