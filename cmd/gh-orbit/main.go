package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/spf13/cobra"
)

var (
	verbose  bool
	logLevel = &slog.LevelVar{} // Default is LevelInfo
	version  = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "gh-orbit",
	Short: "gh-orbit is a GitHub CLI extension for TUI-based notification management",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if verbose {
			logLevel.Set(slog.LevelDebug)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		// Use signal.NotifyContext for modern Go signal handling
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		return run(ctx)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")
}

func run(ctx context.Context) error {
	// 0. Initialize Logger with dynamic level handle
	logger, logCleanup, err := config.SetupLogger(logLevel)
	if err != nil {
		return fmt.Errorf("error setting up logger: %w", err)
	}

	// 0. Optional OpenTelemetry (Opt-in via --verbose)
	var otelCleanup func()
	if verbose {
		_, otelCleanup, err = config.SetupOTel(ctx, version)
		if err != nil {
			logger.Warn("failed to initialize OpenTelemetry", "error", err)
		}
	}

	// Define Cascading Cleanup Sequence
	cleanup := func() {
		// 1. Wait for background workers (via TUI model shutdown)
		// 2. OTel Flush
		if otelCleanup != nil {
			otelCleanup()
		}
		// 3. Logger Flush
		_ = logCleanup()
	}
	defer cleanup()

	// 0. Check for gh CLI
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("github cli 'gh' not found in PATH: %w", err)
	}

	// 0. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("error loading config: %w", err)
	}

	// 1. Initialize Database
	database, err := db.Open(logger)
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
	m := tui.NewModel(ctx, database, client, userID, cfg, logger)
	p := tea.NewProgram(&m)

	// Background goroutine to handle context cancellation (Ctrl+C / SIGTERM)
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	// 5. Shutdown Sequence with Timeout Guard
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	done := make(chan struct{})
	go func() {
		m.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-shutdownCtx.Done():
		logger.Warn("shutdown timeout reached, forcing exit")
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
