package cmd

import (
	"fmt"

	"github.com/cli/go-gh/v2"
	"github.com/spf13/cobra"
)

// statusCmd represents the status command
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check the authentication status with GitHub",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Checking GitHub CLI authentication status...")

		// Execute the official GitHub CLI command: `gh auth status`
		stdout, stderr, err := gh.Exec("auth", "status")
		if err != nil {
			return fmt.Errorf("failed to check auth status: %v\n%s", err, stderr.String())
		}

		fmt.Println(stdout.String())
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
