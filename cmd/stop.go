package cmd

import (
	"fmt"
	"os"

	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running monitor process",
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

		process, err := os.FindProcess(pid)
		if err != nil {
			fmt.Printf("Monitor process %d not found (stale PID file?)\n", pid)
			sm.DeletePID()
			return nil
		}

		err = process.Kill()
		if err != nil {
			fmt.Printf("Failed to stop monitor: %v\n", err)
			return err
		}

		fmt.Printf("Stopped monitor (PID: %d)\n", pid)
		sm.DeletePID()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
