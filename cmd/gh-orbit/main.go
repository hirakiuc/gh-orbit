package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/dustin/go-humanize"
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
	testMode   bool
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
	rootCmd.PersistentFlags().BoolVar(&testMode, "gh-orbit-test-mode", false, "enable headless test mode")
	
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output report in JSON format")
	doctorCmd.Flags().BoolVar(&doctorTest, "test", false, "trigger a test notification")
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor() error {
	ctx := context.Background()
	logger, logCleanup, _ := config.SetupLogger(logLevel)
	defer func() { _ = logCleanup() }()

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
		FocusMode:     "Unsupported platform",
	}

	// 2. Persistence Audit
	confPath, _ := config.ResolveConfigPath()
	dataPath, _ := config.ResolveDataDir()
	statePath, _ := config.ResolveStateDir()
	tracePath, _ := config.ResolveTracePath()
	
	// Proactively ensure data directory exists for persistence reports
	_ = config.EnsurePrivateDir(dataPath)
	_ = config.EnsurePrivateDir(statePath)
	
	totalSize := getDirSize(dataPath) + getDirSize(statePath)
	if totalSize < 0 { totalSize = 0 }
	
	report.Persistence = api.PersistenceReport{
		ConfigPath: confPath,
		DataPath:   dataPath,
		StatePath:  statePath,
		TracePath:  tracePath,
		CacheSize:  humanize.Bytes(uint64(totalSize)), // #nosec G115: conversion checked above
	}

	// 2.5 Config Health Audit
	cfg, cfgErr := config.Load()
	report.Config = api.ConfigReport{
		Status: "Valid",
	}
	if cfgErr != nil {
		report.Config.Status = "Invalid"
		report.Config.Error = cfgErr.Error()
	} else if cfg != nil {
		report.Config.Version = cfg.Version
	}

	// 3. Initialize Service Stack for High-Fidelity Probe
	database, err := db.OpenInMemory(ctx, logger)
	if err != nil {
		return fmt.Errorf("failed to open diagnostic db: %w", err)
	}
	defer func() { _ = database.Close() }()

	native := api.NewPlatformNotifier(ctx, logger)
	fallback := api.NewBeeepNotifier(logger)
	
	activeCfg := config.DefaultConfig()
	if cfg != nil {
		activeCfg = cfg
	}
	alerts := api.NewAlertService(activeCfg, database, native, fallback, logger)
	defer alerts.Shutdown(ctx)

	if runtime.GOOS == "darwin" {
		alerts.Warmup()
		
		// Deterministic wait for readiness
		select {
		case <-alerts.Ready():
		case <-time.After(2 * time.Second):
		}

		probes := api.ProbeBridge()
		for _, p := range probes {
			report.Checks = append(report.Checks, api.BridgeCheck{
				Name:   p.Name,
				Passed: p.Passed,
				Message: p.Message,
			})
		}
		
		tierName, tierStatus := alerts.ActiveTierInfo()
		report.ActiveTier = tierName
		report.BridgeStatus = tierStatus
		
		// Focus Mode Probe (Darwin only)
		report.FocusMode = api.CheckFocusMode()
	} else {
		report.BridgeStatus = api.StatusUnsupported
		report.ActiveTier = "Beeep (Cross-Platform)"
	}

	// 4. Optional End-to-End Notification Test
	if doctorTest {
		err := alerts.TestNotify(ctx, "Diagnostic Test", "gh-orbit doctor", "This is an end-to-end test notification.")
		
		testPassed := (err == nil)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		report.Checks = append(report.Checks, api.BridgeCheck{
			Name:    "End-to-End Notification Delivery",
			Passed:  testPassed,
			Message: msg,
		})
		
		// Allow small time for async delivery to hit system
		time.Sleep(500 * time.Millisecond)
	}

	// 5. Output
	if doctorJSON {
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(report)
	}

	fmt.Println("🤖 gh-orbit doctor report")
	fmt.Println("==========================")
	fmt.Printf("OS:     %s (%s)\n", report.OS, report.Arch)
	fmt.Printf("Kernel: %s", report.KernelVersion)
	
	focusOut := report.FocusMode
	if strings.Contains(report.FocusMode, "Active") {
		focusOut = "[!] " + report.FocusMode
	}
	fmt.Printf("Focus:  %s\n", focusOut)
	
	fmt.Printf("Status: %s\n", report.BridgeStatus)
	fmt.Printf("Tier:   %s\n", report.ActiveTier)
	
	fmt.Println("\nConfiguration:")
	fmt.Printf("  Version: %d\n", report.Config.Version)
	fmt.Printf("  Status:  %s\n", report.Config.Status)
	if report.Config.Error != "" {
		fmt.Printf("  Error:   \033[31m%s\033[0m\n", report.Config.Error)
	}

	fmt.Println("\nPersistence:")
	fmt.Printf("  Config: %s\n", report.Persistence.ConfigPath)
	fmt.Printf("  Data:   %s\n", report.Persistence.DataPath)
	fmt.Printf("  State:  %s\n", report.Persistence.StatePath)
	fmt.Printf("  Traces: %s\n", report.Persistence.TracePath)
	
	usageColor := "" // Default
	if totalSize > 100*1024*1024 { // > 100MB
		usageColor = "\033[33m" // Yellow ANSI
	}
	fmt.Printf("  Usage:  %s%s\033[0m\n", usageColor, report.Persistence.CacheSize)

	fmt.Println("\nChecks:")
	for _, c := range report.Checks {
		status := "[PASS]"
		if !c.Passed {
			status = "[FAIL]"
		}
		fmt.Printf("%s %-35s %s\n", status, c.Name, c.Message)
	}

	return nil
}

func getDirSize(path string) int64 {
	var size int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil { return nil }
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

type environment struct {
	logger     *slog.Logger
	logCleanup func() error
	otelCleanup func()
	span       trace.Span
}

func initEnvironment(ctx context.Context) (*environment, context.Context, error) {
	// 1. Initialize Logger
	logger, logCleanup, err := config.SetupLogger(logLevel)
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
	client   api.GitHubClient
	userID   string
}

func initResources(ctx context.Context, logger *slog.Logger) (*appResources, error) {
	// 1. gh CLI Check
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("github cli 'gh' not found in PATH: %w", err)
	}

	// 2. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("error loading config: %w", err)
	}

	// 3. Initialize Database
	database, err := db.Open(ctx, logger)
	if err != nil {
		return nil, fmt.Errorf("error opening database: %w", err)
	}

	// 4. Initialize API Client
	client, err := api.NewClient()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("error creating API client: %w", err)
	}

	// 5. Get Current User
	user, err := client.CurrentUser(ctx)
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("error fetching current user: %w", err)
	}
	userID := strconv.FormatInt(user.ID, 10)

	return &appResources{
		config:   cfg,
		database: database,
		client:   client,
		userID:   userID,
	}, nil
}

func run(ctx context.Context) error {
	// 1. Init Environment (Logger, OTel)
	env, ctx, err := initEnvironment(ctx)
	if err != nil {
		return err
	}
	
	// Two-Phase Shutdown: Use WithoutCancel for final flushes
	cleanupCtx := context.WithoutCancel(ctx)
	defer env.span.End()
	defer func() {
		if env.otelCleanup != nil { env.otelCleanup() }
		_ = env.logCleanup()
		env.logger.DebugContext(cleanupCtx, "environment cleanup complete")
	}()

	// 2. Init Resources (Config, DB, Client)
	res, err := initResources(ctx, env.logger)
	if err != nil {
		return err
	}
	defer func() { 
		_ = res.database.Close() 
		env.logger.DebugContext(cleanupCtx, "resource cleanup complete")
	}()
	
	env.span.SetAttributes(attribute.String("user_id", res.userID))

	// 3. Launch TUI
	return launchTUI(ctx, env, res)
}

func launchTUI(ctx context.Context, env *environment, res *appResources) error {
	// Instantiate services via interfaces (Dependency Injection)
	native := api.NewPlatformNotifier(ctx, env.logger)
	fallback := api.NewBeeepNotifier(env.logger)
	alerts := api.NewAlertService(res.config, res.database, native, fallback, env.logger)
	fetcher := api.NewNotificationFetcher(res.client, env.logger)
	syncer := api.NewSyncEngine(fetcher, res.database, alerts, env.logger)
	enricher := api.NewEnrichmentEngine(ctx, res.client, res.database, env.logger)
	traffic := api.NewAPITrafficController(ctx, env.logger)

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
		tui.WithVersion(version),
	)

	var opts []tea.ProgramOption
	if testMode {
		opts = append(opts, tea.WithInput(nil), tea.WithOutput(os.Stderr))
	}

	p := tea.NewProgram(m, opts...)

	// Headless Test Mode: Quit immediately after starting
	if testMode {
		go func() {
			time.Sleep(1 * time.Second) // Give it a moment to run initial commands
			p.Quit()
		}()
	}

	// Context Handling
	go func() {
		<-ctx.Done()
		p.Quit()
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("error running TUI: %w", err)
	}

	// Graceful Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		m.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-shutdownCtx.Done():
		env.logger.WarnContext(ctx, "shutdown timeout reached, forcing exit")
		return nil
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
