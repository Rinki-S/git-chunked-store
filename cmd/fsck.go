// Package cmd implements the fsck subcommand for verifying chunk store
// integrity against pointer files stored in reachable git history.
package cmd

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

type fsckPointerBlob struct {
	hash string
	path string
}

type fsckResult struct {
	pointerCount   int
	verifiedChunks int
	errors         int
}

// RunFSCK verifies that all pointer files in reachable git history can be
// reconstructed from the local chunk store and that every loaded chunk matches
// its content-addressed OID.
func RunFSCK() error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	s := store.New(gitDir + "/chunked-objects")

	fmt.Fprintf(os.Stderr, "Scanning git history for chunked pointer files...\n")

	blobs, err := collectUniqueBlobsWithPaths()
	if err != nil {
		return fmt.Errorf("collecting git blobs: %w", err)
	}

	result, err := fsckPointerBlobs(blobs, s)
	if err != nil {
		return err
	}

	if result.pointerCount == 0 {
		fmt.Fprintf(os.Stderr, "No chunked pointer files found.\n")
		return nil
	}

	fmt.Fprintf(os.Stderr, "Checked %d pointer file(s), %d unique chunk(s)\n", result.pointerCount, result.verifiedChunks)

	if result.errors > 0 {
		return fmt.Errorf("fsck found %d problem(s)", result.errors)
	}

	fmt.Fprintf(os.Stderr, "OK: chunk store is consistent\n")
	return nil
}

func collectUniqueBlobsWithPaths() ([]fsckPointerBlob, error) {
	commitsOutput, err := exec.Command("git", "rev-list", "--all").Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-list --all: %w", err)
	}

	commits := strings.Fields(strings.TrimSpace(string(commitsOutput)))
	seen := make(map[string]bool)
	var blobs []fsckPointerBlob

	for _, commit := range commits {
		lsTreeOutput, err := exec.Command("git", "ls-tree", "-r", commit).Output()
		if err != nil {
			continue
		}

		for line := range strings.SplitSeq(string(lsTreeOutput), "\n") {
			if line == "" {
				continue
			}

			meta, path, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}

			fields := strings.Fields(meta)
			if len(fields) < 3 || fields[1] != "blob" {
				continue
			}

			blobHash := fields[2]
			if seen[blobHash] {
				continue
			}

			seen[blobHash] = true
			blobs = append(blobs, fsckPointerBlob{
				hash: blobHash,
				path: path,
			})
		}
	}

	return blobs, nil
}

func fsckPointerBlobs(blobs []fsckPointerBlob, s *store.Store) (*fsckResult, error) {
	result := &fsckResult{}
	if len(blobs) == 0 {
		return result, nil
	}

	cmd := exec.Command("git", "cat-file", "--batch")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening cat-file stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("opening cat-file stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting git cat-file --batch: %w", err)
	}

	go func() {
		defer stdin.Close()

		for _, blob := range blobs {
			fmt.Fprintln(stdin, blob.hash)
		}
	}()

	reader := bufio.NewReader(stdout)
	verifiedChunks := make(map[string]bool)

	for _, blob := range blobs {
		content, objType, err := readBatchObject(reader)
		if err != nil {
			return nil, err
		}

		if objType != "blob" || !IsPointer(content) {
			continue
		}

		result.pointerCount++

		p, err := pointer.Parse(bytes.NewReader(content))
		if err != nil {
			reportFSCKError(result, "%s (%s): invalid pointer: %v", blob.path, blob.hash, err)
			continue
		}

		if err := verifyPointerChunks(blob, p, s, verifiedChunks, result); err != nil {
			reportFSCKError(result, "%s (%s): %v", blob.path, blob.hash, err)
		}
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("git cat-file --batch: %w", err)
	}

	result.verifiedChunks = len(verifiedChunks)
	return result, nil
}

func readBatchObject(reader *bufio.Reader) ([]byte, string, error) {
	header, err := reader.ReadString('\n')
	if err != nil {
		return nil, "", fmt.Errorf("reading cat-file header: %w", err)
	}

	fields := strings.Fields(header)
	if len(fields) != 3 {
		return nil, "", fmt.Errorf("invalid cat-file header: %q", strings.TrimSpace(header))
	}

	objType := fields[1]
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid cat-file object size %q: %w", fields[2], err)
	}

	content := make([]byte, size)
	if _, err := io.ReadFull(reader, content); err != nil {
		return nil, "", fmt.Errorf("reading cat-file object content: %w", err)
	}

	if _, err := reader.ReadByte(); err != nil {
		return nil, "", fmt.Errorf("reading cat-file object separator: %w", err)
	}

	return content, objType, nil
}

func verifyPointerChunks(_ fsckPointerBlob, p *pointer.Pointer, s *store.Store, verifiedChunks map[string]bool, _ *fsckResult) error {
	fileHash := sha256.New()
	var reconstructedSize int64

	for i, oid := range p.ChunkOids {
		chunk, err := s.Load(oid)
		if err != nil {
			return fmt.Errorf("loading chunk %d (%s): %w", i, oid, err)
		}

		if !verifiedChunks[oid] {
			if err := verifyChunkOid(oid, chunk); err != nil {
				return fmt.Errorf("chunk %d (%s): %w", i, oid, err)
			}
			verifiedChunks[oid] = true
		}

		if _, err := fileHash.Write(chunk); err != nil {
			return fmt.Errorf("hashing chunk %d (%s): %w", i, oid, err)
		}
		reconstructedSize += int64(len(chunk))
	}

	if reconstructedSize != p.Size {
		return fmt.Errorf("size mismatch: pointer says %d bytes, reconstructed %d bytes", p.Size, reconstructedSize)
	}

	fileOid := hex.EncodeToString(fileHash.Sum(nil))
	if fileOid != p.Oid {
		return fmt.Errorf("file hash mismatch: pointer says %s, reconstructed %s", p.Oid, fileOid)
	}

	return nil
}

func verifyChunkOid(oid string, chunk []byte) error {
	chunkHash := sha256.Sum256(chunk)
	actual := hex.EncodeToString(chunkHash[:])
	if actual != oid {
		return fmt.Errorf("hash mismatch: stored oid is %s, actual content hash is %s", oid, actual)
	}
	return nil
}

func reportFSCKError(result *fsckResult, format string, args ...any) {
	result.errors++
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
}
