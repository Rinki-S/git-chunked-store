// Package cmd implements the gc subcommand for garbage-collecting
// unreferenced chunks from the chunked-objects store.
package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"git-chunked-store/internal/pointer"
	"git-chunked-store/internal/store"
)

// RunGC scans the git repository for all pointer files, collects referenced
// chunk OIDs, and deletes any stored chunks that are no longer referenced
// by any commit. This is analogous to git gc for loose objects.
//
// If dryRun is true, it only reports what would be deleted without actually
// deleting anything.
func RunGC(dryRun bool) error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	s := store.New(gitDir + "/chunked-objects")

	fmt.Fprintf(os.Stderr, "Scanning git history for pointer files...\n")

	referenced, err := collectReferencedOids()
	if err != nil {
		return fmt.Errorf("collecting referenced oids: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Found %d referenced chunk(s) across all pointer files\n", len(referenced))

	// List all stored chunks
	stored, err := s.List()
	if err != nil {
		return fmt.Errorf("listing stored chunks: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Found %d stored chunk(s) on disk\n", len(stored))

	// Find unreferenced chunks
	var unreferenced []string
	for _, oid := range stored {
		if !referenced[oid] {
			unreferenced = append(unreferenced, oid)
		}
	}

	if len(unreferenced) == 0 {
		fmt.Fprintf(os.Stderr, "No unreferenced chunks to remove.\n")
		return nil
	}

	if dryRun {
		fmt.Fprintf(os.Stderr, "Dry run: would remove %d unreferenced chunk(s):\n", len(unreferenced))
		for _, oid := range unreferenced {
			fmt.Fprintf(os.Stderr, "  %s\n", oid)
		}
		return nil
	}

	// Delete unreferenced chunks
	removed := 0
	var errors []string
	for _, oid := range unreferenced {
		if err := s.Delete(oid); err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %v", oid, err))
		} else {
			removed++
		}
	}

	fmt.Fprintf(os.Stderr, "Removed %d/%d unreferenced chunk(s)\n", removed, len(unreferenced))
	if len(errors) > 0 {
		fmt.Fprintf(os.Stderr, "Errors:\n%s\n", strings.Join(errors, "\n"))
		return fmt.Errorf("%d chunk(s) could not be removed", len(errors))
	}

	return nil
}

// collectReferencedOids scans all reachable commits in the git repository,
// finds all pointer files, and collects the chunk OIDs they reference.
// It uses git ls-tree to enumerate blobs per commit and deduplicates
// by blob hash so each unique blob is only read once.
func collectReferencedOids() (map[string]bool, error) {
	referenced := make(map[string]bool)
	seenBlobs := make(map[string]bool)

	// Get all reachable commit hashes
	commitsOutput, err := exec.Command("git", "rev-list", "--all").Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-list --all: %w", err)
	}
	commits := strings.Fields(strings.TrimSpace(string(commitsOutput)))

	fmt.Fprintf(os.Stderr, "Scanning %d commit(s)...\n", len(commits))

	for i, commit := range commits {
		if i > 0 && i%500 == 0 {
			fmt.Fprintf(os.Stderr, "  scanned %d/%d commits...\n", i, len(commits))
		}

		// List all blobs in this commit's tree
		lsTreeOutput, err := exec.Command("git", "ls-tree", "-r", commit).Output()
		if err != nil {
			// Skip commits that can't be listed (e.g., invalid objects)
			continue
		}

		for _, line := range strings.Split(string(lsTreeOutput), "\n") {
			if line == "" {
				continue
			}
			// Format: <mode> <type> <hash>\t<path>
			tabIdx := strings.Index(line, "\t")
			if tabIdx == -1 {
				continue
			}

			metaField := line[:tabIdx]
			fields := strings.Fields(metaField)
			if len(fields) < 3 {
				continue
			}

			objType := fields[1]
			if objType != "blob" {
				continue
			}

			blobHash := fields[2]
			if seenBlobs[blobHash] {
				continue
			}
			seenBlobs[blobHash] = true

			// Read blob content and check if it's a pointer file
			content, err := exec.Command("git", "cat-file", "-p", blobHash).Output()
			if err != nil {
				continue
			}

			if !IsPointer(content) {
				continue
			}

			p, err := pointer.ParseBytes(content)
			if err != nil {
				// Not a valid pointer file, skip
				continue
			}

			for _, oid := range p.ChunkOids {
				referenced[oid] = true
			}
		}
	}

	return referenced, nil
}
