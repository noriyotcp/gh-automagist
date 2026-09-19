package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
)

func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

// isMonitorRunning reports whether the PID file's process is alive.
func isMonitorRunning() bool {
	sm, err := state.NewManager()
	if err != nil {
		return false
	}
	if sm.Load() != nil {
		return false
	}
	pid := sm.GetPID()
	if pid == 0 {
		return false
	}
	out, err := exec.Command("ps", "-o", "state=", "-p", fmt.Sprintf("%d", pid)).Output()
	if err != nil || len(strings.TrimSpace(string(out))) == 0 {
		return false
	}
	return true
}

// registryChangeNote is what `add` and `remove` print about the running
// daemon. A daemon started from this same version follows state.json live, so
// there is nothing to do; an older one built its watch list once at startup and
// still needs a restart to see the change.
func registryChangeNote() string {
	if !isMonitorRunning() {
		return ""
	}
	sm, err := state.NewManager()
	if err != nil {
		return ""
	}
	info, err := sm.ReadMonitorInfo()
	if err != nil || info == nil || info.Version != Version {
		return "Note: the running monitor predates live registry tracking — run 'gh automagist restart' to pick this up."
	}
	return "The running monitor picks this up on its own; no restart needed."
}

// renderCompactHeader draws the sub-screen status bar.
func renderCompactHeader() {
	appStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	app := appStyle.Render("gh-automagist")

	statusText := "○ STOPPED"
	statusColor := "8"

	sm, err := state.NewManager()
	if err == nil && sm.Load() == nil {
		pid := sm.GetPID()
		if pid != 0 {
			out, err := exec.Command("ps", "-o", "state=", "-p", fmt.Sprintf("%d", pid)).Output()
			if err == nil && len(out) > 0 {
				stateStr := strings.TrimSpace(string(out))
				if strings.HasPrefix(stateStr, "T") {
					statusText = "◐ SUSPENDED"
					statusColor = "3"
				} else {
					statusText = "● RUNNING"
					statusColor = "2"
				}
			}
		}
	}

	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))
	bar := fmt.Sprintf("%s  %s", app, statusStyle.Render(statusText))
	fmt.Println(bar)
	fmt.Println(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("────────────────────────────────"))
	fmt.Println()
}
