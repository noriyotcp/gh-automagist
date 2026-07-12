package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/diff"
	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/notify"
	"github.com/noriyo_tcp/gh-automagist/pkg/pager"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var (
	fetchDiff    bool
	fetchNoPager bool
)

var fetchCmd = &cobra.Command{
	Use:   "fetch [path]",
	Short: "Check tracked Gists for remote changes, optionally showing content diffs",
	Long: `Without --diff, reports which tracked files may have newer remote content
based on Gist commit timestamps (no content is downloaded).

With --diff, downloads the content of every file whose remote is newer and
prints a unified diff (local vs remote) through the pager. Pass a path to
diff a single tracked file instead of all newer ones.`,
	Args: cobra.MaximumNArgs(1),
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

		client := gist.NewClient()

		switch {
		case !fetchDiff && len(args) == 0:
			statuses := notify.Detect(sm, client)
			printFetchResult(os.Stdout, statuses)
			return nil
		case !fetchDiff && len(args) == 1:
			return fmt.Errorf("--diff is required to inspect a specific file")
		case fetchDiff && len(args) == 0:
			return runFetchDiffAll(sm, client, fetchNoPager)
		default: // fetchDiff && len(args) == 1
			absPath, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("failed to resolve path: %w", err)
			}
			fs, ok := sm.Files[absPath]
			if !ok {
				return fmt.Errorf("file not tracked: %s", absPath)
			}
			return runFetchDiffSingle(absPath, fs.GistID, client, fetchNoPager)
		}
	},
}

// printFetchResult renders the fetch command's summary from a notify.Detect
// result. Groups per-Gist for the "N Gist(s)" summary and lists per-file
// entries whose Gist has newer content.
func printFetchResult(w io.Writer, statuses []notify.FileStatus) {
	byGist := groupByGist(statuses)
	gistIDs := make([]string, 0, len(byGist))
	for id := range byGist {
		gistIDs = append(gistIDs, id)
	}
	sort.Strings(gistIDs)

	fmt.Fprintf(w, "Checking %d Gist(s) for remote changes...\n\n", len(gistIDs))

	var newerFiles []string
	for _, gistID := range gistIDs {
		group := byGist[gistID]
		head := group[0]
		if head.Err != nil {
			fmt.Fprintf(w, "  %s: error — %v\n", truncateGistID(gistID), head.Err)
			continue
		}

		var groupNewer []string
		for _, s := range group {
			if s.RemoteNewer {
				groupNewer = append(groupNewer, s.Path)
			}
		}
		if len(groupNewer) == 0 {
			fmt.Fprintf(w, "  %s: in sync\n", truncateGistID(gistID))
			continue
		}

		fmt.Fprintf(w, "  %s: newer available (remote updated %s)\n",
			truncateGistID(gistID),
			time.Unix(head.RemoteUpdatedAt, 0).Format(time.RFC3339))
		newerFiles = append(newerFiles, groupNewer...)
	}

	fmt.Fprintln(w)
	if len(newerFiles) == 0 {
		fmt.Fprintln(w, "All tracked files are in sync with their Gists.")
		return
	}

	sort.Strings(newerFiles)
	fmt.Fprintf(w, "%d file(s) with newer remote content:\n", len(newerFiles))
	for _, p := range newerFiles {
		fmt.Fprintf(w, "  %s\n", displayPath(p))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run `gh automagist pull <path>` to review and apply, or `gh automagist pull` for all.")
}

// runFetchDiffAll prints the meta summary followed by per-file unified diffs
// for every file whose Gist has newer content. Buckets per Gist so each Gist
// is fetched exactly once.
func runFetchDiffAll(sm *state.Manager, client *gist.Client, noPagerFlag bool) error {
	statuses := notify.Detect(sm, client)

	perGist := make(map[string][]notify.FileStatus)
	for _, s := range statuses {
		if s.RemoteNewer && s.Err == nil {
			perGist[s.GistID] = append(perGist[s.GistID], s)
		}
	}

	colorMode := colorForOutput(noPagerFlag)

	return pager.Run(noPagerFlag, func(w io.Writer) error {
		printFetchResult(w, statuses)
		if len(perGist) == 0 {
			return nil
		}

		gistIDs := make([]string, 0, len(perGist))
		for id := range perGist {
			gistIDs = append(gistIDs, id)
		}
		sort.Strings(gistIDs)

		for _, gistID := range gistIDs {
			group := perGist[gistID]
			sort.Slice(group, func(i, j int) bool { return group[i].Path < group[j].Path })

			allFiles, _, err := client.FetchAllFiles(gistID)
			if err != nil {
				fmt.Fprintf(w, "=== Gist %s ===\n", truncateGistID(gistID))
				fmt.Fprintf(w, "Error fetching content: %v\n\n", err)
				continue
			}

			for _, f := range group {
				filename := filepath.Base(f.Path)
				remoteContent, ok := allFiles[filename]
				if !ok {
					fmt.Fprintf(w, "=== %s ===\n", displayPath(f.Path))
					fmt.Fprintf(w, "Error: file %q not in Gist\n\n", filename)
					continue
				}
				if err := writeFileDiff(w, f.Path, remoteContent, colorMode); err != nil {
					fmt.Fprintf(w, "Error: %v\n\n", err)
				}
			}
		}
		return nil
	})
}

// runFetchDiffSingle prints the unified diff for one tracked file.
func runFetchDiffSingle(absPath, gistID string, client *gist.Client, noPagerFlag bool) error {
	allFiles, _, err := client.FetchAllFiles(gistID)
	if err != nil {
		return fmt.Errorf("failed to fetch gist %s: %w", truncateGistID(gistID), err)
	}
	filename := filepath.Base(absPath)
	remoteContent, ok := allFiles[filename]
	if !ok {
		return fmt.Errorf("file %q not found in gist %s", filename, truncateGistID(gistID))
	}

	colorMode := colorForOutput(noPagerFlag)
	return pager.Run(noPagerFlag, func(w io.Writer) error {
		return writeFileDiff(w, absPath, remoteContent, colorMode)
	})
}

// writeFileDiff writes a `=== path ===` header and the unified diff of local
// vs remote to w. In-sync files get "No diff (in sync)." instead of an empty
// diff block.
func writeFileDiff(w io.Writer, localPath string, remoteContent []byte, mode diff.ColorMode) error {
	fmt.Fprintf(w, "=== %s ===\n", displayPath(localPath))

	localContent, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local %s: %w", localPath, err)
	}

	// Write to a shared tmp dir with short relative names so git's diff
	// headers read as `a/local` vs `b/remote` instead of full tmp paths.
	tmpDir, err := os.MkdirTemp("", "gh-automagist-diff-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	if err := os.WriteFile(filepath.Join(tmpDir, "local"), localContent, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "remote"), remoteContent, 0644); err != nil {
		return err
	}

	out, err := diff.Unified(tmpDir, "local", "remote", mode)
	if err != nil {
		return err
	}
	if len(out) == 0 {
		fmt.Fprintln(w, "No diff (in sync).")
	} else {
		w.Write(out)
	}
	fmt.Fprintln(w)
	return nil
}

// colorForOutput picks the diff color mode. When output actually goes to a
// terminal via the pager, force color-always so ANSI escapes survive the pipe
// (less -R passes them through). Otherwise plain output.
func colorForOutput(noPagerFlag bool) diff.ColorMode {
	if pager.IsTTY() && !noPagerFlag {
		return diff.ColorAlways
	}
	return diff.ColorNever
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
	fetchCmd.Flags().BoolVar(&fetchDiff, "diff", false, "Fetch content and show a unified diff (local vs remote)")
	fetchCmd.Flags().BoolVar(&fetchNoPager, "no-pager", false, "Skip the pager even when stdout is a terminal")
	rootCmd.AddCommand(fetchCmd)
}
