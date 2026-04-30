package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/hirakiuc/gh-orbit/internal/api"
	"github.com/hirakiuc/gh-orbit/internal/buildinfo"
	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/hirakiuc/gh-orbit/internal/engine"
	"github.com/hirakiuc/gh-orbit/internal/engine/transport"
	"github.com/hirakiuc/gh-orbit/internal/tui"
	"github.com/hirakiuc/gh-orbit/internal/types"
	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/spf13/cobra"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var (
	logLevel = "info"
	verbose  = false
	testMode = false
)

func main() {
	rootCmd := &cobra.Command{
		Use:     "gh-orbit",
		Short:   "A local-first triage tool for GitHub notifications.",
		Version: buildinfo.Version,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTUI()
		},
	}

	rootCmd.SetVersionTemplate(fmt.Sprintf("gh-orbit {{.Version}} (%s) build %s\n", buildinfo.Commit, buildinfo.Date))

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Logging level (debug, info, warn, error)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output (OTel tracing)")
	rootCmd.PersistentFlags().BoolVar(&testMode, "gh-orbit-test-mode", false, "Internal use only for E2E testing")
	_ = rootCmd.PersistentFlags().MarkHidden("gh-orbit-test-mode")

	rootCmd.AddCommand(doctorCmd())
	rootCmd.AddCommand(syncCmd())
	rootCmd.AddCommand(engineCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func engineCmd() *cobra.Command {
	var socketPath string
	var insecure bool

	cmd := &cobra.Command{
		Use:   "engine",
		Short: "Start the headless MCP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if socketPath == "" {
				// Use private runtime directory as default
				runtimeDir := os.ExpandEnv("$XDG_RUNTIME_DIR/gh-orbit")
				if runtimeDir == "" || runtimeDir == "/gh-orbit" {
					home, _ := os.UserHomeDir()
					runtimeDir = filepath.Join(home, ".local/run/gh-orbit")
				}
				socketPath = filepath.Join(runtimeDir, "engine.sock")
			}
			return runEngine(socketPath, insecure)
		},
	}

	cmd.Flags().StringVar(&socketPath, "socket", "", "Custom path for the Unix Domain Socket")
	cmd.Flags().BoolVar(&insecure, "insecure-dev", false, "Disable peer verification for local development")

	return cmd
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

func resolveLogLevel(baseLevel string, isVerbose bool) slog.Level {
	if isVerbose {
		return slog.LevelDebug
	}
	return getSlogLevel(baseLevel)
}

func runDoctor() error {
	if testMode {
		return nil
	}
	ctx := context.Background()

	// Pass os.Stderr for doctor if verbose is set, otherwise use file
	var sink io.Writer
	if verbose {
		sink = os.Stderr
	}

	env, ctx, err := initEnvironment(ctx, sink)
	if err != nil {
		return err
	}
	defer func() { _ = env.logCleanup() }()

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
		Build: types.BuildReport{
			Version: buildinfo.Version,
			Commit:  buildinfo.Commit,
			Date:    buildinfo.Date,
		},
		BridgeStatus: types.StatusUnknown,
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
		CacheSize:  "0 B", // Placeholder
	}

	// 3. Config
	cfg, err := config.Load()
	if err != nil {
		report.Config = types.ConfigReport{Status: "Invalid", Error: err.Error()}
	} else {
		report.Config = types.ConfigReport{Version: cfg.Version, Status: "Valid"}
	}

	// 4. Bridge
	native := api.NewPlatformNotifier(ctx, executor, env.logger)
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
	fmt.Printf("Version: %s\n", r.Build.Version)
	fmt.Printf("Commit:  %s\n", r.Build.Commit)
	fmt.Printf("Build:   %s\n", r.Build.Date)
	fmt.Printf("OS:      %s (%s)\n", r.OS, r.Arch)
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

func runSync() error {
	env, ctx, err := initEnvironment(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = env.logCleanup() }()
	if env.otelCleanup != nil {
		defer env.otelCleanup()
	}
	defer env.span.End()

	executor := api.NewOSCommandExecutor()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	eng, err := engine.NewCoreEngine(ctx, cfg, env.logger, executor, engine.WithSilentAlerts())
	if err != nil {
		return err
	}
	defer eng.Shutdown(ctx)

	if testMode {
		return nil
	}

	user, err := eng.Client.CurrentUser(ctx)
	if err != nil {
		return err
	}

	fmt.Println("🚀 Starting cold sync...")
	rl, err := eng.Sync.Sync(ctx, user.Login, true)
	if err != nil {
		return err
	}

	fmt.Printf("✅ Sync complete. Quota remaining: %d/%d\n", rl.Remaining, rl.Limit)
	return nil
}

func runTUI() error {
	env, ctx, err := initEnvironment(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = env.logCleanup() }()
	if env.otelCleanup != nil {
		defer env.otelCleanup()
	}
	defer env.span.End()

	executor := api.NewOSCommandExecutor()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// 1. Attempt MCP Client Mode
	runtimeDir := os.ExpandEnv("$XDG_RUNTIME_DIR/gh-orbit")
	if runtimeDir == "" || runtimeDir == "/gh-orbit" {
		home, _ := os.UserHomeDir()
		runtimeDir = filepath.Join(home, ".local/run/gh-orbit")
	}
	socketPath := filepath.Join(runtimeDir, "engine.sock")
	requireEngine := os.Getenv("GH_ORBIT_REQUIRE_ENGINE") == "1"

	if _, err := os.Stat(socketPath); err == nil {
		env.logger.Debug("detecting headless engine", "socket", socketPath)
		// Try to connect
		t := transport.NewUDSClientTransport(socketPath)
		mcpClient := client.NewClient(t)

		// Attempt start with 2s timeout
		connectCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if err := mcpClient.Start(connectCtx); err == nil {
			// Handshake (Initialize)
			initResp, err := mcpClient.Initialize(connectCtx, mcp.InitializeRequest{
				Params: mcp.InitializeParams{
					ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
					ClientInfo:      mcp.Implementation{Name: "gh-orbit-tui", Version: buildinfo.Version},
				},
			})

			if err == nil {
				env.logger.Info("connected to headless engine", "server", initResp.ServerInfo.Name)
				adapter := engine.NewMCPAdapter(mcpClient)

				user := "mcp-user" // Placeholder until tool added
				return launchTUIMCP(ctx, env, adapter, user)
			}
			if requireEngine {
				return fmt.Errorf("cockpit-managed launch requires MCP engine initialization: %w", err)
			}
			env.logger.Warn("mcp handshake failed, falling back to standalone", "error", err)
		} else {
			if requireEngine {
				return fmt.Errorf("cockpit-managed launch requires MCP engine connection: %w", err)
			}
			env.logger.Warn("failed to connect to engine UDS, falling back to standalone", "error", err)
		}
	} else if requireEngine {
		return fmt.Errorf("cockpit-managed launch requires a running MCP engine at %s", socketPath)
	}

	// 2. Standalone Mode (Library access)
	eng, err := engine.NewCoreEngine(ctx, cfg, env.logger, executor)
	if err != nil {
		return err
	}
	defer eng.Shutdown(ctx)

	if testMode {
		return nil
	}

	user, err := eng.Client.CurrentUser(ctx)
	if err != nil {
		return err
	}

	return launchTUIStandalone(ctx, env, eng, user.Login)
}

func launchTUIMCP(ctx context.Context, env *environment, adapter *engine.MCPAdapter, userID string) error {
	m, err := tui.NewModel(tui.ModelParams{
		UserID:   userID,
		Config:   config.DefaultConfig(), // Placeholder or from engine
		Logger:   env.logger,
		DB:       adapter, // Repository
		Client:   nil,     // Client (unused in MCP mode)
		Syncer:   adapter, // Syncer
		Enricher: adapter, // Enricher
		Traffic:  nil,     // Traffic (Engine handles it)
		Alerter:  adapter, // Alerter (Mock/Engine bridge)
		Options: []tui.Option{
			tui.WithConnectionMode("Connected"),
			tui.WithVersion(buildinfo.Version),
		},
	})
	if err != nil {
		return err
	}

	return runProgram(ctx, m)
}

func launchTUIStandalone(ctx context.Context, env *environment, eng *engine.CoreEngine, userID string) error {
	// Step 6.15: Connect Client to TrafficController for intelligent rate limit propagation
	eng.Client.SetRateLimitUpdates(eng.Traffic.RateLimitUpdates())

	m, err := tui.NewModel(tui.ModelParams{
		UserID:   userID,
		Config:   eng.Config,
		Logger:   env.logger,
		DB:       eng.DB,
		Client:   eng.Client,
		Syncer:   eng.Sync,
		Enricher: eng.Enrich,
		Traffic:  eng.Traffic,
		Alerter:  eng.Alert,
		Options: []tui.Option{
			tui.WithExecutor(api.NewOSCommandExecutor()),
			tui.WithTheme(true),
			tui.WithConnectionMode("Standalone"),
			tui.WithVersion(buildinfo.Version),
		},
	})
	if err != nil {
		return err
	}

	return runProgram(ctx, m)
}

func runProgram(ctx context.Context, m *tui.Model) error {
	lifecycle := api.NewAppLifecycle(ctx)
	defer lifecycle.Shutdown()

	p := tea.NewProgram(m, tea.WithContext(lifecycle.Context()))
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}

func runEngine(socketPath string, insecure bool) error {
	// engine mode ALWAYS logs to stderr for process supervisor visibility
	env, ctx, err := initEnvironment(context.Background(), os.Stderr)
	if err != nil {
		return err
	}
	defer func() { _ = env.logCleanup() }()
	if env.otelCleanup != nil {
		defer env.otelCleanup()
	}
	defer env.span.End()

	executor := api.NewOSCommandExecutor()
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	eng, err := engine.NewCoreEngine(ctx, cfg, env.logger, executor)
	if err != nil {
		return err
	}
	defer eng.Shutdown(ctx)

	server := engine.NewMCPServer(eng, socketPath, insecure, verbose)
	return server.Serve(ctx)
}

type environment struct {
	logger      *slog.Logger
	logCleanup  func() error
	otelCleanup func()
	span        trace.Span
}

func initEnvironment(ctx context.Context, sink io.Writer) (*environment, context.Context, error) {
	level := &slog.LevelVar{}
	level.Set(resolveLogLevel(logLevel, verbose))
	logger, logCleanup, err := config.SetupLogger(level, sink)
	if err != nil {
		return nil, nil, fmt.Errorf("error setting up logger: %w", err)
	}

	var otelCleanup func()
	if verbose {
		var otelErr error
		_, otelCleanup, otelErr = config.SetupOTel(ctx, buildinfo.Version)
		if otelErr != nil {
			logger.WarnContext(ctx, "failed to initialize OpenTelemetry", "error", otelErr)
		}
	}

	tracer := config.GetTracer()
	ctx, span := tracer.Start(ctx, "session",
		trace.WithAttributes(
			attribute.String("version", buildinfo.Version),
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
