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
	fmt.Fprintf(os.Stderr, "Usage: git-chunked-store <command>\n\n")
	fmt.Fprintf(os.Stderr, "Commands:\n")
	fmt.Fprintf(os.Stderr, "  clean   Read file from stdin, store chunks, write pointer file to stdout\n")
	fmt.Fprintf(os.Stderr, "  smudge  Read pointer file from stdin, load chunks, write file to stdout\n")
	fmt.Fprintf(os.Stderr, "  setup   Configure git filter and create .gitattributes\n")
}
