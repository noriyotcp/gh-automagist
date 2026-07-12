package pager

import (
	"io"
	"os"
	"os/exec"

	"golang.org/x/term"
)

// IsTTY reports whether os.Stdout is a terminal. Callers use this to decide
// upstream formatting (e.g. force-color-when-piping-to-pager) — Run itself
// makes the same check internally to choose pager vs direct-stdout.
func IsTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// Run calls fn with a writer that pipes to the user's pager
// ($PAGER, fallback `less -FRX`). When stdout is not a tty, or noPager
// is true, fn writes directly to os.Stdout.
//
// less -FRX: -F quits if content fits one screen (so short output stays
// inline), -R passes ANSI color escapes through, -X keeps the screen
// contents visible after quit.
func Run(noPager bool, fn func(io.Writer) error) error {
	if noPager || !term.IsTerminal(int(os.Stdout.Fd())) {
		return fn(os.Stdout)
	}

	pagerCmd := os.Getenv("PAGER")
	if pagerCmd == "" {
		pagerCmd = "less -FRX"
	}
	cmd := exec.Command("sh", "-c", pagerCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	fnErr := fn(stdin)
	stdin.Close()
	waitErr := cmd.Wait()
	if fnErr != nil {
		return fnErr
	}
	return waitErr
}
