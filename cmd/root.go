package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "gh-automagist",
	Short: "Automagically sync local files to GitHub Gists",
	Long: `gh-automagist is an extension for the GitHub CLI that watches local files
and automatically synchronizes their changes seamlessly to GitHub Gists.`,
	Run: func(cmd *cobra.Command, args []string) {
		// By default, show help if no explicit subcommand is passed
		cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
