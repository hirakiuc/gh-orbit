package main

import (
	"fmt"
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gh-orbit",
	Short: "gh-orbit is a GitHub CLI extension for TUI-based notification management",
	Run: func(cmd *cobra.Command, args []string) {
		// 1. Initialize Database
		database, err := db.Open()
		if err != nil {
			fmt.Printf("Error opening database: %v\n", err)
			os.Exit(1)
		}
		defer func() { _ = database.Close() }()

		// 2. Initialize API Client
		client, err := api.NewClient()
		if err != nil {
			fmt.Printf("Error creating API client: %v\n", err)
			os.Exit(1)
		}

		// 3. Get Current User for scoping
		user, err := client.CurrentUser()
		if err != nil {
			fmt.Printf("Error fetching current user: %v\n", err)
			os.Exit(1)
		}
		userID := strconv.FormatInt(user.ID, 10)

		// 4. Start TUI
		m := tui.NewModel(database, client, userID)
		p := tea.NewProgram(m)
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error running TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
