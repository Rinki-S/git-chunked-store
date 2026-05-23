// Package cmd implements the stats subcommand for reporting chunk store
// metrics that are useful for demos and repository maintenance.
package cmd

import (
	"fmt"
	"os"

	"git-chunked-store/internal/store"
)

type chunkStoreStats struct {
	storedChunks       int
	referencedChunks   int
	orphanedChunks     int
	missingReferenced  int
	logicalSize        int64
	compressedSize     int64
	orphanedCompressed int64
}

// RunStats prints aggregate metrics for the local chunk store, including
// compression ratio and whether stored chunks are still referenced by git
// history.
func RunStats() error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	s := store.New(gitDir + "/chunked-objects")

	fmt.Fprintf(os.Stderr, "Collecting chunk references from git history...\n")

	referenced, err := collectReferencedOids()
	if err != nil {
		return fmt.Errorf("collecting referenced oids: %w", err)
	}

	stats, err := collectChunkStoreStats(s, referenced)
	if err != nil {
		return err
	}

	printChunkStoreStats(stats)
	return nil
}

func collectChunkStoreStats(s *store.Store, referenced map[string]bool) (*chunkStoreStats, error) {
	stored, err := s.List()
	if err != nil {
		return nil, fmt.Errorf("listing stored chunks: %w", err)
	}

	stats := &chunkStoreStats{
		storedChunks: len(stored),
	}

	storedSet := make(map[string]bool, len(stored))
	for _, oid := range stored {
		storedSet[oid] = true

		compressedSize, err := s.CompressedSize(oid)
		if err != nil {
			return nil, fmt.Errorf("reading compressed size for chunk %s: %w", oid, err)
		}

		chunk, err := s.Load(oid)
		if err != nil {
			return nil, fmt.Errorf("loading chunk %s: %w", oid, err)
		}

		stats.compressedSize += compressedSize
		stats.logicalSize += int64(len(chunk))

		if referenced[oid] {
			stats.referencedChunks++
		} else {
			stats.orphanedChunks++
			stats.orphanedCompressed += compressedSize
		}
	}

	for oid := range referenced {
		if !storedSet[oid] {
			stats.missingReferenced++
		}
	}

	return stats, nil
}

func printChunkStoreStats(stats *chunkStoreStats) {
	fmt.Printf("Stored chunks: %d\n", stats.storedChunks)
	fmt.Printf("Referenced chunks: %d\n", stats.referencedChunks)
	fmt.Printf("Orphaned chunks: %d\n", stats.orphanedChunks)
	fmt.Printf("Missing referenced chunks: %d\n", stats.missingReferenced)
	fmt.Printf("Logical chunk size: %s (%d bytes)\n", formatBytes(stats.logicalSize), stats.logicalSize)
	fmt.Printf("Compressed size: %s (%d bytes)\n", formatBytes(stats.compressedSize), stats.compressedSize)
	fmt.Printf("Orphaned compressed size: %s (%d bytes)\n", formatBytes(stats.orphanedCompressed), stats.orphanedCompressed)
	fmt.Printf("Compression ratio: %.1f%%\n", compressionRatio(stats.logicalSize, stats.compressedSize))
}

func compressionRatio(logicalSize, compressedSize int64) float64 {
	if logicalSize == 0 {
		return 0
	}
	return float64(compressedSize) / float64(logicalSize) * 100
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB", "TB", "PB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}

	return fmt.Sprintf("%.1f EB", value/unit)
}
