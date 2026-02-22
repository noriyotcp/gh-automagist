package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Launch interactive TUI dashboard",
	Long:  `Provide a continuous loop menu for managing gh-automagist operations using an interactive UI.`,
	Run: func(cmd *cobra.Command, args []string) {
		runDashboard()
	},
}

func init() {
	rootCmd.AddCommand(dashboardCmd)
}

func runDashboard() {
	for {
		var action string

		clearScreen()
		renderHeader()

		// Create the selection menu
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Options(
						huh.NewOption("Status (Check Monitor)", "status"),
						huh.NewOption("List Monitored Files", "list"),
						huh.NewOption("Add File", "add"),
						huh.NewOption("Remove File", "remove"),
						huh.NewOption("Start Monitor", "start"),
						huh.NewOption("Stop Monitor", "stop"),
						huh.NewOption("Exit", "exit"),
					).
					Value(&action),
			),
		)

		err := form.Run()
		if err != nil {
			// Handle user cancellation (e.g. Esc/Ctrl+C)
			fmt.Println("Exiting dashboard...")
			break
		}

		// Execute the chosen action
		backedOut := false
		switch action {
		case "status":
			_ = statusCmd.RunE(statusCmd, []string{})
		case "list":
			var err error
			backedOut, err = runListInteractive()
			_ = err
		case "add":
			if runDashboardAddInteraction() {
				backedOut = true
			}
		case "remove":
			if runDashboardRemoveInteraction() {
				backedOut = true
			}
		case "start":
			_ = monitorCmd.RunE(monitorCmd, []string{})
		case "stop":
			_ = stopCmd.RunE(stopCmd, []string{})
		case "exit":
			fmt.Println("Goodbye!")
			return
		}

		// Wait before looping back, unless the user just backed out of a submenu
		if !backedOut {
			waitForEnter()
		}
	}
}

func renderHeader() {
	art := `
   ██████╗ ██╗  ██╗     █████╗ ██╗   ██╗████████╗ ██████╗ ███╗   ███╗ █████╗  ██████╗ ██╗███████╗████████╗
  ██╔════╝ ██║  ██║    ██╔══██╗██║   ██║╚══██╔══╝██╔═══██╗████╗ ████║██╔══██╗██╔════╝ ██║██╔════╝╚══██╔══╝
  ██║  ███╗███████║    ███████║██║   ██║   ██║   ██║   ██║██╔████╔██║███████║██║  ███╗██║███████╗   ██║
  ██║   ██║██╔══██║    ██╔══██║██║   ██║   ██║   ██║   ██║██║╚██╔╝██║██╔══██║██║   ██║██║╚════██║   ██║
  ╚██████╔╝██║  ██║    ██║  ██║╚██████╔╝   ██║   ╚██████╔╝██║ ╚═╝ ██║██║  ██║╚██████╔╝██║███████║   ██║
   ╚═════╝ ╚═╝  ╚═╝    ╚═╝  ╚═╝ ╚═════╝    ╚═╝    ╚═════╝ ╚═╝     ╚═╝╚═╝  ╚═╝ ╚═════╝ ╚═╝╚══════╝   ╚═╝
`

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	fmt.Println(headerStyle.Render(art))

	// Status detection
	sm, err := state.NewManager()
	statusText := "○ STOPPED"
	statusColor := "8" // Grey
	pidText := ""

	if err == nil && sm.Load() == nil {
		pid := sm.GetPID()
		if pid != 0 {
			out, err := exec.Command("ps", "-o", "state=", "-p", fmt.Sprintf("%d", pid)).Output()
			if err == nil && len(out) > 0 {
				stateStr := strings.TrimSpace(string(out))
				if strings.HasPrefix(stateStr, "T") {
					statusText = "◐ SUSPENDED"
					statusColor = "3" // Yellow
				} else {
					statusText = "● RUNNING"
					statusColor = "2" // Green
				}
				pidText = fmt.Sprintf(" (PID: %d)", pid)
			}
		}
	}

	statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true)
	fmt.Println(statusStyle.Render(fmt.Sprintf("  Monitor: %s%s\n", statusText, pidText)))
}

// runDashboardAddInteraction shows the compact header then prompts for a file
// path to add. Returns true if the user cancelled (Ctrl+C or empty input),
// meaning the caller should skip waitForEnter and go straight back to the menu.
func runDashboardAddInteraction() bool {
	clearScreen()
	renderCompactHeader()
	var filePath string
	err := huh.NewInput().
		Title("Enter file path to add (empty to cancel):").
		Value(&filePath).
		Run()

	// Ctrl+C returns an error; empty input means the user wants to go back
	if err != nil || filePath == "" {
		return true
	}
	_ = addCmd.RunE(addCmd, []string{filePath})
	return false
}

// runDashboardRemoveInteraction shows the compact header then prompts for a
// file path to remove. Returns true if the user cancelled.
func runDashboardRemoveInteraction() bool {
	clearScreen()
	renderCompactHeader()
	var filePath string
	err := huh.NewInput().
		Title("Enter file path to remove (empty to cancel):").
		Value(&filePath).
		Run()

	if err != nil || filePath == "" {
		return true
	}
	_ = removeCmd.RunE(removeCmd, []string{filePath})
	return false
}

func waitForEnter() {
	fmt.Println("\nPress Enter to return to Dashboard...")
	var dummy string
	fmt.Scanln(&dummy)
}
