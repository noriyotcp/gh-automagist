package cmd

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List currently monitored files",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Mock data for UI testing
		items := []string{
			"~/.zshrc -> gist: 12345",
			"~/.config/nvim/init.lua -> gist: 67890",
		}
		var selectedItem string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Monitored Files").
					Options(huh.NewOptions(items...)...).
					Value(&selectedItem),
			),
		)

		err := form.Run()
		if err != nil {
			return err
		}

		fmt.Printf("You selected: %s\n", selectedItem)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
