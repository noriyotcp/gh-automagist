package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/monitor"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var daemonMode bool
var debounceInterval time.Duration

// GH_AUTOMAGIST_DEBOUNCE_INTERVAL is the env-var fallback for --debounce.
// Kept at package scope so cmd/monitor.go and its test share one name.
const debounceEnvVar = "GH_AUTOMAGIST_DEBOUNCE_INTERVAL"

// GH_AUTOMAGIST_LOG_FILE carries the daemon's log destination from the parent
// process to the detached child. It is internal plumbing rather than a knob:
// setting it by hand on a foreground run just redirects that run's log.
const logFileEnvVar = "GH_AUTOMAGIST_LOG_FILE"

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Start monitoring files defined in state.json and sync them to GitHub Gists",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Prevent double-starting regardless of mode (daemon or foreground)
		if isMonitorRunning() {
			fmt.Println("Monitor is already running.")
			return nil
		}

		// --daemon: re-launch self without the flag as a detached background process
		if daemonMode {
			binary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("could not determine executable path: %w", err)
			}
			// The child's stdio is wired to the null device below, so its log
			// output would vanish. The parent picks the destination and hands
			// it over, so it can also tell the user where to look.
			logSM, err := state.NewManager()
			if err != nil {
				return fmt.Errorf("failed to initialize state manager: %w", err)
			}
			logPath := monitorLogPath(logSM.LogDir(), time.Now())
			// Forward --debounce to the child so the daemon runs with the
			// caller-specified interval. The env var is inherited automatically.
			childArgs := []string{"monitor"}
			if cmd.Flags().Changed("debounce") {
				childArgs = append(childArgs, "--debounce", debounceInterval.String())
			}
			child := exec.Command(binary, childArgs...)
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
			child.Env = append(os.Environ(), logFileEnvVar+"="+logPath)
			child.Stdin = nil
			child.Stdout = nil
			child.Stderr = nil
			if err := child.Start(); err != nil {
				return fmt.Errorf("failed to start monitor daemon: %w", err)
			}
			_ = child.Process.Release()

			// Poll up to 3 seconds for the PID file to confirm startup
			fmt.Print("Starting monitor daemon")
			for i := 0; i < 6; i++ {
				time.Sleep(500 * time.Millisecond)
				fmt.Print(".")
				sm, err := state.NewManager()
				if err == nil && sm.Load() == nil && sm.GetPID() != 0 {
					fmt.Printf(" started! (PID: %d)\n", sm.GetPID())
					fmt.Printf("Log: %s\n", displayPath(logPath))
					return nil
				}
			}
			fmt.Println(" (monitor may still be starting up)")
			fmt.Printf("Log: %s\n", displayPath(logPath))
			return nil
		}

		fmt.Println("Starting gh-automagist monitor...")

		// Set by the parent when this process is the detached daemon child.
		// An empty value means a foreground run, where stderr is the right
		// place for the log and redirecting it would hide it from the user.
		if logPath := os.Getenv(logFileEnvVar); logPath != "" {
			logFile, err := openMonitorLog(logPath)
			if err != nil {
				return fmt.Errorf("failed to open monitor log: %w", err)
			}
			defer logFile.Close()
		}

		// 1. Load the state manager to know what files to watch
		sm, err := state.NewManager()
		if err != nil {
			return fmt.Errorf("failed to initialize state manager: %w", err)
		}

		err = sm.Load()
		if err != nil {
			return fmt.Errorf("failed to load state.json: %w", err)
		}

		if len(sm.Files) == 0 {
			fmt.Println("No files are currently configured for monitoring.")
			fmt.Println("Use 'gh automagist add' to start tracking files.")
			return nil
		}

		// 2. Initialize the file watcher
		watcher, err := monitor.NewWatcher(sm)
		if err != nil {
			return fmt.Errorf("failed to initialize watcher: %w", err)
		}

		// Resolve the debounce interval: --debounce > env var > default.
		effective, resolveErr := resolveDebounce(
			cmd.Flags().Changed("debounce"),
			debounceInterval,
			os.Getenv(debounceEnvVar),
		)
		if resolveErr != nil {
			log.Printf("Warning: invalid %s=%q, using default: %v",
				debounceEnvVar, os.Getenv(debounceEnvVar), resolveErr)
		}
		watcher.DebounceInterval = effective
		if effective > 0 {
			log.Printf("[gh-automagist] debounce interval: %s", effective)
		} else {
			log.Printf("[gh-automagist] debounce disabled (every write triggers immediate sync)")
		}

		// 3. Initialize the GitHub API Client
		gistClient := gist.NewClient()

		// 4. Hook up the watcher's OnChange callback to trigger the Gist upload.
		// Debounce timers fire on their own goroutines, so two files edited
		// together would otherwise run overlapping load-modify-save cycles over
		// the same state.json. One mutex around the whole callback serialises
		// them.
		var syncMu sync.Mutex
		watcher.OnChange = func(absPath string, gistID string) {
			syncMu.Lock()
			defer syncMu.Unlock()

			content, err := os.ReadFile(absPath)
			if err != nil {
				log.Printf("Error reading file %s: %v", absPath, err)
				return
			}

			// Re-check the on-disk state right before deciding: pull may have
			// written PullSuppressUntil after the event-loop reload.
			if err := sm.Load(); err != nil {
				log.Printf("Warning: failed to reload state.json before suppression check: %v", err)
			}
			fs := sm.Files[absPath]
			currentSHA := sha256Hex(content)
			if monitor.ShouldSuppress(fs, currentSHA, time.Now().Unix()) {
				log.Printf("  [Suppressed] %s matches pull baseline; skipping redundant PATCH", filepath.Base(absPath))
				fs.PullSuppressUntil = 0
				sm.Files[absPath] = fs
				if err := sm.Save(); err != nil {
					log.Printf("  Warning: failed to clear pull_suppress_until: %v", err)
				}
				return
			}

			log.Printf("  -> Uploading %s to Gist %s...", filepath.Base(absPath), gistID)

			remoteUpdatedAt, err := gistClient.UpdateFile(gistID, absPath, content)
			if err != nil {
				// Deliberately no state write: leaving the old timestamps in
				// place is what keeps `status` and `pull` treating this file as
				// unsynced instead of silently declaring success.
				log.Printf("  [Error] Failed to update gist: %v", err)
				return
			}
			log.Printf("  [Success] Gist updated successfully.")

			// Reload once more — the PATCH is a network round-trip, long enough
			// for another command to have rewritten state.json.
			if err := sm.Load(); err != nil {
				log.Printf("  Warning: failed to reload state.json before recording sync: %v", err)
			}
			fs, stillTracked := sm.Files[absPath]
			if !stillTracked {
				return
			}
			sm.Files[absPath] = recordSyncSuccess(fs, currentSHA, remoteUpdatedAt, time.Now().Unix())
			if err := sm.Save(); err != nil {
				log.Printf("  Warning: failed to record sync result: %v", err)
			}
		}

		// 5. Start the blocking event loop
		if err := sm.WritePID(); err != nil {
			log.Printf("Warning: failed to write PID file: %v", err)
		}
		if err := sm.WriteMonitorInfo(state.MonitorInfo{
			PID:       os.Getpid(),
			Version:   Version,
			Commit:    Commit,
			StartedAt: time.Now().Unix(),
		}); err != nil {
			log.Printf("Warning: failed to write monitor info: %v", err)
		}
		defer sm.DeletePID()
		defer sm.DeleteMonitorInfo()

		reconcileAtStartup(sm, gistClient)

		fmt.Printf("Monitoring %d files. Press Ctrl+C to stop.\n", len(sm.Files))
		return watcher.Start()
	},
}

// recordSyncSuccess stamps a file's state after a PATCH the daemon confirmed.
// UpdatedAt is the moment the sync succeeded rather than the moment the write
// was noticed, ContentSHA is the digest of the bytes actually sent, and
// RemoteUpdatedAt is the Gist timestamp our own PATCH produced — without that
// last one every push leaves the Gist looking newer than the last thing we
// observed, and `status` reports a remote change that is really our own.
// monitorLogPath names one file per daemon start. The timestamp layout matches
// the log files the earlier Ruby implementation left in the same directory, so
// a plain sort still interleaves the two correctly.
func monitorLogPath(dir string, start time.Time) string {
	return filepath.Join(dir, fmt.Sprintf("monitor_%s.log", start.Format("20060102_150405")))
}

// openMonitorLog redirects the standard logger to path, creating the log
// directory on the way. The caller keeps the handle open for the process's
// lifetime.
func openMonitorLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	log.SetOutput(f)
	return f, nil
}

// reconcileAtStartup catches up on everything that happened while the daemon
// was down: fsnotify only reports writes that occur while the watcher is live,
// so an edit made between shutdown and startup would otherwise sit unsynced
// until the file happened to be written again. Every failure here is logged
// and swallowed — an unreachable API at boot must not stop the watcher.
func reconcileAtStartup(sm *state.Manager, client gistPusher) {
	targets := trackedPaths(sm)
	if len(targets) == 0 {
		return
	}
	log.Printf("[gh-automagist] reconciling %d tracked files...", len(targets))
	summary := pushTargets(sm, client, targets, pushOptions{
		logf: func(format string, args ...any) {
			log.Printf(format, args...)
		},
	})
	log.Printf("[gh-automagist] reconcile: %s", summary)
}

func recordSyncSuccess(fs state.FileState, contentSHA string, remoteUpdatedAt, nowUnix int64) state.FileState {
	fs.UpdatedAt = nowUnix
	fs.ContentSHA = contentSHA
	fs.RemoteUpdatedAt = remoteUpdatedAt
	return fs
}

func init() {
	monitorCmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "Run monitor in the background as a daemon")
	monitorCmd.Flags().DurationVar(&debounceInterval, "debounce", 0,
		"Quiet-window between the last write and the Gist sync (e.g. 5s, 500ms, 0 to disable). "+
			"Overrides "+debounceEnvVar+" env var and the compiled-in default.")
	rootCmd.AddCommand(monitorCmd)
}
