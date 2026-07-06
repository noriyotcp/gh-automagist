package cmd

import (
	"fmt"
	"os"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var restartForeground bool

var restartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the monitor to pick up the current state.json. Runs as a daemon by default",
	RunE: func(cmd *cobra.Command, args []string) error {
		sm, err := state.NewManager()
		if err != nil {
			return err
		}
		if err := sm.Load(); err != nil {
			return err
		}

		if pid := sm.GetPID(); pid != 0 {
			process, _ := os.FindProcess(pid)
			if err := process.Kill(); err != nil {
				fmt.Printf("Note: monitor was not actually running (stale PID file for %d)\n", pid)
			} else {
				fmt.Printf("Stopped monitor (PID: %d)\n", pid)
			}
			sm.DeletePID()
		} else {
			fmt.Println("Monitor was not running; starting fresh.")
		}

		daemonMode = !restartForeground
		return monitorCmd.RunE(cmd, args)
	},
}

func init() {
	restartCmd.Flags().BoolVarP(&restartForeground, "foreground", "f", false, "Run the monitor in the foreground instead of as a daemon")
	rootCmd.AddCommand(restartCmd)
}
