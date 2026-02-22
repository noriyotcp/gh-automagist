package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
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

		// Create the selection menu
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("gh-automagist Dashboard").
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
