package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// gitattributesContent is the default .gitattributes content that maps
// common binary file types to the chunked filter.
const gitattributesContent = `# Managed by git-chunked-store — do not edit manually
# Use 'git-chunked-store setup' to regenerate
*.bin filter=chunked
*.zip filter=chunked
*.tar filter=chunked
*.gz filter=chunked
*.mp4 filter=chunked
*.avi filter=chunked
*.pdf filter=chunked
*.docx filter=chunked
*.xlsx filter=chunked
*.pptx filter=chunked
*.exe filter=chunked
*.dll filter=chunked
*.so filter=chunked
*.dylib filter=chunked
*.iso filter=chunked
*.dmg filter=chunked
`

// RunSetup configures git filter settings and creates .gitattributes.
// It sets up the clean and smudge filters so that git automatically
// applies chunked storage to files matching the .gitattributes patterns.
func RunSetup() error {
	// Determine the binary path from the current executable
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("determining executable path: %w", err)
	}

	// Resolve symlinks to get the real path
	binPath, err = filepath.EvalSymlinks(binPath)
	if err != nil {
		return fmt.Errorf("resolving executable path: %w", err)
	}

	// Configure git filter settings
	filterClean := binPath + " clean"
	filterSmudge := binPath + " smudge"

	commands := []struct {
		args []string
		desc string
	}{
		{[]string{"config", "filter.chunked.clean", filterClean}, "setting filter.chunked.clean"},
		{[]string{"config", "filter.chunked.smudge", filterSmudge}, "setting filter.chunked.smudge"},
		{[]string{"config", "filter.chunked.required", "true"}, "setting filter.chunked.required"},
	}

	for _, cmd := range commands {
		if err := exec.Command("git", cmd.args...).Run(); err != nil {
			return fmt.Errorf("%s: %w", cmd.desc, err)
		}
	}

	// Create .gitattributes if it doesn't exist
	gitattributesPath := ".gitattributes"

	if _, err := os.Stat(gitattributesPath); os.IsNotExist(err) {
		if err := os.WriteFile(gitattributesPath, []byte(gitattributesContent), 0644); err != nil {
			return fmt.Errorf("creating .gitattributes: %w", err)
		}
	} else {
		fmt.Fprintf(os.Stderr, "warning: .gitattributes already exists, skipping creation\n")
		fmt.Fprintf(os.Stderr, "hint: you may need to manually add 'filter=chunked' rules to .gitattributes\n")
	}

	return nil
}
