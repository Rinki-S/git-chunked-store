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

// gcHookContent is the template for the pre-auto-gc hook.
// %s will be replaced with the absolute path to the git-chunked-store binary.
// This hook runs automatically before 'git gc --auto' executes.
const gcHookContent = `#!/bin/sh
# Managed by git-chunked-store — do not edit manually
# This hook runs chunk garbage collection before git gc --auto.
# For manual gc, run: git-chunked-store gc

%s gc
`

// RunSetup configures git filter settings, creates .gitattributes,
// and installs the pre-auto-gc hook so that chunk garbage collection
// runs automatically as part of git gc.
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

	// Install pre-auto-gc hook
	if err := installGcHook(binPath); err != nil {
		// Not fatal — the hook is a convenience, not a requirement
		fmt.Fprintf(os.Stderr, "warning: could not install gc hook: %v\n", err)
		fmt.Fprintf(os.Stderr, "hint: you can manually run 'git-chunked-store gc' to clean up unreferenced chunks\n")
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

// installGcHook creates the pre-auto-gc hook in .git/hooks/ so that
// 'git-chunked-store gc' runs automatically before 'git gc --auto'.
// If a hook already exists, it is not overwritten to preserve user customizations.
func installGcHook(binPath string) error {
	gitDir, err := findGitDir()
	if err != nil {
		return fmt.Errorf("finding git directory: %w", err)
	}

	hooksDir := filepath.Join(gitDir, "hooks")
	hookPath := filepath.Join(hooksDir, "pre-auto-gc")

	// Check if hook already exists
	if _, err := os.Stat(hookPath); err == nil {
		fmt.Fprintf(os.Stderr, "warning: .git/hooks/pre-auto-gc already exists, skipping hook installation\n")
		fmt.Fprintf(os.Stderr, "hint: add '%s gc' to your pre-auto-gc hook for automatic chunk cleanup\n", binPath)
		return nil
	}

	// Create hooks directory if it doesn't exist
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		return fmt.Errorf("creating hooks directory: %w", err)
	}

	// Write hook script
	hookScript := fmt.Sprintf(gcHookContent, binPath)
	if err := os.WriteFile(hookPath, []byte(hookScript), 0755); err != nil {
		return fmt.Errorf("writing gc hook: %w", err)
	}

	fmt.Fprintf(os.Stderr, "Installed pre-auto-gc hook to %s\n", hookPath)
	fmt.Fprintf(os.Stderr, "  'git gc --auto' will now run chunk garbage collection automatically\n")
	return nil
}
