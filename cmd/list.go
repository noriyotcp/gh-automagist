package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/noriyo_tcp/gh-automagist/pkg/state"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List currently monitored files",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := runListInteractive()
		return err
	},
}

// runListInteractive runs the interactive file list.
// Returns (backed, error): backed is true if the user chose "← Back".
func runListInteractive() (bool, error) {
	sm, err := state.NewManager()
	if err != nil {
		return false, err
	}
	if err := sm.Load(); err != nil {
		return false, err
	}

	if len(sm.Files) == 0 {
		fmt.Println("No monitored files found.")
		return false, nil
	}

	for {
		var options []huh.Option[string]
		options = append(options, huh.NewOption("← Back to Dashboard", "__back__"))
		for path, info := range sm.Files {
			label := fmt.Sprintf("%s | %s", path, info.GistID)
			options = append(options, huh.NewOption(label, label))
		}

		var selectedItem string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select a file (press / to search)").
					Options(options...).
					Value(&selectedItem),
			),
		)

		if err := form.Run(); err != nil {
			return false, err
		}

		if selectedItem == "__back__" {
			return true, nil
		}

		// selectedItem format is "path | gist_id"
		parts := strings.Split(selectedItem, " | ")
		if len(parts) != 2 {
			return false, fmt.Errorf("invalid selection parsed")
		}

		path := parts[0]
		gistID := parts[1]

		// Inner loop: stay on the action menu for this file until a definitive action or cancel
		for {
			var action string
			actionForm := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title(fmt.Sprintf("Select action for: %s", filepath.Base(path))).
						Options(
							huh.NewOption("Edit in $EDITOR", "edit"),
							huh.NewOption("View Gist", "view"),
							huh.NewOption("← Back to File List", "cancel"),
						).
						Value(&action),
				),
			)

			if err := actionForm.Run(); err != nil {
				return false, err
			}

			switch action {
			case "edit":
				editor := os.Getenv("EDITOR")
				if editor == "" {
					editor = "vim"
				}
				cm := exec.Command(editor, path)
				cm.Stdin = os.Stdin
				cm.Stdout = os.Stdout
				cm.Stderr = os.Stderr
				return false, cm.Run()
			case "view":
				cm := exec.Command("gh", "gist", "view", gistID)
				cm.Stdin = os.Stdin
				cm.Stdout = os.Stdout
				cm.Stderr = os.Stderr
				_ = cm.Run()
				// After viewing, return to this action menu
				continue
			case "cancel":
				// Back to file list
			}
			break
		}
	}
}

func init() {
	rootCmd.AddCommand(listCmd)
}
