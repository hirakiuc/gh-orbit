package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Notifications NotificationsConfig `yaml:"notifications"`
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
	}
}

// Load loads the configuration from the XDG config directory.
func Load() (*Config, error) {
	path, err := resolveConfigPath()
	if err != nil {
		return nil, err
	}

	// Create parent directory with 0700 permissions
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Save default config with 0600 permissions
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
	path, err := resolveConfigPath()
	if err != nil {
		return err
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0o600)
}

func resolveConfigPath() (string, error) {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configHome = filepath.Join(home, ".config")
	}

	return filepath.Join(configHome, "gh-orbit", "config.yml"), nil
}
