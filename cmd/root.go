package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Set by SetVersionInfo at process start (main.go passes goreleaser-injected
// values). Read by cmd/status.go for the daemon-vs-binary mismatch check and
// by cmd/monitor.go when recording MonitorInfo.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

var rootCmd = newRootCmd()

// The groups have to exist before any subcommand carrying a GroupID is added,
// or cobra's AddCommand panics. Package-level variable initialization runs
// ahead of every init(), so registering them here — rather than in an init() of
// this file — keeps the per-command init() registrations safe regardless of the
// order the compiler happens to walk the files in.
func newRootCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "automagist",
		Short: "Automagically sync local files to GitHub Gists",
		Long: `gh-automagist is an extension for the GitHub CLI that watches local files
and automatically synchronizes their changes seamlessly to GitHub Gists.`,
		Run: func(cmd *cobra.Command, args []string) {
			// No subcommand → show help.
			cmd.Help()
		},
	}
	c.AddGroup(
		&cobra.Group{ID: "interactive", Title: "Interactive:"},
		&cobra.Group{ID: "tracking", Title: "Tracking files:"},
		&cobra.Group{ID: "sync", Title: "Syncing content:"},
		&cobra.Group{ID: "daemon", Title: "Background daemon:"},
	)
	return c
}

// SetVersionInfo wires the goreleaser-injected build metadata into both the
// package-scope vars (used at runtime by status/monitor) and Cobra's Version
// field (which auto-adds the --version flag).
func SetVersionInfo(v, c, d string) {
	Version, Commit, Date = v, c, d
	rootCmd.Version = fmt.Sprintf("%s (commit %s, built %s)", v, c, d)
}

// newGhCmd wraps rootCmd in a dummy gh parent so cobra renders usage as
// "gh automagist [command]" rather than naming the extension binary.
func newGhCmd() *cobra.Command {
	ghCmd := &cobra.Command{
		Use:   "gh",
		Short: "GitHub CLI",
		Run: func(c *cobra.Command, args []string) {
			c.Help()
		},
	}
	ghCmd.AddCommand(rootCmd)
	return ghCmd
}

// ghArgs restates the process arguments the way the dummy gh parent expects:
// gh runs the extension binary directly, so the "automagist" the usage text
// advertises is never in os.Args.
func ghArgs(osArgs []string) []string {
	args := []string{"automagist"}
	if len(osArgs) > 1 {
		args = append(args, osArgs[1:]...)
	}
	return args
}

// run is Execute without the exit, so a test can read everything a failing
// command emits. A nil writer leaves cobra's own default stream in place,
// which is what keeps the usage block on stderr for the real process.
func run(osArgs []string, out, errOut io.Writer) int {
	ghCmd := newGhCmd()
	ghCmd.SetArgs(ghArgs(osArgs))
	if out != nil {
		ghCmd.SetOut(out)
	}
	if errOut != nil {
		ghCmd.SetErr(errOut)
	}

	// Cobra has already printed the error — with its "Error:" prefix, and the
	// usage block unless the command silenced it — by the time Execute
	// returns. Printing err again here is what put the message on stderr twice.
	if err := ghCmd.Execute(); err != nil {
		return 1
	}
	return 0
}

func Execute() {
	if code := run(os.Args, nil, nil); code != 0 {
		os.Exit(code)
	}
}
