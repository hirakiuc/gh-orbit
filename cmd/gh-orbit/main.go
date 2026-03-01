package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cli/go-gh/v2"
	"github.com/gen2brain/beeep"
	"github.com/spf13/cobra"
	_ "modernc.org/sqlite"
)

var rootCmd = &cobra.Command{
	Use:   "gh-orbit",
	Short: "gh-orbit is a GitHub CLI extension for TUI-based notification management",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Hello gh-orbit! (Go version)")
		// Dummy references to ensure dependencies are tracked
		_ = tea.NewProgram(nil)
		_ = lipgloss.NewStyle()
		_, _ = gh.CurrentRepository()
		_ = beeep.Alert("title", "message", "")
		_ = list.New([]list.Item{}, nil, 0, 0)
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
