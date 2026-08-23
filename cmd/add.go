package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var gistIDFlag string

var addCmd = &cobra.Command{
	Use:   "add [path]",
	Short: "Add a file to be monitored",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := args[0]
		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to resolve absolute path: %w", err)
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			fmt.Printf("File not found: %s\n", path)
			return nil
		}

		sm, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := sm.Load(); err != nil {
			return err
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}

		gistClient := gist.NewClient()
		var finalGistID string
		var remoteUpdatedAt int64

		if gistIDFlag != "" {
			fmt.Printf("Linking %s to Gist %s...\n", path, gistIDFlag)
			remoteUpdatedAt, err = gistClient.UpdateFile(gistIDFlag, absPath, content)
			if err != nil {
				fmt.Println("Failed to link file to Gist. Please check the ID and permissions.")
				return err
			}
			finalGistID = gistIDFlag
		} else {
			fmt.Printf("Creating Gist for %s...\n", path)
			desc := fmt.Sprintf("Automagist: %s", filepath.Base(absPath))
			id, createdAt, err := gistClient.CreateGist(absPath, desc, false)
			if err != nil {
				fmt.Println("Failed to create Gist.")
				return err
			}
			finalGistID = id
			remoteUpdatedAt = createdAt
		}

		// Local and remote are identical the instant `add` finishes, so record
		// both watermarks now. Skipping this is what left every freshly added
		// file permanently flagged as "remote: newer" until its first pull.
		sm.AddTrackedFile(absPath, finalGistID, time.Now().Unix())
		fs := sm.Files[absPath]
		fs.RemoteUpdatedAt = remoteUpdatedAt
		fs.ContentSHA = sha256Hex(content)
		sm.Files[absPath] = fs
		if err := sm.Save(); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}

		fmt.Printf("Added %s to monitor (Gist ID: %s)\n", absPath, finalGistID)
		fmt.Println("Note: If 'gh-automagist monitor' is running, run 'gh automagist restart' to pick up the new file.")

		return nil
	},
}

func init() {
	addCmd.Flags().StringVar(&gistIDFlag, "gist-id", "", "Existing Gist ID to link to")
	rootCmd.AddCommand(addCmd)
}
