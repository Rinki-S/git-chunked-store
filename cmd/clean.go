package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"

	"git-chunked-store/internal/chunker"
	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

// DefaultChunkSize is the chunk size used by the clean filter (64KB).
const DefaultChunkSize = 64 * 1024

// DefaultCleanWorkers is the default number of workers used to save chunks
// during streaming clean. It is tied to CPU count because chunk saving includes
// zlib compression, which is CPU-bound before the compressed bytes hit disk.
var DefaultCleanWorkers = runtime.NumCPU()

type cleanChunkJob struct {
	index int
	oid   string
	data  []byte
}

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
// Peak chunk-buffer memory is bounded by the worker count and channel capacity,
// so it scales with workers rather than file size. This makes it suitable for
// files larger than available RAM.
func ProcessCleanStreaming(r io.Reader, s *store.Store) (string, error) {
	return ProcessCleanStreamingWithWorkers(r, s, DefaultCleanWorkers)
}

// ProcessCleanStreamingWithWorkers reads data from an io.Reader in 64KB chunks,
// computes the full-file SHA-256 hash in input order, and saves chunks through
// a bounded worker pool.
//
// The pointer order is still deterministic: chunk OIDs are appended by the
// reader goroutine before each chunk is handed to workers. Only compression and
// storage are parallelized.
func ProcessCleanStreamingWithWorkers(r io.Reader, s *store.Store, workers int) (string, error) {
	if workers <= 0 {
		workers = 1
	}

	fileHash := sha256.New()
	var fileSize int64
	var chunkOids []string

	jobs := make(chan cleanChunkJob, workers)
	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for job := range jobs {
				if err := s.Save(job.oid, job.data); err != nil {
					select {
					case errCh <- fmt.Errorf("saving chunk %d (%s): %w", job.index, job.oid, err):
					default:
					}
					return
				}
			}
		}()
	}

	readErr := func() error {
		defer close(jobs)

		buf := make([]byte, DefaultChunkSize)
		for chunkIndex := 0; ; chunkIndex++ {
			n, err := io.ReadFull(r, buf)

			if n == 0 {
				if err != nil && err != io.EOF {
					return fmt.Errorf("reading input: %w", err)
				}
				return nil
			}
			chunk := make([]byte, n)
			copy(chunk, buf[:n])

			fileHash.Write(chunk)
			fileSize += int64(n)
			chunkHash := sha256.Sum256(chunk)
			chunkOid := hex.EncodeToString(chunkHash[:])
			chunkOids = append(chunkOids, chunkOid)

			select {
			case jobs <- cleanChunkJob{index: chunkIndex, oid: chunkOid, data: chunk}:
			case saveErr := <-errCh:
				return saveErr
			}

			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return nil
			}
			if err != nil {
				return fmt.Errorf("reading input: %w", err)
			}
		}
	}()

	wg.Wait()

	select {
	case saveErr := <-errCh:
		return "", saveErr
	default:
	}

	if readErr != nil {
		return "", readErr
	}

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
