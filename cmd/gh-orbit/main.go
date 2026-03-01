package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gh-orbit",
	Short: "gh-orbit is a GitHub CLI extension for TUI-based notification management",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Hello gh-orbit! (Go version)")
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
