// Package cmd implements the gc subcommand for garbage-collecting
// unreferenced chunks from the chunked-objects store.
package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
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

	var blobHashes []string

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

		for line := range strings.SplitSeq(string(lsTreeOutput), "\n") {
			if line == "" {
				continue
			}
			// Format: <mode> <type> <hash>\t<path>
			before, _, ok := strings.Cut(line, "\t")
			if !ok {
				continue
			}

			metaField := before
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
			blobHashes = append(blobHashes, blobHash)
		}
	}

	fmt.Fprintf(os.Stderr, "Reading %d unique blob(s) with git cat-file --batch...\n", len(blobHashes))

	if err := collectPointerOidsFromBlobs(blobHashes, referenced); err != nil {
		return nil, err
	}

	return referenced, nil
}

func collectPointerOidsFromBlobs(blobHashes []string, referenced map[string]bool) error {
	if len(blobHashes) == 0 {
		return nil
	}

	cmd := exec.Command("git", "cat-file", "--batch")

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("opening cat-file stdin: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("opening cat-file stdout: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting git cat-file --batch: %w", err)
	}

	go func() {
		defer stdin.Close()

		for _, blobHash := range blobHashes {
			fmt.Fprintln(stdin, blobHash)
		}
	}()

	reader := bufio.NewReader(stdout)

	for range blobHashes {
		header, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("reading cat-file header: %w", err)
		}

		fields := strings.Fields(header)
		if len(fields) != 3 {
			return fmt.Errorf("invalid cat-file header: %q", strings.TrimSpace(header))
		}

		objType := fields[1]
		size, err := strconv.ParseInt(fields[2], 10, 64)
		if err != nil {
			return fmt.Errorf("invalid cat-file object size %q: %w", fields[2], err)
		}

		content := make([]byte, size)
		if _, err := io.ReadFull(reader, content); err != nil {
			return fmt.Errorf("reading cat-file object content: %w", err)
		}

		if _, err := reader.ReadByte(); err != nil {
			return fmt.Errorf("reading cat-file object separator: %w", err)
		}

		if objType != "blob" {
			continue
		}

		if !IsPointer(content) {
			continue
		}

		p, err := pointer.Parse(bytes.NewReader(content))
		if err != nil {
			continue
		}

		for _, oid := range p.ChunkOids {
			referenced[oid] = true
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git cat-file --batch: %w", err)
	}

	return nil
}
