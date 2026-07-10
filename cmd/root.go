package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "automagist",
	Short: "Automagically sync local files to GitHub Gists",
	Long: `gh-automagist is an extension for the GitHub CLI that watches local files
and automatically synchronizes their changes seamlessly to GitHub Gists.`,
	Run: func(cmd *cobra.Command, args []string) {
		// No subcommand → show help.
		cmd.Help()
	},
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
