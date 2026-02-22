package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var removeCmd = &cobra.Command{
	Use:   "remove [path]",
	Short: "Remove a file from monitoring",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path: %w", err)
		}

		sm, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := sm.Load(); err != nil {
			return err
		}

		if _, exists := sm.Files[absPath]; !exists {
			fmt.Printf("File not monitored: %s\n", path)
			return nil
		}

		sm.RemoveTrackedFile(absPath)
		if err := sm.Save(); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		fmt.Printf("Removed %s from monitor.\n", absPath)
		fmt.Println("Note: If 'gh-automagist monitor' is running, please restart it.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(removeCmd)
}
