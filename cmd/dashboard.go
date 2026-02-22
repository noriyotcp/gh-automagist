package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
	"golang.org/x/term"
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
	// Fall back to compact header on narrow terminals to prevent wrapping
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w < 110 {
		renderCompactHeader()
		return
	}

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

// runDashboardAddInteraction is a multi-step wizard for adding a file to monitor.
// Returns true if the user cancelled at any step.
func runDashboardAddInteraction() bool {
	// Step 1: File path
	clearScreen()
	renderCompactHeader()
	var filePath string
	err := huh.NewInput().
		Title("Step 1/2 – Enter file path to add (empty to cancel):").
		Value(&filePath).
		Run()
	if err != nil || filePath == "" {
		return true
	}

	// Step 2: New Gist or link to existing?
	clearScreen()
	renderCompactHeader()
	var gistMode string
	err = huh.NewSelect[string]().
		Title("Step 2/2 – How should this file be tracked?").
		Options(
			huh.NewOption("Create a new Gist", "new"),
			huh.NewOption("Link to an existing Gist", "existing"),
			huh.NewOption("← Cancel", "cancel"),
		).
		Value(&gistMode).
		Run()
	if err != nil || gistMode == "cancel" {
		return true
	}

	if gistMode == "new" {
		_ = addCmd.RunE(addCmd, []string{filePath})
		return false
	}

	// Step 3: Ask for the existing Gist ID
	clearScreen()
	renderCompactHeader()
	var gistID string
	err = huh.NewInput().
		Title("Enter Gist ID to link to (empty to cancel):").
		Value(&gistID).
		Run()
	if err != nil || gistID == "" {
		return true
	}

	// Set the gist-id flag and execute
	_ = addCmd.Flags().Set("gist-id", gistID)
	_ = addCmd.RunE(addCmd, []string{filePath})
	// Reset the flag so it doesn't persist across calls
	_ = addCmd.Flags().Set("gist-id", "")
	return false
}

// runDashboardRemoveInteraction shows a selectable list of monitored files,
// then confirms before removing. Returns true if the user cancelled.
func runDashboardRemoveInteraction() bool {
	sm, err := state.NewManager()
	if err != nil {
		return true
	}
	if err := sm.Load(); err != nil {
		return true
	}
	if len(sm.Files) == 0 {
		clearScreen()
		renderCompactHeader()
		fmt.Println("No monitored files to remove.")
		waitForEnter()
		return true
	}

	// Build the selection list
	clearScreen()
	renderCompactHeader()
	var options []huh.Option[string]
	options = append(options, huh.NewOption("← Cancel", ""))
	for path := range sm.Files {
		label := strings.Replace(path, homeDir(), "~", 1)
		options = append(options, huh.NewOption(label, path))
	}

	var selectedPath string
	err = huh.NewSelect[string]().
		Title("Select a file to stop monitoring:").
		Options(options...).
		Value(&selectedPath).
		Run()
	if err != nil || selectedPath == "" {
		return true
	}

	// Confirmation step
	clearScreen()
	renderCompactHeader()
	var confirmed bool
	err = huh.NewConfirm().
		Title(fmt.Sprintf("Stop monitoring %s?", strings.Replace(selectedPath, homeDir(), "~", 1))).
		Value(&confirmed).
		Run()
	if err != nil || !confirmed {
		return true
	}

	_ = removeCmd.RunE(removeCmd, []string{selectedPath})
	return false
}

// homeDir returns the user's home directory path.
func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

func waitForEnter() {
	fmt.Println("\nPress Enter to return to Dashboard...")
	var dummy string
	fmt.Scanln(&dummy)
}
