package config

import (
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
	Notifications NotificationsConfig `yaml:"notifications"`
	Enrichment    EnrichmentConfig    `yaml:"enrichment"`
}

// EnrichmentConfig represents the settings for background metadata enrichment.
type EnrichmentConfig struct {
	DebounceMS  int `yaml:"debounce_ms"`
	Concurrency int `yaml:"concurrency"`
	BatchSize   int `yaml:"batch_size"`
}

// NotificationsConfig represents the settings for system alerts.
type NotificationsConfig struct {
	Enabled      bool     `yaml:"enabled"`
	Mute         bool     `yaml:"mute"`
	SyncInterval int      `yaml:"sync_interval"`
	Reasons      []string `yaml:"reasons"`
	IgnoreRepos  []string `yaml:"ignore_repos"`
}

// DefaultConfig returns the default configuration values.
func DefaultConfig() *Config {
	return &Config{
		Notifications: NotificationsConfig{
			Enabled:      true,
			Mute:         false,
			SyncInterval: 60,
			Reasons:      []string{"assign", "mention", "review_requested"},
			IgnoreRepos:  []string{},
		},
		Enrichment: EnrichmentConfig{
			DebounceMS:  250,
			Concurrency: 1,
			BatchSize:   20,
		},
	}
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
		return cfg, nil
	}

	data, err := os.ReadFile(path) // #nosec G304: Path is internally resolved following XDG specs
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
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
				return os.Chmod(path, 0o700) // #nosec G302: Intentional directory hardening
			}
		} else {
			if info.Mode().Perm() != 0o600 {
				logger.DebugContext(ctx, "hardening file permissions", "path", path, "mode", "0600")
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
