package cmd

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	pullForce    bool
	pullYes      bool
	pullDiff     bool
	pullDryRun   bool
	pullNoBackup bool
)

var pullCmd = &cobra.Command{
	Use:   "pull [path]",
	Short: "Pull tracked files from their Gists back to local disk",
	Long: `Fetch each tracked file's Gist content and apply it locally, with backup and safety checks.
If [path] is omitted, all tracked files are processed.`,
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
			for path := range sm.Files {
				targets = append(targets, path)
			}
			sort.Strings(targets)
		}

		gistClient := gist.NewClient()
		var pulled, skipped, blocked, errored int
		for _, path := range targets {
			switch pullFile(sm, gistClient, path) {
			case pullStatusPulled:
				pulled++
			case pullStatusSkipped:
				skipped++
			case pullStatusBlocked:
				blocked++
			case pullStatusError:
				errored++
			}
		}

		fmt.Printf("\nPull complete: %d pulled, %d skipped, %d blocked, %d error(s)\n",
			pulled, skipped, blocked, errored)

		if err := sm.Save(); err != nil {
			return fmt.Errorf("failed to save state: %w", err)
		}
		return nil
	},
}

type pullStatus int

const (
	pullStatusPulled pullStatus = iota
	pullStatusSkipped
	pullStatusBlocked // safety check triggered — user must intervene
	pullStatusError
)

func pullFile(sm *state.Manager, client *gist.Client, absPath string) pullStatus {
	fs := sm.Files[absPath]
	fmt.Printf("\n-> %s\n", displayPath(absPath))

	filename := filepath.Base(absPath)
	remoteContent, remoteUpdatedAt, err := client.FetchFile(fs.GistID, filename)
	if err != nil {
		fmt.Printf("  Error: %v\n", err)
		return pullStatusError
	}

	// Check (c): remote unchanged since last observed sync
	if fs.RemoteUpdatedAt != 0 && remoteUpdatedAt <= fs.RemoteUpdatedAt {
		fmt.Println("  Skipped: remote unchanged since last sync")
		return pullStatusSkipped
	}

	localContent, err := os.ReadFile(absPath)
	if err != nil {
		fmt.Printf("  Error reading local file: %v\n", err)
		return pullStatusError
	}

	// Check (b): remote and local content are byte-identical
	remoteSHA := sha256Hex(remoteContent)
	localSHA := sha256Hex(localContent)
	if remoteSHA == localSHA {
		fmt.Println("  Skipped: content identical (in sync)")
		fs.RemoteUpdatedAt = remoteUpdatedAt
		fs.ContentSHA = remoteSHA
		sm.Files[absPath] = fs
		return pullStatusSkipped
	}

	// Check (a): local mtime ahead of last recorded sync — signals unsynced local edit
	localInfo, err := os.Stat(absPath)
	if err != nil {
		fmt.Printf("  Error stat'ing local file: %v\n", err)
		return pullStatusError
	}
	localMtime := localInfo.ModTime().Unix()
	if localMtime > fs.UpdatedAt && !pullForce {
		fmt.Printf("  LOCAL AHEAD (mtime %s > last-sync %s) — use --force to overwrite\n",
			time.Unix(localMtime, 0).Format(time.RFC3339),
			time.Unix(fs.UpdatedAt, 0).Format(time.RFC3339))
		return pullStatusBlocked
	}

	added, removed := lineDiffSummary(localContent, remoteContent)
	fmt.Printf("  Local:  %d bytes, last synced %s\n", len(localContent),
		time.Unix(fs.UpdatedAt, 0).Format(time.RFC3339))
	fmt.Printf("  Remote: %d bytes, updated %s\n", len(remoteContent),
		time.Unix(remoteUpdatedAt, 0).Format(time.RFC3339))
	fmt.Printf("  Diff:   +%d lines, -%d lines\n", added, removed)

	if pullDiff {
		fmt.Println("  --- Remote content ---")
		fmt.Println(string(remoteContent))
		fmt.Println("  --- End ---")
	}

	if pullDryRun {
		fmt.Println("  Dry-run: no write performed.")
		return pullStatusSkipped
	}

	if !pullYes {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Println("  stdin is not a tty — pass --yes to overwrite non-interactively.")
			return pullStatusBlocked
		}
		fmt.Print("  Proceed with backup and overwrite? [Y/n]: ")
		var response string
		_, _ = fmt.Scanln(&response)
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "" && response != "y" && response != "yes" {
			fmt.Println("  Skipped by user.")
			return pullStatusSkipped
		}
	}

	if !pullNoBackup {
		backupPath := fmt.Sprintf("%s.bak.%s", absPath, time.Now().Format("20060102-150405"))
		if err := os.WriteFile(backupPath, localContent, localInfo.Mode().Perm()); err != nil {
			fmt.Printf("  Error creating backup: %v\n", err)
			return pullStatusError
		}
		fmt.Printf("  [Backup] %s\n", displayPath(backupPath))
	}

	// The suppression marker must land in state.json BEFORE the atomic rename
	// so the daemon reloads it in response to the fsnotify write and treats the
	// PATCH as a pull-echo. resolveDebounce here matches the daemon's env + default
	// path; a running daemon that was itself started with `--debounce=<flag>` and
	// no env var will out-race the pull window — documented as a known edge case.
	effective, _ := resolveDebounce(false, 0, os.Getenv(debounceEnvVar))
	suppressUntil := time.Now().Add(effective + pullSuppressSlack).Unix()
	fs.PullSuppressUntil = suppressUntil
	fs.ContentSHA = remoteSHA
	sm.Files[absPath] = fs
	if err := sm.Save(); err != nil {
		fmt.Printf("  Error saving suppression marker: %v\n", err)
		return pullStatusError
	}

	// Atomic write: <path>.pull.tmp then rename over the original.
	tmpPath := absPath + ".pull.tmp"
	if err := os.WriteFile(tmpPath, remoteContent, localInfo.Mode().Perm()); err != nil {
		fmt.Printf("  Error writing tmp: %v\n", err)
		return pullStatusError
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)
		fmt.Printf("  Error renaming into place: %v\n", err)
		return pullStatusError
	}
	fmt.Printf("  [Write] %d bytes written atomically\n", len(remoteContent))

	fs.UpdatedAt = time.Now().Unix()
	fs.RemoteUpdatedAt = remoteUpdatedAt
	sm.Files[absPath] = fs

	if isMonitorRunning() {
		fmt.Printf("  Note: PATCH will be suppressed until %s (SHA + window match).\n",
			time.Unix(suppressUntil, 0).Format(time.RFC3339))
	}

	return pullStatusPulled
}

// pullSuppressSlack is the safety margin added on top of the resolved debounce
// interval when we set FileState.PullSuppressUntil. Covers fsnotify delivery
// jitter and the gap between pull's Save and the daemon's Load.
const pullSuppressSlack = 2 * time.Second

func sha256Hex(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

// lineDiffSummary returns a rough (added, removed) line count via multiset
// comparison. This is not a real unified diff — a moved line is invisible —
// but it gives a "how big is the change" hint without a diff dependency.
func lineDiffSummary(a, b []byte) (added, removed int) {
	aLines := strings.Split(string(a), "\n")
	bLines := strings.Split(string(b), "\n")
	aCount := make(map[string]int)
	for _, line := range aLines {
		aCount[line]++
	}
	bCount := make(map[string]int)
	for _, line := range bLines {
		bCount[line]++
	}
	for line, c := range bCount {
		if extra := c - aCount[line]; extra > 0 {
			added += extra
		}
	}
	for line, c := range aCount {
		if extra := c - bCount[line]; extra > 0 {
			removed += extra
		}
	}
	return
}

func displayPath(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func init() {
	pullCmd.Flags().BoolVar(&pullForce, "force", false, "Overwrite even if local mtime is newer than last sync")
	pullCmd.Flags().BoolVarP(&pullYes, "yes", "y", false, "Skip the confirmation prompt")
	pullCmd.Flags().BoolVar(&pullDiff, "diff", false, "Print the remote content before overwriting (rough diff, not unified)")
	pullCmd.Flags().BoolVar(&pullDryRun, "dry-run", false, "Show what would happen without writing")
	pullCmd.Flags().BoolVar(&pullNoBackup, "no-backup", false, "Skip creating the .bak file")
	rootCmd.AddCommand(pullCmd)
}
