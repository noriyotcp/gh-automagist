package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/noriyo_tcp/gh-automagist/pkg/gist"
	"github.com/noriyo_tcp/gh-automagist/pkg/notify"
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
	// One-shot fetch at dashboard entry — cached across the menu loop.
	// Refreshing on every iteration would issue a network round-trip per
	// menu action, which turns interactive UX into wait-then-interact.
	// User exits and re-enters the dashboard to refresh.
	dashboardStatuses := fetchDashboardStatuses()

	for {
		var action string

		clearScreen()
		renderHeader()
		renderDashboardNotice(dashboardStatuses)

		// Filter hint disabled — not useful on a 7-item menu.
		km := huh.NewDefaultKeyMap()
		km.Select.Filter.SetEnabled(false)
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
		).WithKeyMap(km)

		err := form.Run()
		if err != nil {
			// Handle user cancellation (e.g. Esc/Ctrl+C)
			fmt.Println("Exiting dashboard...")
			break
		}

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
			startMonitorInBackground()
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

// fetchDashboardStatuses runs a single notify.Detect at dashboard entry.
// Returns nil silently on setup failure (no tracked files, load error) —
// renderDashboardNotice treats nil as "nothing to say".
func fetchDashboardStatuses() []notify.FileStatus {
	sm, err := state.NewManager()
	if err != nil {
		return nil
	}
	if err := sm.Load(); err != nil {
		return nil
	}
	if len(sm.Files) == 0 {
		return nil
	}
	return notify.Detect(sm, gist.NewClient())
}

// renderDashboardNotice prints the "N files have remote changes" summary
// under the header if there is anything to report; silent otherwise.
// Fetch failures are surfaced as a separate line so users notice offline
// runs but a normal in-sync setup renders no extra chrome.
func renderDashboardNotice(statuses []notify.FileStatus) {
	if len(statuses) == 0 {
		return
	}
	var newerCount, errCount int
	for _, s := range statuses {
		switch {
		case s.Err != nil:
			errCount++
		case s.RemoteNewer:
			newerCount++
		}
	}
	if newerCount > 0 {
		notice := lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(
			fmt.Sprintf("⚠ %d file(s) have remote changes — run `gh automagist fetch` for details", newerCount))
		fmt.Println(notice)
	}
	if errCount > 0 {
		notice := lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(
			fmt.Sprintf("! %d file(s) could not be checked (network error)", errCount))
		fmt.Println(notice)
	}
	if newerCount > 0 || errCount > 0 {
		fmt.Println()
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

// runDashboardAddInteraction runs the add-file wizard; returns true if the user cancelled.
func runDashboardAddInteraction() bool {
	// Step 1: File selection
	clearScreen()
	renderCompactHeader()
	filePath, err := runFilteredFileBrowser(homeDir())
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

	_ = addCmd.Flags().Set("gist-id", gistID)
	_ = addCmd.RunE(addCmd, []string{filePath})
	// Reset the flag so it doesn't persist across calls
	_ = addCmd.Flags().Set("gist-id", "")
	return false
}

// runDashboardRemoveInteraction runs the remove-file wizard; returns true if the user cancelled.
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

// runFilteredFileBrowser prompts for a file via a searchable list.
func runFilteredFileBrowser(startDir string) (string, error) {
	currentDir := startDir
	for {
		entries, err := os.ReadDir(currentDir)
		if err != nil {
			return "", err
		}

		var options []huh.Option[string]
		options = append(options, huh.NewOption(".. (Up)", ".."))

		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue // Skip hidden files for cleaner UI
			}
			label := name
			if entry.IsDir() {
				label += "/"
			}
			options = append(options, huh.NewOption(label, name))
		}

		var selected string
		clearScreen()
		renderCompactHeader()
		err = huh.NewSelect[string]().
			Title(fmt.Sprintf("Directory: %s\n(Filter: /  Select: Enter  Cancel: Esc)", strings.Replace(currentDir, homeDir(), "~", 1))).
			Options(options...).
			Value(&selected).
			Filtering(true).
			Run()

		if err != nil {
			return "", err // User cancelled
		}

		if selected == ".." {
			currentDir = filepath.Dir(currentDir)
			continue
		}

		fullPath := filepath.Join(currentDir, selected)
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		if info.IsDir() {
			currentDir = fullPath
			continue
		}

		return fullPath, nil
	}
}

func homeDir() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return ""
}

// startMonitorInBackground launches a detached monitor so the dashboard is not blocked.
func startMonitorInBackground() {
	if isMonitorRunning() {
		fmt.Println("Monitor is already running.")
		return
	}

	binary, err := os.Executable()
	if err != nil {
		fmt.Println("Error: could not determine executable path:", err)
		return
	}

	cmd := exec.Command(binary, "monitor")
	// Detach from the current process group so the monitor survives if the
	// dashboard exits, and doesn't receive signals sent to the terminal.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Do not inherit stdin/stdout/stderr – it's a daemon.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		fmt.Println("Error starting monitor:", err)
		return
	}
	// Detach from the child so we don't wait for it.
	_ = cmd.Process.Release()

	// Poll up to 3 seconds for the PID file to confirm the monitor started.
	fmt.Print("Starting monitor")
	for i := 0; i < 6; i++ {
		time.Sleep(500 * time.Millisecond)
		fmt.Print(".")
		sm, err := state.NewManager()
		if err == nil && sm.Load() == nil && sm.GetPID() != 0 {
			fmt.Println(" started! (PID:", sm.GetPID(), ")")
			return
		}
	}
	fmt.Println(" (monitor may still be starting up)")
}

func waitForEnter() {
	fmt.Println("\nPress Enter to return to Dashboard...")
	var dummy string
	fmt.Scanln(&dummy)
}
