package config

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Version       int                 `yaml:"version"`
	Notifications NotificationsConfig `yaml:"notifications"`
	Enrichment    EnrichmentConfig    `yaml:"enrichment"`
	TUI           TUIConfig           `yaml:"tui"`
	Keys          KeyMapConfig        `yaml:"keys"`
}

// KeyMapConfig defines the keyboard shortcuts for the TUI.
type KeyMapConfig struct {
	Sync             []string `yaml:"sync"`
	PriorityUp       []string `yaml:"priority_up"`
	PriorityDown     []string `yaml:"priority_down"`
	PriorityNone     []string `yaml:"priority_none"`
	Inbox            []string `yaml:"inbox"`
	Unread           []string `yaml:"unread"`
	Triaged          []string `yaml:"triaged"`
	All              []string `yaml:"all"`
	CopyURL          []string `yaml:"copy_url"`
	ToggleRead       []string `yaml:"toggle_read"`
	NextTab          []string `yaml:"next_tab"`
	PrevTab          []string `yaml:"prev_tab"`
	CheckoutPR       []string `yaml:"checkout_pr"`
	ViewContextual   []string `yaml:"view_contextual"`
	OpenBrowser      []string `yaml:"open_browser"`
	ToggleDetail     []string `yaml:"toggle_detail"`
	Back             []string `yaml:"back"`
	Quit             []string `yaml:"quit"`
	FilterPR         []string `yaml:"filter_pr"`
	FilterIssue      []string `yaml:"filter_issue"`
	FilterDiscussion []string `yaml:"filter_discussion"`
	Help             []string `yaml:"help"`
}

// TUIConfig represents settings for the Terminal UI.
type TUIConfig struct {
	AutoReadOnOpen bool `yaml:"auto_read_on_open"`
}

// EnrichmentConfig represents the settings for background metadata enrichment.
type EnrichmentConfig struct {
	DebounceMS  int `yaml:"debounce_ms"`
	Concurrency int `yaml:"concurrency"`
	BatchSize   int `yaml:"batch_size"`
}

// NotificationsConfig represents the settings for system alerts.
type NotificationsConfig struct {
	Enabled           bool     `yaml:"enabled"`
	Mute              bool     `yaml:"mute"`
	SyncInterval      int      `yaml:"sync_interval"`
	MaxVisibleAgeDays int      `yaml:"max_visible_age_days"`
	Reasons           []string `yaml:"reasons"`
	IgnoreRepos       []string `yaml:"ignore_repos"`
}

// DefaultConfig returns the default configuration values.
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Notifications: NotificationsConfig{
			Enabled:           true,
			Mute:              false,
			SyncInterval:      60,
			MaxVisibleAgeDays: 365,
			Reasons:           []string{"assign", "mention", "review_requested"},
			IgnoreRepos:       []string{},
		},
		Enrichment: EnrichmentConfig{
			DebounceMS:  250,
			Concurrency: 1,
			BatchSize:   20,
		},
		TUI: TUIConfig{
			AutoReadOnOpen: false,
		},
		Keys: KeyMapConfig{
			Sync:             []string{"r"},
			PriorityUp:       []string{"shift+up", "K"},
			PriorityDown:     []string{"shift+down", "J"},
			PriorityNone:     []string{"0"},
			Inbox:            []string{"1"},
			Unread:           []string{"2"},
			Triaged:          []string{"3"},
			All:              []string{"4"},
			CopyURL:          []string{"y"},
			ToggleRead:       []string{"m"},
			NextTab:          []string{"]", "tab"},
			PrevTab:          []string{"[", "shift+tab"},
			CheckoutPR:       []string{"c"},
			ViewContextual:   []string{"v"},
			OpenBrowser:      []string{"enter"},
			ToggleDetail:     []string{" ", "space"},
			Back:             []string{"esc", "backspace"},
			Quit:             []string{"q"},
			FilterPR:         []string{"p"},
			FilterIssue:      []string{"i"},
			FilterDiscussion: []string{"d"},
			Help:             []string{"?"},
		},
	}
}

// Validate ensures the configuration values are within safe logical boundaries.
func (c *Config) Validate() error {
	// 1. Version Check
	if c.Version < 1 {
		return fmt.Errorf("config version must be >= 1, got %d", c.Version)
	}

	// 2. Notifications Validation
	if c.Notifications.SyncInterval < 10 || c.Notifications.SyncInterval > 3600 {
		return fmt.Errorf("notifications.sync_interval must be between 10 and 3600 seconds, got %d", c.Notifications.SyncInterval)
	}
	if c.Notifications.MaxVisibleAgeDays < 0 || c.Notifications.MaxVisibleAgeDays > 3650 {
		return fmt.Errorf("notifications.max_visible_age_days must be between 0 and 3650 days, got %d", c.Notifications.MaxVisibleAgeDays)
	}

	// 3. Enrichment Validation
	if c.Enrichment.DebounceMS < 50 || c.Enrichment.DebounceMS > 5000 {
		return fmt.Errorf("enrichment.debounce_ms must be between 50 and 5000ms, got %d", c.Enrichment.DebounceMS)
	}

	if c.Enrichment.Concurrency < 1 || c.Enrichment.Concurrency > 10 {
		return fmt.Errorf("enrichment.concurrency must be between 1 and 10, got %d", c.Enrichment.Concurrency)
	}

	// 4. TUI Validation
	// Currently no range constraints for booleans, but maintains structure.

	// 5. KeyMap Validation
	if len(c.Keys.Sync) == 0 {
		return fmt.Errorf("keys.sync must have at least one key defined")
	}
	if len(c.Keys.Quit) == 0 {
		return fmt.Errorf("keys.quit must have at least one key defined")
	}
	if len(c.Keys.Back) == 0 {
		return fmt.Errorf("keys.back must have at least one key defined")
	}
	if len(c.Keys.Help) == 0 {
		return fmt.Errorf("keys.help must have at least one key defined")
	}
	if len(c.Keys.ToggleDetail) == 0 {
		return fmt.Errorf("keys.toggle_detail must have at least one key defined")
	}

	return nil
}

// Load loads the configuration from the XDG config directory.
func Load() (*Config, error) {
	path, err := ResolveConfigPath()
	if err != nil {
		return nil, err
	}

	// Create parent directory with strict permissions
	if err := EnsurePrivateDir(filepath.Dir(path)); err != nil {
		return nil, fmt.Errorf("failed to secure config directory: %w", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		cfg := DefaultConfig()
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}

	dir := filepath.Dir(path)
	name := filepath.Base(path)
	data, err := SecureReadFile(dir, name)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Strict Schema Enforcement:
	// 1. Initialize with defaults to handle missing fields
	cfg := DefaultConfig()

	// 2. Use Decoder with KnownFields(true) to catch typos
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("invalid config.yml (check for typos): %w", err)
	}

	// 3. Semantic Validation
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// Save saves the current configuration to disk.
func (c *Config) Save() error {
	path, err := ResolveConfigPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}

// EnsurePrivateDir creates a directory with 0700 permissions if it doesn't exist.
func EnsurePrivateDir(path string) error {
	return os.MkdirAll(path, 0o700)
}

// AuditPermissions recursively ensures directories are 0700 and files are 0600.
// It only targets files owned by the current user UID.
func AuditPermissions(ctx context.Context, logger *slog.Logger, root string) error {
	uid := os.Getuid()

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Security: Only audit files owned by the current user
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != uid {
			return nil
		}

		if info.IsDir() {
			if info.Mode().Perm() != 0o700 {
				logger.DebugContext(ctx, "hardening directory permissions", "path", path, "mode", "0700")
				// #nosec G302: Intentional directory hardening
				// #nosec G122: Known risk, standard directory permission enforcement
				return os.Chmod(path, 0o700)
			}
		} else {
			if info.Mode().Perm() != 0o600 {
				logger.DebugContext(ctx, "hardening file permissions", "path", path, "mode", "0600")
				// #nosec G122: Known risk, standard file permission enforcement
				return os.Chmod(path, 0o600) // #nosec G302: Intentional file hardening
			}
		}
		return nil
	})
}

// ResolveConfigPath returns the path to config.yml (XDG_CONFIG_HOME/gh/extensions/gh-orbit).
func ResolveConfigPath() (string, error) {
	dir, err := resolveXDG("XDG_CONFIG_HOME", ".config")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh", "extensions", "gh-orbit", "config.yml"), nil
}

// ResolveDataDir returns the path to the data directory (XDG_DATA_HOME).
func ResolveDataDir() (string, error) {
	dir, err := resolveXDG("XDG_DATA_HOME", ".local/share")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh-orbit"), nil
}

// ResolveStateDir returns the path to the state directory (XDG_STATE_HOME).
func ResolveStateDir() (string, error) {
	dir, err := resolveXDG("XDG_STATE_HOME", ".local/state")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "gh-orbit"), nil
}

// ResolveTracePath returns the path to orbit.traces.json (XDG_STATE_HOME/gh-orbit/orbit.traces.json).
func ResolveTracePath() (string, error) {
	dir, err := ResolveStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "orbit.traces.json"), nil
}

func resolveXDG(env, fallbackRel string) (string, error) {
	val := os.Getenv(env)
	if val != "" {
		return val, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to determine user home directory: %w", err)
	}

	return filepath.Join(home, fallbackRel), nil
}
