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
