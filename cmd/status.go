package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show monitor process status and currently monitored files",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := sm.Load(); err != nil {
			return err
		}

		pid := sm.GetPID()
		if pid == 0 {
			fmt.Println("Monitor Status: STOPPED")
		} else {
			// Check process state using ps (macOS/Linux compatible)
			out, err := exec.Command("ps", "-o", "state=", "-p", fmt.Sprintf("%d", pid)).Output()
			if err != nil || len(out) == 0 {
				fmt.Println("Monitor Status: STOPPED")
			} else {
				stateStr := strings.TrimSpace(string(out))
				if strings.HasPrefix(stateStr, "T") {
					fmt.Printf("Monitor Status: SUSPENDED (PID: %d)\n", pid)
				} else {
					fmt.Printf("Monitor Status: RUNNING (PID: %d)\n", pid)
				}
			}
		}

		fmt.Println()

		if len(sm.Files) == 0 {
			fmt.Println("No files registered.")
			return nil
		}

		fmt.Printf("Registered Files (%d):\n", len(sm.Files))
		for path, info := range sm.Files {
			fmt.Printf("- %s (Gist ID: %s)\n", path, info.GistID)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
