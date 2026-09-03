package cmd

import (
	"errors"
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
		if err := addCmd.RunE(addCmd, []string{filePath}); err != nil {
			fmt.Printf("  [Error] %v\n", err)
		}
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
	// Reset the flag so it doesn't persist across calls
	defer func() { _ = addCmd.Flags().Set("gist-id", "") }()

	err = addCmd.RunE(addCmd, []string{filePath})
	if err == nil {
		return false
	}
	if !errors.Is(err, errAddDiverged) {
		fmt.Printf("  [Error] %v\n", err)
		return false
	}

	// `add` printed both sides and wrote nothing. The flags that resolve it are
	// CLI-only, so offer the same two directions here instead of sending the
	// user out to a shell. huh renders inline on stderr with no alt screen
	// (huh@v1.0.0/form.go:111-114), so the comparison above stays on screen.
	promptLinkDirection(filePath)
	return false
}

// promptLinkDirection asks which side wins after a blocked `add --gist-id` and
// re-runs the command with the matching flag. addForce and addAdoptRemote are
// package vars shared with the CLI, so they are cleared unconditionally: a
// stale `true` would silently overwrite on the next add.
func promptLinkDirection(filePath string) {
	var direction string
	err := huh.NewSelect[string]().
		Title("Local and remote differ. Which side wins?").
		Options(
			huh.NewOption("Take the Gist's content (--adopt-remote)", "adopt"),
			huh.NewOption("Replace the Gist with the local file (--force)", "force"),
			huh.NewOption("← Leave both alone", "cancel"),
		).
		Value(&direction).
		Run()
	if err != nil || direction == "cancel" {
		fmt.Println("  Nothing was written.")
		return
	}

	if err := applyLinkDirection(direction, filePath); err != nil {
		fmt.Printf("  [Error] %v\n", err)
	}
}

// applyLinkDirection re-runs `add` with the flag matching the chosen direction.
// The flags are cleared on every return path, including an error one: they are
// package vars shared with the CLI, so a stale `true` would silently overwrite
// on the dashboard's next add.
func applyLinkDirection(direction, filePath string) error {
	defer func() { addAdoptRemote, addForce = false, false }()

	switch direction {
	case "adopt":
		addAdoptRemote = true
	case "force":
		addForce = true
	default:
		return fmt.Errorf("unknown link direction %q", direction)
	}

	return addCmd.RunE(addCmd, []string{filePath})
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
// pickerEntry is one directory entry, reduced to what the picker renders. It
// exists so the ordering below is testable without fabricating os.DirEntry.
type pickerEntry struct {
	name  string
	isDir bool
}

// pickerOptions orders one directory's entries for the file browser: visible
// names first, then dot-entries, each group keeping os.ReadDir's alphabetical
// order. Directories get a trailing slash.
//
// Dot-entries are listed rather than skipped. Hiding them made the dashboard
// unable to reach the files automagist mostly exists for — ~/.zshrc could not
// be selected at all, and ~/.config could not be entered. They sort last so a
// home directory full of .cache and .local does not bury everything else; the
// picker enables huh's `/` filter for narrowing either group.
func pickerOptions(entries []pickerEntry) []huh.Option[string] {
	label := func(e pickerEntry) huh.Option[string] {
		if e.isDir {
			return huh.NewOption(e.name+"/", e.name)
		}
		return huh.NewOption(e.name, e.name)
	}

	options := make([]huh.Option[string], 0, len(entries))
	for _, e := range entries {
		if !strings.HasPrefix(e.name, ".") {
			options = append(options, label(e))
		}
	}
	for _, e := range entries {
		if strings.HasPrefix(e.name, ".") {
			options = append(options, label(e))
		}
	}
	return options
}

func runFilteredFileBrowser(startDir string) (string, error) {
	currentDir := startDir
	for {
		entries, err := os.ReadDir(currentDir)
		if err != nil {
			return "", err
		}

		picked := make([]pickerEntry, 0, len(entries))
		for _, entry := range entries {
			picked = append(picked, pickerEntry{name: entry.Name(), isDir: entry.IsDir()})
		}

		options := append([]huh.Option[string]{huh.NewOption(".. (Up)", "..")}, pickerOptions(picked)...)

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
