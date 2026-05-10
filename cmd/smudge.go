package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

// ProcessSmudge is the core logic of the smudge filter, extracted for testability.
// It parses a pointer file, loads each chunk from the store, concatenates them,
// and returns the reconstructed file content.
func ProcessSmudge(pointerData []byte, s *store.Store) ([]byte, error) {
	p, err := pointer.ParseBytes(pointerData)
	if err != nil {
		return nil, fmt.Errorf("parsing pointer file: %w", err)
	}

	// Empty file: 0 chunks, return empty bytes
	if p.Chunks == 0 {
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

	return buf.Bytes(), nil
}

// RunSmudge reads a pointer file from stdin, loads each chunk from the
// chunked-objects store, concatenates them, and writes the original file
// content to stdout.
// This command is invoked by git's smudge filter when running `git checkout`.
func RunSmudge() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("reading stdin: %w", err)
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
