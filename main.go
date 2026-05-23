package main

import (
	"fmt"
	"os"

	"git-chunked-store/cmd"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error

	switch os.Args[1] {
	case "clean":
		err = cmd.RunClean()
	case "smudge":
		err = cmd.RunSmudge()
	case "setup":
		err = cmd.RunSetup()
	case "gc":
		dryRun := false
		for _, arg := range os.Args[2:] {
			if arg == "--dry-run" || arg == "-n" {
				dryRun = true
			}
		}
		err = cmd.RunGC(dryRun)
	case "fsck":
		err = cmd.RunFSCK()
	case "stats":
		err = cmd.RunStats()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "Usage: git-chunked-store <command> [options]\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  clean     Read file from stdin, store chunks, write pointer file to stdout\n")
	fmt.Fprintf(os.Stderr, "  smudge    Read pointer file from stdin, load chunks, write file to stdout\n")
	fmt.Fprintf(os.Stderr, "  setup     Configure git filter and create .gitattributes\n")
	fmt.Fprintf(os.Stderr, "  gc        Remove unreferenced chunks from .git/chunked-objects/\n")
	fmt.Fprintf(os.Stderr, "  fsck      Verify pointer files and chunk store integrity\n")
	fmt.Fprintf(os.Stderr, "  stats     Show chunk store compression and reference metrics\n\n")
	fmt.Fprintf(os.Stderr, "gc options:\n")
	fmt.Fprintf(os.Stderr, "  --dry-run, -n   Show what would be removed without actually removing\n")
	fmt.Fprintf(os.Stderr, "\nExamples:\n")
	fmt.Fprintf(os.Stderr, "  git-chunked-store gc            # Remove unreferenced chunks\n")
	fmt.Fprintf(os.Stderr, "  git-chunked-store gc --dry-run  # Preview what would be removed\n")
	fmt.Fprintf(os.Stderr, "  git-chunked-store fsck          # Verify chunk store integrity\n")
	fmt.Fprintf(os.Stderr, "  git-chunked-store stats         # Show chunk store metrics\n")
}
