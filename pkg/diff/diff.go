package diff

import (
	"errors"
	"fmt"
	"os/exec"
)

type ColorMode int

const (
	ColorAuto   ColorMode = iota // git decides based on stdout isatty
	ColorAlways                  // --color=always
	ColorNever                   // --no-color
)

// Unified returns the unified diff of aPath vs bPath as produced by
// `git diff --no-index`. When cwd is non-empty, git runs from that directory
// — useful for keeping the diff headers short (relative paths instead of
// long temp paths). Empty output with nil error means the files are
// identical (exit 0). Non-empty output with nil error means there is a
// diff (exit 1, git's normal signal for "files differ"). A non-nil error
// means git failed for a reason other than "files differ".
func Unified(cwd, aPath, bPath string, mode ColorMode) ([]byte, error) {
	args := []string{"diff", "--no-index"}
	switch mode {
	case ColorAlways:
		args = append(args, "--color=always")
	case ColorNever:
		args = append(args, "--no-color")
	}
	args = append(args, aPath, bPath)

	cmd := exec.Command("git", args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}
	if exitErr.ExitCode() == 1 {
		// Exit 1 is ambiguous: "files differ" (stdout has diff) OR "can't open
		// a path" (stdout empty, stderr has "error: Could not access ..."). Only
		// the former is normal.
		if len(out) == 0 && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("git diff failed: %s", exitErr.Stderr)
		}
		return out, nil
	}
	return nil, fmt.Errorf("git diff failed (exit %d): %s", exitErr.ExitCode(), exitErr.Stderr)
}
