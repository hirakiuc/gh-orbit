package main

import (
	"fmt"
	"os"
	"strconv"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gh-orbit",
	Short: "gh-orbit is a GitHub CLI extension for TUI-based notification management",
	RunE: func(cmd *cobra.Command, args []string) error {
		return run()
	},
}

func run() error {
	// 0. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	// 1. Initialize Database
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("error opening database: %w", err)
	}
	defer func() { _ = database.Close() }()

	// 2. Initialize API Client
	client, err := api.NewClient()
	if err != nil {
		return fmt.Errorf("error creating API client: %w", err)
	}

	// 3. Get Current User for scoping
	user, err := client.CurrentUser()
	if err != nil {
		return fmt.Errorf("error fetching current user: %w", err)
	}
	userID := strconv.FormatInt(user.ID, 10)

	// 4. Start TUI
	m := tui.NewModel(database, client, userID, cfg)
	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
