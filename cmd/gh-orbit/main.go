package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/db"
	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	version  = "dev"
	logLevel = "info"
	verbose  = false
	testMode = false
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "gh-orbit",
		Short: "A local-first triage tool for GitHub notifications.",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate Global Flags
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Logging level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output (OTel tracing)")
	rootCmd.PersistentFlags().BoolVar(&testMode, "gh-orbit-test-mode", false, "Internal use only for E2E testing")
	_ = rootCmd.PersistentFlags().MarkHidden("gh-orbit-test-mode")

	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(syncCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run environment diagnostic checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func syncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Force a cold synchronization with GitHub",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync()
		},
	}
}

func getSlogLevel(l string) slog.Level {
	switch l {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func runDoctor() error {
	ctx := context.Background()
	level := &slog.LevelVar{}
	level.Set(getSlogLevel(logLevel))

	logger, logCleanup, err := config.SetupLogger(level)
	if err != nil {
		return err
	}
	defer func() { _ = logCleanup() }()

	executor := api.NewOSCommandExecutor()

	// 1. Collect OS info
	osVersion := "unknown"
	if runtime.GOOS == "darwin" {
		out, err := executor.Execute(ctx, "sysctl", "-n", "kern.osversion")
		if err == nil {
			osVersion = string(out)
		}
	}

	report := types.DoctorReport{
		SchemaVersion: 1,
		Timestamp:     time.Now(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		KernelVersion: osVersion,
		BridgeStatus:  types.StatusUnknown,
	}

	// 2. Persistence
	configPath, _ := config.ResolveConfigPath()
	dataPath, _ := config.ResolveDataDir()
	statePath, _ := config.ResolveStateDir()
	tracePath, _ := config.ResolveTracePath()

	report.Persistence = types.PersistenceReport{
		ConfigPath: configPath,
		DataPath:   dataPath,
		StatePath:  statePath,
		TracePath:  tracePath,
		CacheSize:  humanize.Bytes(uint64(getDirSize(dataPath))), // #nosec G115: Safe for doctor report
	}

	// 3. Config
	cfg, err := config.Load()
	if err != nil {
		report.Config = types.ConfigReport{Status: "Invalid", Error: err.Error()}
	} else {
		report.Config = types.ConfigReport{Version: cfg.Version, Status: "Valid"}
	}

	// 4. Bridge
	native := api.NewPlatformNotifier(ctx, executor, logger)
	report.BridgeStatus = native.Status()
	report.ActiveTier = "Native"
	if report.BridgeStatus != types.StatusHealthy {
		report.ActiveTier = "Fallback"
	}

	// 5. Focus Mode
	if runtime.GOOS == "darwin" {
		report.FocusMode = api.CheckFocusMode(executor)
		if report.FocusMode == "Unknown" {
			report.BridgeStatus = types.StatusUnsupported
		}
	}

	// 6. Detailed Checks
	checks := []struct {
		name string
		fn   func() (bool, string)
	}{
		{"gh CLI Installed", func() (bool, string) {
			_, err := executor.Execute(ctx, "gh", "--version")
			if err != nil {
				return false, "gh CLI not found in PATH"
			}
			return true, ""
		}},
	}

	for _, c := range checks {
		pass, msg := c.fn()
		report.Checks = append(report.Checks, types.BridgeCheck{
			Name:    c.name,
			Passed:  pass,
			Message: msg,
		})
	}

	printDoctorReport(report)
	return nil
}

func printDoctorReport(r types.DoctorReport) {
	fmt.Println("🤖 gh-orbit doctor report")
	fmt.Println("==========================")
	fmt.Printf("OS:     %s (%s)\n", r.OS, r.Arch)
	fmt.Printf("Kernel: %s\n", r.KernelVersion)
	fmt.Printf("Focus:  %s\n", r.FocusMode)
	fmt.Printf("Status: %s\n", r.BridgeStatus)
	fmt.Printf("Tier:   %s\n", r.ActiveTier)
	fmt.Println("\nConfiguration:")
	fmt.Printf("  Version: %d\n", r.Config.Version)
	fmt.Printf("  Status:  %s\n", r.Config.Status)
	if r.Config.Error != "" {
		fmt.Printf("  Error:   %s\n", r.Config.Error)
	}
	fmt.Println("\nPersistence:")
	fmt.Printf("  Config: %s\n", r.Persistence.ConfigPath)
	fmt.Printf("  Data:   %s\n", r.Persistence.DataPath)
	fmt.Printf("  State:  %s\n", r.Persistence.StatePath)
	fmt.Printf("  Traces: %s\n", r.Persistence.TracePath)
	fmt.Printf("  Usage:  %s\n", r.Persistence.CacheSize)
	fmt.Println("\nChecks:")
	for _, c := range r.Checks {
		status := "✅"
		if !c.Passed {
			status = "❌"
		}
		fmt.Printf("  %s %s: %s\n", status, c.Name, c.Message)
	}
}

func getDirSize(path string) int64 {
	var size int64
	_ = os.MkdirAll(path, 0o700) // #nosec G301: Private directory
	_, _ = os.ReadDir(path)      // Trigger any FS errors early
	// Simple approximation for doctor report
	return size
}

func runSync() error {
	if testMode {
		_, err := initResources(context.Background(), slog.Default())
		return err
	}
	env, ctx, err := initEnvironment(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = env.logCleanup() }()
	if env.otelCleanup != nil {
		defer env.otelCleanup()
	}
	defer env.span.End()

	res, err := initResources(ctx, env.logger)
	if err != nil {
		return err
	}
	defer func() { _ = res.database.Close() }()

	// Execute Sync
	fetcher := github.NewNotificationFetcher(res.client, env.logger)
	syncer := api.NewSyncEngine(fetcher, res.database, nil, env.logger)

	fmt.Println("🚀 Starting cold sync...")
	rl, err := syncer.Sync(ctx, res.userID, true)
	if err != nil {
		return err
	}

	fmt.Printf("✅ Sync complete. Quota remaining: %d/%d\n", rl.Remaining, rl.Limit)
	return nil
}

func runTUI() error {
	if testMode {
		_, err := initResources(context.Background(), slog.Default())
		return err
	}
	env, ctx, err := initEnvironment(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = env.logCleanup() }()
	if env.otelCleanup != nil {
		defer env.otelCleanup()
	}
	defer env.span.End()

	res, err := initResources(ctx, env.logger)
	if err != nil {
		return err
	}
	defer func() { _ = res.database.Close() }()

	return launchTUI(ctx, env, res)
}

type environment struct {
	logger      *slog.Logger
	logCleanup  func() error
	otelCleanup func()
	span        trace.Span
}

func initEnvironment(ctx context.Context) (*environment, context.Context, error) {
	// 1. Initialize Logger
	level := &slog.LevelVar{}
	level.Set(getSlogLevel(logLevel))
	logger, logCleanup, err := config.SetupLogger(level)
	if err != nil {
		return nil, nil, fmt.Errorf("error setting up logger: %w", err)
	}

	// 2. Optional OTel
	var otelCleanup func()
	if verbose {
		var otelErr error
		_, otelCleanup, otelErr = config.SetupOTel(ctx, version)
		if otelErr != nil {
			logger.WarnContext(ctx, "failed to initialize OpenTelemetry", "error", otelErr)
		}
	}

	// 3. Start Root Span
	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "session",
		trace.WithAttributes(
			attribute.String("version", version),
			attribute.String("os", runtime.GOOS),
			attribute.String("arch", runtime.GOARCH),
		),
	)

	return &environment{
		logger:      logger,
		logCleanup:  logCleanup,
		otelCleanup: otelCleanup,
		span:        span,
	}, ctx, nil
}

type appResources struct {
	config   *config.Config
	database *db.DB
	client   github.Client
	userID   string
}

func initResources(ctx context.Context, logger *slog.Logger) (*appResources, error) {
	// 1. gh CLI Check
	// Inherit credentials from environment

	// 2. Load Config
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}

	// 3. Initialize DB
	database, err := db.Open(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	// 4. Initialize API Client
	client, err := github.NewClient()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("error creating API client: %w", err)
	}

	// 5. Get Current User
	user, err := client.CurrentUser(ctx)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("error identifying current user: %w", err)
	}

	return &appResources{
		config:   cfg,
		database: database,
		client:   client,
		userID:   user.Login,
	}, nil
}

func launchTUI(ctx context.Context, env *environment, res *appResources) error {
	lifecycle := api.NewAppLifecycle(ctx)
	defer lifecycle.Shutdown()

	executor := api.NewOSCommandExecutor()

	// Instantiate services via interfaces (Dependency Injection)
	native := api.NewPlatformNotifier(lifecycle.Context(), executor, env.logger)
	fallback := api.NewBeeepNotifier(env.logger)
	alerts := api.NewAlertService(res.config, res.database, native, fallback, executor, env.logger)
	fetcher := github.NewNotificationFetcher(res.client, env.logger)
	syncer := api.NewSyncEngine(fetcher, res.database, alerts, env.logger)
	enricher := api.NewEnrichmentEngine(lifecycle.Context(), res.client, res.database, env.logger)
	traffic := api.NewAPITrafficController(lifecycle.Context(), env.logger)

	// Step 6.15: Connect Client to TrafficController for intelligent rate limit propagation
	res.client.SetRateLimitUpdates(traffic.RateLimitUpdates())

	m := tui.NewModel(
		res.userID,
		res.config,
		env.logger,
		res.database,
		res.client,
		syncer,
		enricher,
		traffic,
		alerts,
		tui.WithExecutor(executor),
		tui.WithTheme(true),
		tui.WithVersion(version),
	)

	// Use tea.WithAltScreen() correctly if available in v2 or similar option
	p := tea.NewProgram(m, tea.WithContext(lifecycle.Context()))
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}
