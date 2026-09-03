package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var (
	gistIDFlag     string
	addForce       bool
	addAdoptRemote bool
)

var addCmd = &cobra.Command{
	Use:   "add [path]",
	Short: "Add a file to be monitored",
	Long: `Register a local file for monitoring, either as a new Gist or linked to an existing one.

With --gist-id, the Gist is read before anything is written. If it already holds
a file of the same name with different content, nothing is sent and nothing is
tracked — the direction is a decision for you, made with --adopt-remote or
--force.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if addForce && addAdoptRemote {
			return errors.New("--force and --adopt-remote pick opposite directions; pass only one")
		}
		if gistIDFlag == "" && (addForce || addAdoptRemote) {
			return errors.New("--force and --adopt-remote only apply to --gist-id")
		}

		// Past this point every error is a runtime outcome (a blocked link, a
		// failed fetch), not a usage mistake, so cobra should not answer it
		// with the flag list.
		cmd.SilenceUsage = true

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

		if gistIDFlag != "" {
			fmt.Printf("Linking %s to Gist %s...\n", path, gistIDFlag)
			if err := linkExistingGist(sm, gistClient, absPath, gistIDFlag, content, addOptions{
				force:       addForce,
				adoptRemote: addAdoptRemote,
				logf: func(format string, args ...any) {
					fmt.Printf(format+"\n", args...)
				},
			}); err != nil {
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

			// Local and remote are identical the instant `add` finishes, so record
			// both watermarks now. Skipping this is what left every freshly added
			// file permanently flagged as "remote: newer" until its first pull.
			trackSynced(sm, absPath, id, sha256Hex(content), createdAt)
			if err := sm.Save(); err != nil {
				return fmt.Errorf("failed to save state: %w", err)
			}
		}

		fmt.Printf("Added %s to monitor (Gist ID: %s)\n", absPath, finalGistID)
		fmt.Println("Note: If 'gh-automagist monitor' is running, run 'gh automagist restart' to pick up the new file.")

		return nil
	},
}

// gistLinker is the subset of gist.Client that the --gist-id path needs, so
// tests can script remote content and PATCH results without touching the
// network.
type gistLinker interface {
	FetchAllFiles(gistID string) (files map[string][]byte, updatedAt int64, err error)
	UpdateFile(gistID string, localFilePath string, content []byte) (updatedAt int64, err error)
}

type addOptions struct {
	force       bool
	adoptRemote bool
	logf        func(format string, args ...any)
}

type addDecision int

const (
	addDecisionPush  addDecision = iota // send local content up
	addDecisionLink                     // record state only; no transfer in either direction
	addDecisionAdopt                    // bring remote content down over the local file
	addDecisionBlock                    // the two sides differ and no direction was chosen
)

// addGate decides how `add --gist-id` reconciles a local file with the Gist it
// is being linked to. remoteSHA is empty when the Gist holds no file of that
// name.
//
//	no file of that name    push   nothing to overwrite; this is the plain link case
//	remoteSHA == localSHA   link   both sides already agree, so no API write
//	force                   push   caller accepts overwriting the Gist
//	adoptRemote             adopt  caller accepts overwriting the local file
//	otherwise               block  a real divergence — the caller picks a side
//
// Identical content is checked before force so a forced link of bytes the Gist
// already has costs no API write. Before this gate existed, --gist-id PATCHed
// unconditionally: linking a second machine whose copy was stale silently
// overwrote the newer content already in the Gist.
func addGate(remoteSHA, localSHA string, force, adoptRemote bool) (addDecision, string) {
	switch {
	case remoteSHA == "" && adoptRemote:
		return addDecisionBlock, "the Gist has no file of that name — there is nothing to adopt"
	case remoteSHA == "":
		return addDecisionPush, "the Gist has no file of that name yet"
	case remoteSHA == localSHA:
		return addDecisionLink, "content identical (already in sync)"
	case force:
		return addDecisionPush, "forced: local content replaces the Gist's"
	case adoptRemote:
		return addDecisionAdopt, "adopting the Gist's content"
	default:
		return addDecisionBlock, "local and remote differ"
	}
}

// linkExistingGist reads the Gist before writing anything, then records the
// resulting state itself. It owns the state write because the adopt path has
// to persist a suppression marker before it touches the local file, which the
// other paths never do.
func linkExistingGist(sm *state.Manager, client gistLinker, absPath, gistID string, localContent []byte, opts addOptions) error {
	filename := filepath.Base(absPath)
	remoteFiles, remoteUpdatedAt, err := client.FetchAllFiles(gistID)
	if err != nil {
		return fmt.Errorf("failed to read gist %s: %w", gistID, err)
	}

	localSHA := sha256Hex(localContent)
	remoteContent, present := remoteFiles[filename]
	var remoteSHA string
	if present {
		remoteSHA = sha256Hex(remoteContent)
	}

	decision, reason := addGate(remoteSHA, localSHA, opts.force, opts.adoptRemote)
	switch decision {
	case addDecisionLink:
		opts.logf("  Nothing sent: %s", reason)
		trackSynced(sm, absPath, gistID, localSHA, remoteUpdatedAt)

	case addDecisionPush:
		patchedAt, err := client.UpdateFile(gistID, absPath, localContent)
		if err != nil {
			return fmt.Errorf("failed to link %s to gist %s: %w", displayPath(absPath), gistID, err)
		}
		opts.logf("  Pushed %d bytes to the Gist: %s", len(localContent), reason)
		trackSynced(sm, absPath, gistID, localSHA, patchedAt)

	case addDecisionAdopt:
		if err := adoptRemoteContent(sm, absPath, gistID, remoteContent, remoteSHA, remoteUpdatedAt, opts.logf); err != nil {
			return err
		}

	default:
		if remoteSHA != "" && remoteSHA != localSHA {
			reportDivergence(localContent, remoteContent, remoteUpdatedAt, opts.logf)
		}
		return fmt.Errorf("aborted: %s — nothing was written and %s is not tracked", reason, displayPath(absPath))
	}

	return sm.Save()
}

// adoptRemoteContent overwrites the local file with the Gist's content. It
// mirrors pull's sequence — backup, persist the suppression marker, atomic
// rename — because the file may already be tracked by a running daemon, whose
// fsnotify write would otherwise bounce the adopted bytes straight back up.
func adoptRemoteContent(sm *state.Manager, absPath, gistID string, remoteContent []byte, remoteSHA string, remoteUpdatedAt int64, logf func(string, ...any)) error {
	info, err := os.Stat(absPath)
	if err != nil {
		return fmt.Errorf("failed to stat %s: %w", displayPath(absPath), err)
	}
	perm := info.Mode().Perm()

	localContent, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", displayPath(absPath), err)
	}
	backupPath, err := backupLocalFile(absPath, localContent, perm)
	if err != nil {
		return fmt.Errorf("failed to back up %s: %w", displayPath(absPath), err)
	}
	logf("  [Backup] %s", displayPath(backupPath))

	// Must be persisted before the rename below; the daemon reacts to the
	// fsnotify write and needs to see the marker before it decides on the PATCH.
	effective, _ := resolveDebounce(false, 0, os.Getenv(debounceEnvVar))
	trackSynced(sm, absPath, gistID, remoteSHA, remoteUpdatedAt)
	fs := sm.Files[absPath]
	fs.PullSuppressUntil = time.Now().Add(effective + pullSuppressGrace).Unix()
	sm.Files[absPath] = fs
	if err := sm.Save(); err != nil {
		return fmt.Errorf("failed to save suppression marker: %w", err)
	}

	if err := writeAtomic(absPath, remoteContent, perm); err != nil {
		return fmt.Errorf("failed to write %s: %w", displayPath(absPath), err)
	}
	logf("  [Write] %d bytes adopted from the Gist", len(remoteContent))
	return nil
}

// reportDivergence prints the two sides together so the caller can pick a
// direction without running pull --dry-run first.
func reportDivergence(localContent, remoteContent []byte, remoteUpdatedAt int64, logf func(string, ...any)) {
	added, removed := lineDiffSummary(localContent, remoteContent)
	logf("  Local:  %d bytes", len(localContent))
	logf("  Remote: %d bytes, updated %s", len(remoteContent),
		time.Unix(remoteUpdatedAt, 0).Format(time.RFC3339))
	logf("  Diff:   +%d lines, -%d lines (remote relative to local)", added, removed)
	logf("")
	logf("  Nothing was written. Re-run with one of:")
	logf("    --adopt-remote  take the Gist's content, backing up the local file")
	logf("    --force         replace the Gist's content with the local file")
}

// trackSynced registers absPath with both watermarks set, marking the moment
// local and remote are known to agree.
func trackSynced(sm *state.Manager, absPath, gistID, contentSHA string, remoteUpdatedAt int64) {
	sm.AddTrackedFile(absPath, gistID, time.Now().Unix())
	fs := sm.Files[absPath]
	fs.ContentSHA = contentSHA
	fs.RemoteUpdatedAt = remoteUpdatedAt
	sm.Files[absPath] = fs
}

func init() {
	addCmd.Flags().StringVar(&gistIDFlag, "gist-id", "", "Existing Gist ID to link to")
	addCmd.Flags().BoolVar(&addForce, "force", false,
		"With --gist-id: replace the Gist's content with the local file when the two differ")
	addCmd.Flags().BoolVar(&addAdoptRemote, "adopt-remote", false,
		"With --gist-id: replace the local file with the Gist's content when the two differ")
	rootCmd.AddCommand(addCmd)
}
