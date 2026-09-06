package cmd

import (
	"fmt"
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

func Execute() {
	// Create a dummy gh command to ensure cobra generates usage like "gh automagist [command]"
	ghCmd := &cobra.Command{
		Use:   "gh",
		Short: "GitHub CLI",
		Run: func(c *cobra.Command, args []string) {
			c.Help()
		},
	}
	ghCmd.AddCommand(rootCmd)

	// Inject "automagist" into os.Args so the dummy gh command properly routes to rootCmd
	if len(os.Args) > 0 {
		os.Args = append([]string{os.Args[0], "automagist"}, os.Args[1:]...)
	}

	if err := ghCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
