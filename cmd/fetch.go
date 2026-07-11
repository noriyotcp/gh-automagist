package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Check tracked Gists for remote changes without applying them",
	Long: `Check every tracked Gist's latest commit timestamp against the last observed state
and report which tracked files may have newer remote content. Does not download
file contents and does not modify anything on disk.

Semantically equivalent to git fetch: see what's new, apply later with pull.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := sm.Load(); err != nil {
			return err
		}
		if len(sm.Files) == 0 {
			fmt.Println("No files are currently tracked.")
			return nil
		}

		// Dedup by Gist ID — multiple tracked files may share one Gist.
		gistToFiles := make(map[string][]string)
		for absPath, fs := range sm.Files {
			gistToFiles[fs.GistID] = append(gistToFiles[fs.GistID], absPath)
		}
		gistIDs := make([]string, 0, len(gistToFiles))
		for id := range gistToFiles {
			gistIDs = append(gistIDs, id)
		}
		sort.Strings(gistIDs)

		client := gist.NewClient()
		fmt.Printf("Checking %d Gist(s) for remote changes...\n\n", len(gistIDs))

		var newerFiles []string

		for _, gistID := range gistIDs {
			updatedAt, err := client.FetchGistMeta(gistID)
			if err != nil {
				fmt.Printf("  %s: error — %v\n", truncateGistID(gistID), err)
				continue
			}

			files := gistToFiles[gistID]
			sort.Strings(files)

			// Per-file comparison: a file whose RemoteUpdatedAt lags the current
			// Gist commit timestamp may have newer content available. Handled
			// per-file (not per-Gist) because sibling files in the same Gist
			// can be at different sync states.
			var filesInGistNewer []string
			for _, path := range files {
				if updatedAt > sm.Files[path].RemoteUpdatedAt {
					filesInGistNewer = append(filesInGistNewer, path)
				}
			}

			if len(filesInGistNewer) == 0 {
				fmt.Printf("  %s: in sync\n", truncateGistID(gistID))
				continue
			}

			fmt.Printf("  %s: newer available (remote updated %s)\n",
				truncateGistID(gistID),
				time.Unix(updatedAt, 0).Format(time.RFC3339))
			newerFiles = append(newerFiles, filesInGistNewer...)
		}

		fmt.Println()
		if len(newerFiles) == 0 {
			fmt.Println("All tracked files are in sync with their Gists.")
			return nil
		}

		fmt.Printf("%d file(s) with newer remote content:\n", len(newerFiles))
		for _, p := range newerFiles {
			fmt.Printf("  %s\n", displayPath(p))
		}
		fmt.Println()
		fmt.Println("Run `gh automagist pull <path>` to review and apply, or `gh automagist pull` for all.")
		return nil
	},
}

func truncateGistID(id string) string {
	if len(id) > 8 {
		return id[:8] + "..."
	}
	return id
}

func init() {
	rootCmd.AddCommand(fetchCmd)
}
