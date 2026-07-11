package cmd

import (
	"fmt"
	"sort"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/notify"
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

		statuses := notify.Detect(sm, gist.NewClient())
		printFetchResult(statuses)
		return nil
	},
}

// printFetchResult renders the fetch command's output from a notify.Detect
// result. Groups per-Gist for the "N Gist(s)" summary and lists per-file
// entries whose Gist has newer content.
func printFetchResult(statuses []notify.FileStatus) {
	byGist := groupByGist(statuses)
	gistIDs := make([]string, 0, len(byGist))
	for id := range byGist {
		gistIDs = append(gistIDs, id)
	}
	sort.Strings(gistIDs)

	fmt.Printf("Checking %d Gist(s) for remote changes...\n\n", len(gistIDs))

	var newerFiles []string
	for _, gistID := range gistIDs {
		group := byGist[gistID]
		// All files in a Gist share the same fetch error and RemoteUpdatedAt,
		// so we can inspect the first entry.
		head := group[0]
		if head.Err != nil {
			fmt.Printf("  %s: error — %v\n", truncateGistID(gistID), head.Err)
			continue
		}

		var groupNewer []string
		for _, s := range group {
			if s.RemoteNewer {
				groupNewer = append(groupNewer, s.Path)
			}
		}
		if len(groupNewer) == 0 {
			fmt.Printf("  %s: in sync\n", truncateGistID(gistID))
			continue
		}

		fmt.Printf("  %s: newer available (remote updated %s)\n",
			truncateGistID(gistID),
			time.Unix(head.RemoteUpdatedAt, 0).Format(time.RFC3339))
		newerFiles = append(newerFiles, groupNewer...)
	}

	fmt.Println()
	if len(newerFiles) == 0 {
		fmt.Println("All tracked files are in sync with their Gists.")
		return
	}

	sort.Strings(newerFiles)
	fmt.Printf("%d file(s) with newer remote content:\n", len(newerFiles))
	for _, p := range newerFiles {
		fmt.Printf("  %s\n", displayPath(p))
	}
	fmt.Println()
	fmt.Println("Run `gh automagist pull <path>` to review and apply, or `gh automagist pull` for all.")
}

// groupByGist buckets FileStatus entries by their GistID, preserving the
// per-Gist sharing of fetch error / RemoteUpdatedAt.
func groupByGist(statuses []notify.FileStatus) map[string][]notify.FileStatus {
	out := make(map[string][]notify.FileStatus)
	for _, s := range statuses {
		out[s.GistID] = append(out[s.GistID], s)
	}
	return out
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
