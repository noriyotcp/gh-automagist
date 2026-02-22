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
		switch action {
		case "status":
			_ = statusCmd.RunE(statusCmd, []string{})
		case "list":
			_ = listCmd.RunE(listCmd, []string{})
		case "add":
			runDashboardAddInteraction()
		case "remove":
			runDashboardRemoveInteraction()
		case "start":
			_ = monitorCmd.RunE(monitorCmd, []string{})
		case "stop":
			_ = stopCmd.RunE(stopCmd, []string{})
		case "exit":
			fmt.Println("Goodbye!")
			return
		}

		// Wait before looping back if the action wasn't Exit
		if action != "exit" {
			waitForEnter()
		}
	}
}

func clearScreen() {
	// ANSI escape sequence to clear screen and move cursor to top-left
	fmt.Print("\033[H\033[2J")
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

// Helper to ask the user a quick input during the dashboard loop
func runDashboardAddInteraction() {
	var filePath string
	err := huh.NewInput().
		Title("Enter file path to add:").
		Value(&filePath).
		Run()

	if err == nil && filePath != "" {
		// Call Add with args instead of Run
		addCmd.Run(addCmd, []string{filePath})
	}
}

func runDashboardRemoveInteraction() {
	var filePath string
	err := huh.NewInput().
		Title("Enter file path to remove:").
		Value(&filePath).
		Run()

	if err == nil && filePath != "" {
		// Call Remove with args instead of Run
		removeCmd.Run(removeCmd, []string{filePath})
	}
}

func waitForEnter() {
	fmt.Println("\nPress Enter to return to Dashboard...")
	var dummy string
	fmt.Scanln(&dummy)
}
