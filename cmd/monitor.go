package cmd

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/monitor"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var daemonMode bool

var monitorCmd = &cobra.Command{
	Use:   "monitor",
	Short: "Start monitoring files defined in state.json and sync them to GitHub Gists",
	RunE: func(cmd *cobra.Command, args []string) error {
		// --daemon: re-launch self without the flag as a detached background process
		if daemonMode {
			binary, err := os.Executable()
			if err != nil {
				return fmt.Errorf("could not determine executable path: %w", err)
			}
			child := exec.Command(binary, "monitor")
			child.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
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
					return nil
				}
			}
			fmt.Println(" (monitor may still be starting up)")
			return nil
		}

		fmt.Println("Starting gh-automagist monitor...")

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

		// 3. Initialize the GitHub API Client
		gistClient := gist.NewClient()

		// 4. Hook up the watcher's OnChange callback to trigger the Gist upload
		watcher.OnChange = func(absPath string, gistID string) {
			content, err := os.ReadFile(absPath)
			if err != nil {
				log.Printf("Error reading file %s: %v", absPath, err)
				return
			}

			log.Printf("  -> Uploading %s to Gist %s...", filepath.Base(absPath), gistID)

			err = gistClient.UpdateFile(gistID, absPath, content)
			if err != nil {
				log.Printf("  [Error] Failed to update gist: %v", err)
			} else {
				log.Printf("  [Success] Gist updated successfully.")
			}
		}

		// 5. Start the blocking event loop
		if err := sm.WritePID(); err != nil {
			log.Printf("Warning: failed to write PID file: %v", err)
		}
		defer sm.DeletePID()

		fmt.Printf("Monitoring %d files. Press Ctrl+C to stop.\n", len(sm.Files))
		return watcher.Start()
	},
}

func init() {
	monitorCmd.Flags().BoolVarP(&daemonMode, "daemon", "d", false, "Run monitor in the background as a daemon")
	rootCmd.AddCommand(monitorCmd)
}
