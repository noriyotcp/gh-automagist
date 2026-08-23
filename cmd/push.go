package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var (
	pushForce  bool
	pushDryRun bool
)

var pushCmd = &cobra.Command{
	Use:   "push [path]",
	Short: "Push tracked files up to their Gists",
	Long: `Send local changes to each tracked file's Gist, skipping files that already match.
If [path] is omitted, all tracked files are processed.

A file is only pushed when the Gist still holds the content we last synced —
otherwise the remote moved on its own and the direction is a decision for you,
not for this command.`,
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

		var targets []string
		if len(args) == 1 {
			absPath, err := filepath.Abs(args[0])
			if err != nil {
				return fmt.Errorf("failed to resolve absolute path: %w", err)
			}
			if _, ok := sm.Files[absPath]; !ok {
				return fmt.Errorf("file not tracked: %s", absPath)
			}
			targets = []string{absPath}
		} else {
			targets = trackedPaths(sm)
		}

		summary := pushTargets(sm, gist.NewClient(), targets, pushOptions{
			force:  pushForce,
			dryRun: pushDryRun,
			logf: func(format string, args ...any) {
				fmt.Printf(format+"\n", args...)
			},
		})

		fmt.Printf("\nPush complete: %s\n", summary)
		return nil
	},
}

// gistPusher is the subset of gist.Client that pushTargets needs, so tests can
// script remote content and PATCH results without touching the network.
type gistPusher interface {
	FetchAllFiles(gistID string) (files map[string][]byte, updatedAt int64, err error)
	UpdateFile(gistID string, localFilePath string, content []byte) (updatedAt int64, err error)
}

type pushOptions struct {
	force  bool
	dryRun bool
	logf   func(format string, args ...any)
}

type pushSummary struct {
	pushed     int
	backfilled int
	skipped    int
	blocked    int
	errored    int
}

func (s pushSummary) String() string {
	return fmt.Sprintf("%d pushed, %d backfilled, %d skipped, %d blocked, %d error(s)",
		s.pushed, s.backfilled, s.skipped, s.blocked, s.errored)
}

type pushDecision int

const (
	pushDecisionPush pushDecision = iota
	pushDecisionSkip
	pushDecisionBlock // remote moved on its own — a human has to pick a direction
)

// pushGate decides whether local content may overwrite the Gist, by comparing
// content rather than timestamps. A Gist's updated_at covers every file in it,
// so five dotfiles sharing one Gist would block each other on a timestamp rule
// even when nothing touched them.
//
//	remoteSHA == localSHA            skip   nothing to send
//	force                            push   caller accepts the overwrite
//	contentSHA == ""                 block  no baseline, so no proof the Gist is ours to overwrite
//	remoteSHA == contentSHA          push   the Gist still holds what we last synced
//	otherwise                        block  the Gist moved since our last sync
//
// Identical content is checked before force: a forced PATCH of bytes the Gist
// already has is a wasted API call, not an override.
func pushGate(remoteSHA, localSHA, contentSHA string, force bool) (pushDecision, string) {
	switch {
	case remoteSHA == localSHA:
		return pushDecisionSkip, "content identical (in sync)"
	case force:
		return pushDecisionPush, "forced"
	case contentSHA == "":
		return pushDecisionBlock, "no sync baseline recorded — pull first, or use --force"
	case remoteSHA == contentSHA:
		return pushDecisionPush, "remote still matches our last sync"
	default:
		return pushDecisionBlock, "remote changed since our last sync — pull first, or use --force"
	}
}

// pushResult carries what the second pass needs to stamp state, once the whole
// Gist has been processed and its final updated_at is known.
type pushResult struct {
	path     string
	localSHA string
	pushed   bool
}

// pushTargets processes the given tracked paths, one Gist at a time. Files
// sharing a Gist cost one fetch between them, and their state is stamped with
// that Gist's newest observed timestamp — including one our own PATCH just
// produced, which is what stops a sibling push from making an untouched file
// look remotely changed.
func pushTargets(sm *state.Manager, client gistPusher, targets []string, opts pushOptions) pushSummary {
	var summary pushSummary

	byGist := make(map[string][]string)
	for _, path := range targets {
		byGist[sm.Files[path].GistID] = append(byGist[sm.Files[path].GistID], path)
	}
	gistIDs := make([]string, 0, len(byGist))
	for gistID := range byGist {
		gistIDs = append(gistIDs, gistID)
	}
	sort.Strings(gistIDs)

	for _, gistID := range gistIDs {
		paths := byGist[gistID]
		sort.Strings(paths)

		remoteFiles, gistUpdatedAt, err := client.FetchAllFiles(gistID)
		if err != nil {
			opts.logf("  [Error] gist %s: %v", gistID, err)
			summary.errored += len(paths)
			continue
		}

		latestUpdatedAt := gistUpdatedAt
		var results []pushResult

		for _, path := range paths {
			fs := sm.Files[path]
			localContent, err := os.ReadFile(path)
			if err != nil {
				opts.logf("  [Error] %s: %v", displayPath(path), err)
				summary.errored++
				continue
			}
			remoteContent, ok := remoteFiles[filepath.Base(path)]
			if !ok {
				opts.logf("  [Error] %s: not present in gist %s", displayPath(path), gistID)
				summary.errored++
				continue
			}

			localSHA := sha256Hex(localContent)
			decision, reason := pushGate(sha256Hex(remoteContent), localSHA, fs.ContentSHA, opts.force)
			switch decision {
			case pushDecisionBlock:
				opts.logf("  [Blocked] %s: %s", displayPath(path), reason)
				summary.blocked++
			case pushDecisionSkip:
				results = append(results, pushResult{path: path, localSHA: localSHA})
			case pushDecisionPush:
				if opts.dryRun {
					opts.logf("  [Dry-run] %s: would push %d bytes (%s)", displayPath(path), len(localContent), reason)
					results = append(results, pushResult{path: path, localSHA: localSHA, pushed: true})
					summary.pushed++
					continue
				}
				updatedAt, err := client.UpdateFile(gistID, path, localContent)
				if err != nil {
					opts.logf("  [Error] %s: failed to update gist: %v", displayPath(path), err)
					summary.errored++
					continue
				}
				opts.logf("  [Pushed] %s: %d bytes (%s)", displayPath(path), len(localContent), reason)
				if updatedAt > latestUpdatedAt {
					latestUpdatedAt = updatedAt
				}
				results = append(results, pushResult{path: path, localSHA: localSHA, pushed: true})
				summary.pushed++
			}
		}

		now := time.Now().Unix()
		changed := false
		for _, r := range results {
			fs, stillTracked := sm.Files[r.path]
			if !stillTracked {
				continue
			}
			var updated state.FileState
			if r.pushed {
				updated = recordSyncSuccess(fs, r.localSHA, latestUpdatedAt, now)
			} else {
				updated = recordObservedInSync(fs, r.localSHA, latestUpdatedAt)
				if updated == fs {
					summary.skipped++
				} else {
					summary.backfilled++
					if opts.dryRun {
						opts.logf("  [Dry-run] %s: would record digest and remote timestamp", displayPath(r.path))
					} else {
						opts.logf("  [Backfill] %s: recorded digest and remote timestamp", displayPath(r.path))
					}
				}
			}
			if updated == fs {
				continue
			}
			changed = true
			if !opts.dryRun {
				sm.Files[r.path] = updated
			}
		}
		if changed && !opts.dryRun {
			if err := sm.Save(); err != nil {
				opts.logf("  [Error] failed to save state for gist %s: %v", gistID, err)
			}
		}
	}

	return summary
}

// recordObservedInSync stamps a file we just confirmed byte-identical to its
// Gist. UpdatedAt is deliberately left alone: no bytes moved, and pull reads
// that field as "when we last transferred content".
func recordObservedInSync(fs state.FileState, contentSHA string, remoteUpdatedAt int64) state.FileState {
	fs.ContentSHA = contentSHA
	fs.RemoteUpdatedAt = remoteUpdatedAt
	return fs
}

func trackedPaths(sm *state.Manager) []string {
	paths := make([]string, 0, len(sm.Files))
	for path := range sm.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func init() {
	pushCmd.Flags().BoolVar(&pushForce, "force", false, "Push even when the Gist changed since our last sync")
	pushCmd.Flags().BoolVar(&pushDryRun, "dry-run", false, "Show what would happen without sending anything")
	rootCmd.AddCommand(pushCmd)
}
