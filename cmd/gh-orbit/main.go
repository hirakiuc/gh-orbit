package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	verbose  bool
	logLevel = &slog.LevelVar{} // Default is LevelInfo
	version  = "dev"
)

// doctor flags
var (
	doctorJSON bool
	doctorTest bool
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

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check the health of the application environment",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor()
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose logging")
	
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output report in JSON format")
	doctorCmd.Flags().BoolVar(&doctorTest, "test", false, "trigger a test notification")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor() error {
	ctx := context.Background()
	
	// 1. Collect OS info
	osVersion := "unknown"
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("sysctl", "-n", "kern.osversion").Output()
		if err == nil {
			osVersion = string(out)
		}
	}

	execPath, _ := os.Executable()

	report := api.DoctorReport{
		SchemaVersion: 1,
		Timestamp:     time.Now(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		KernelVersion: osVersion,
		BinaryPath:    execPath,
		BridgeStatus:  api.StatusUnknown,
	}

	// 2. Probe Bridge
	if runtime.GOOS == "darwin" {
		// Create a temporary notifier to trigger warmup logic
		logger, cleanup, _ := config.SetupLogger(logLevel)
		defer func() { _ = cleanup() }()
		n := api.NewPlatformNotifier(ctx, logger)
		n.Warmup()
		// Wait a small amount for the async warmup to complete
		time.Sleep(100 * time.Millisecond)

		probes := api.ProbeBridge()
		allPassed := true
		for _, p := range probes {
			report.Checks = append(report.Checks, api.BridgeCheck{
				Name:   p.Name,
				Passed: p.Passed,
			})
			if !p.Passed {
				allPassed = false
			}
		}
		if allPassed {
			report.BridgeStatus = api.StatusHealthy
		} else {
			report.BridgeStatus = api.StatusBroken
		}
	} else {
		report.BridgeStatus = api.StatusUnsupported
	}

	// 3. Optional Test Notification
	if doctorTest {
		logger, cleanup, _ := config.SetupLogger(logLevel)
		defer func() { _ = cleanup() }()
		
		notifier := api.NewPlatformNotifier(ctx, logger)
		err := notifier.Notify("Diagnostic Test", "gh-orbit doctor", "This is a test notification.", "", 1)
		
		testPassed := (err == nil)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		report.Checks = append(report.Checks, api.BridgeCheck{
			Name:    "End-to-End Notification Test",
			Passed:  testPassed,
			Message: msg,
		})
		
		// Wait for async delivery
		time.Sleep(1 * time.Second)
		notifier.Shutdown()
	}

	// 4. Output
	if doctorJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	fmt.Println("🤖 gh-orbit doctor report")
	fmt.Println("==========================")
	fmt.Printf("OS:     %s (%s)\n", report.OS, report.Arch)
	fmt.Printf("Kernel: %s", report.KernelVersion)
	fmt.Printf("Status: %s\n", report.BridgeStatus)
	fmt.Println("\nChecks:")
	for _, c := range report.Checks {
		status := "[PASS]"
		if !c.Passed {
			status = "[FAIL]"
		}
		fmt.Printf("%s %s %s\n", status, c.Name, c.Message)
	}

	return nil
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
		var otelErr error
		_, otelCleanup, otelErr = config.SetupOTel(ctx, version)
		if otelErr != nil {
			logger.WarnContext(ctx, "failed to initialize OpenTelemetry", "error", otelErr)
		}
	}

	// Root Session Span
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "session",
		trace.WithAttributes(
			attribute.String("version", version),
			attribute.String("os", runtime.GOOS),
			attribute.String("arch", runtime.GOARCH),
		),
	)
	defer span.End()

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
	database, err := db.Open(ctx, logger)
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
	span.SetAttributes(attribute.String("user_id", userID))

	// 4. Start TUI
	m := tui.NewModel(ctx, database, client, userID, cfg, logger, version)
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
		logger.WarnContext(ctx, "shutdown timeout reached, forcing exit")
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
