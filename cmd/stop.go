package cmd

import (
	"fmt"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:     "stop",
	Short:   "Stop the running monitor process",
	GroupID: "daemon",
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
			fmt.Println("Monitor is not running.")
			return nil
		}

		killed, err := sm.KillMonitor(pid)
		if err != nil {
			// KillMonitor already names the PID, and both callers report what
			// they get back — cobra for the CLI, the dashboard for the TUI.
			// Printing here as well is how one failure became three lines.
			return err
		}
		if killed {
			fmt.Printf("Stopped monitor (PID: %d)\n", pid)
		} else {
			fmt.Printf("Monitor process %d was not running (stale PID file, cleaned up)\n", pid)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
