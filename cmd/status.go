package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/notify"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

// Traffic-light badge palette. lipgloss strips ANSI codes automatically when
// stdout is not a TTY so pipes / redirects stay grep-clean.
var (
	inSyncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	newerStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
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
		info, infoErr := sm.ReadMonitorInfo()
		if infoErr != nil {
			fmt.Printf("Warning: failed to read monitor info: %v\n", infoErr)
		}
		if pid == 0 {
			fmt.Println("Monitor Status: STOPPED")
		} else {
			out, err := exec.Command("ps", "-o", "state=", "-p", fmt.Sprintf("%d", pid)).Output()
			if err != nil || len(out) == 0 {
				fmt.Println("Monitor Status: STOPPED")
			} else {
				stateStr := strings.TrimSpace(string(out))
				label := "RUNNING"
				if strings.HasPrefix(stateStr, "T") {
					label = "SUSPENDED"
				}
				daemonVersion := ""
				if info != nil {
					daemonVersion = info.Version
				}
				if daemonVersion != "" {
					fmt.Printf("Monitor Status: %s (PID: %d, version: %s)\n", label, pid, daemonVersion)
				} else {
					fmt.Printf("Monitor Status: %s (PID: %d)\n", label, pid)
				}
				if daemonVersion != "" && daemonVersion != Version {
					fmt.Printf("  %s daemon is %s but installed binary is %s — run 'gh automagist restart' to pick it up.\n",
						errorStyle.Render("!"), daemonVersion, Version)
				}
			}
		}

		fmt.Println()

		if len(sm.Files) == 0 {
			fmt.Println("No files registered.")
			return nil
		}

		statuses := notify.Detect(sm, gist.NewClient())

		fmt.Printf("Registered Files (%d):\n", len(sm.Files))
		for _, s := range statuses {
			fmt.Printf("- %s (Gist ID: %s)  %s\n", s.Path, s.GistID, statusBadge(s))
		}

		return nil
	},
}

// statusBadge renders a short suffix describing the file's notify state.
// Kept trivial so both status and dashboard can reuse it if we ever wire
// dashboard through the same struct.
func statusBadge(s notify.FileStatus) string {
	switch {
	case s.Err != nil:
		return errorStyle.Render(fmt.Sprintf("[error: %v]", s.Err))
	case s.RemoteNewer:
		return newerStyle.Render("[remote: newer ⇧]")
	default:
		return inSyncStyle.Render("[in sync]")
	}
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
