package config

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestConfig_Validate(t *testing.T) {
	t.Run("Valid Default Config", func(t *testing.T) {
		cfg := DefaultConfig()
		require.NoError(t, cfg.Validate())
	})

	t.Run("Invalid Sync Interval (Too Low)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.SyncInterval = 5
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.sync_interval must be between 10 and 3600")
	})

	t.Run("Invalid Sync Interval (Too High)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.SyncInterval = 4000
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.sync_interval must be between 10 and 3600")
	})

	t.Run("Invalid MaxVisibleAgeDays (Too Low)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.MaxVisibleAgeDays = -1
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.max_visible_age_days must be between 0 and 3650 days")
	})

	t.Run("Invalid MaxVisibleAgeDays (Too High)", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Notifications.MaxVisibleAgeDays = 3651
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "notifications.max_visible_age_days must be between 0 and 3650 days")
	})

	t.Run("Invalid DebounceMS", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Enrichment.DebounceMS = 10
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enrichment.debounce_ms must be between 50 and 5000ms")
	})

	t.Run("Invalid Concurrency", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Enrichment.Concurrency = 0
		err := cfg.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "enrichment.concurrency must be between 1 and 10")
	})
}

func TestConfig_Load_Strictness(t *testing.T) {
	tmpDir := t.TempDir()

	// Setup environment override
	err := os.Setenv("XDG_CONFIG_HOME", tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = os.Unsetenv("XDG_CONFIG_HOME")
	})

	// The expected path resolved by our logic
	expectedPath, err := ResolveConfigPath()
	require.NoError(t, err)

	t.Run("Catch Unknown Fields (Typo)", func(t *testing.T) {
		content := `
version: 1
notifications:
  enabled: true
  sync_intreval: 30 # TYPO
`
		err := os.MkdirAll(filepath.Dir(expectedPath), 0o700)
		require.NoError(t, err)

		err = os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		_, err = Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "check for typos")
	})

	t.Run("Catch Unknown Key Fields", func(t *testing.T) {
		content := "version: 1\nkeys:\n  toggle_handeled: [\"x\"]\n"
		require.NoError(t, os.WriteFile(expectedPath, []byte(content), 0o600))
		_, err := Load()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "field toggle_handeled not found")
	})

	t.Run("Successful Load with Safe Defaults", func(t *testing.T) {
		// YAML only provides version and enabled
		content := `
version: 1
notifications:
  enabled: false
`
		// Overwrite the file for this test
		err := os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		cfg, err := Load()
		require.NoError(t, err)
		require.NotNil(t, cfg)
		assert.False(t, cfg.Notifications.Enabled)
		// Verify default was preserved for missing field
		assert.Equal(t, 60, cfg.Notifications.SyncInterval)
		assert.Equal(t, 365, cfg.Notifications.MaxVisibleAgeDays)
		// Verify TUI defaults when section is missing
		assert.False(t, cfg.TUI.AutoReadOnOpen)
	})

	t.Run("Successful Load with Keybinding Overrides", func(t *testing.T) {
		content := `
version: 1
keys:
  sync: ["s"]
  quit: ["escape"]
`
		err := os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		cfg, err := Load()
		require.NoError(t, err)
		require.NotNil(t, cfg)

		// Verify overrides
		assert.Equal(t, []string{"s"}, cfg.Keys.Sync)
		assert.Equal(t, []string{"escape"}, cfg.Keys.Quit)

		// Verify fallback to defaults for other keys
		assert.Equal(t, []string{"r"}, DefaultConfig().Keys.Sync) // Default was 'r'
		assert.Equal(t, []string{"m"}, cfg.Keys.ToggleRead)       // Still 'm'
	})

	t.Run("Old Default Tab Keys Normalize To Three Tabs", func(t *testing.T) {
		content := `
version: 1
keys:
  inbox: ["1"]
  unread: ["2"]
  triaged: ["3"]
  all: ["4"]
`
		err := os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, []string{"1"}, cfg.Keys.Inbox)
		assert.Empty(t, cfg.Keys.Unread)
		assert.Equal(t, []string{"2"}, cfg.Keys.Triaged)
		assert.Equal(t, []string{"3"}, cfg.Keys.All)
	})

	t.Run("Custom Tab Keys Are Preserved", func(t *testing.T) {
		content := `
version: 1
keys:
  inbox: ["g", "i"]
  unread: ["u"]
  triaged: ["t"]
  all: ["a"]
`
		err := os.WriteFile(expectedPath, []byte(content), 0o600)
		require.NoError(t, err)

		cfg, err := Load()
		require.NoError(t, err)

		assert.Equal(t, []string{"g", "i"}, cfg.Keys.Inbox)
		assert.Equal(t, []string{"u"}, cfg.Keys.Unread)
		assert.Equal(t, []string{"t"}, cfg.Keys.Triaged)
		assert.Equal(t, []string{"a"}, cfg.Keys.All)
	})

	t.Run("Handled Binding Presence And Collisions", func(t *testing.T) {
		tests := []struct {
			name    string
			binding string
			want    []string
		}{
			{name: "absent inherits default", want: []string{"x"}},
			{name: "explicit empty disables", binding: "  toggle_handled: []\n", want: []string{}},
			{name: "explicit custom", binding: "  toggle_handled: [\"h\"]\n", want: []string{"h"}},
			{name: "explicit collision preserves configured intent", binding: "  toggle_handled: [\"m\"]\n", want: []string{"m"}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				content := "version: 1\nkeys:\n" + tt.binding
				require.NoError(t, os.WriteFile(expectedPath, []byte(content), 0o600))
				cfg, err := Load()
				require.NoError(t, err)
				assert.Equal(t, tt.want, cfg.Keys.ToggleHandled)
			})
		}
	})

	t.Run("Handled Binding Collisions Round Trip Exactly", func(t *testing.T) {
		for _, binding := range []string{"[\"m\"]", "[\"m\", \"h\"]"} {
			content := "version: 1\nkeys:\n  toggle_handled: " + binding + "\n"
			require.NoError(t, os.WriteFile(expectedPath, []byte(content), 0o600))
			cfg, err := Load()
			require.NoError(t, err)
			want := append([]string(nil), cfg.Keys.ToggleHandled...)
			require.NoError(t, cfg.Save())
			reloaded, err := Load()
			require.NoError(t, err)
			assert.Equal(t, want, reloaded.Keys.ToggleHandled)
		}
	})
}

func TestConfig_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	cfg := DefaultConfig()

	// Test Save (Must ensure parent exists first)
	path, err := ResolveConfigPath()
	require.NoError(t, err)
	require.NoError(t, EnsurePrivateDir(filepath.Dir(path)))
	require.NoError(t, cfg.Save())

	assert.FileExists(t, path)
	data, err := os.ReadFile(path) // #nosec G304 -- path is resolved under t.TempDir.
	require.NoError(t, err)
	assert.NotContains(t, string(data), "toggle_handled:", "inherited defaults must stay readable by old strict decoders")

	explicit := DefaultConfig()
	explicit.Keys.ToggleHandled = []string{"h"}
	explicit.Keys.toggleHandledExplicit = true
	require.NoError(t, explicit.Save())
	data, err = os.ReadFile(path) // #nosec G304 -- path is resolved under t.TempDir.
	require.NoError(t, err)
	assert.Contains(t, string(data), "toggle_handled:")
	assert.Error(t, decodeLegacyKeyConfig(data))
	withoutHandled := strings.ReplaceAll(string(data), "    toggle_handled:\n        - h\n", "")
	assert.NoError(t, decodeLegacyKeyConfig([]byte(withoutHandled)))

	// Test Resolve helpers
	d, err := ResolveDataDir()
	require.NoError(t, err)
	assert.Contains(t, d, tmpDir)

	s, err := ResolveStateDir()
	require.NoError(t, err)
	assert.Contains(t, s, tmpDir)

	tp, err := ResolveTracePath()
	require.NoError(t, err)
	assert.Contains(t, tp, tmpDir)
}

func decodeLegacyKeyConfig(data []byte) error {
	var legacy struct {
		Version       int                 `yaml:"version"`
		Notifications NotificationsConfig `yaml:"notifications"`
		Enrichment    EnrichmentConfig    `yaml:"enrichment"`
		TUI           TUIConfig           `yaml:"tui"`
		Keys          legacyKeyMapConfig  `yaml:"keys"`
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	return decoder.Decode(&legacy)
}

type legacyKeyMapConfig struct {
	Sync                 []string `yaml:"sync"`
	PriorityUp           []string `yaml:"priority_up"`
	PriorityDown         []string `yaml:"priority_down"`
	PriorityNone         []string `yaml:"priority_none"`
	Inbox                []string `yaml:"inbox"`
	Unread               []string `yaml:"unread"`
	Triaged              []string `yaml:"triaged"`
	All                  []string `yaml:"all"`
	CopyURL              []string `yaml:"copy_url"`
	ToggleRead           []string `yaml:"toggle_read"`
	NextTab              []string `yaml:"next_tab"`
	PrevTab              []string `yaml:"prev_tab"`
	CheckoutPR           []string `yaml:"checkout_pr"`
	StartReviewWorkspace []string `yaml:"start_review_workspace"`
	ViewContextual       []string `yaml:"view_contextual"`
	OpenBrowser          []string `yaml:"open_browser"`
	ToggleDetail         []string `yaml:"toggle_detail"`
	Back                 []string `yaml:"back"`
	Quit                 []string `yaml:"quit"`
	FilterPR             []string `yaml:"filter_pr"`
	FilterIssue          []string `yaml:"filter_issue"`
	FilterDiscussion     []string `yaml:"filter_discussion"`
	Help                 []string `yaml:"help"`
}

func TestConfig_AuditPermissions(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	// Isolate from real user paths
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	t.Setenv("XDG_DATA_HOME", tmpDir)
	t.Setenv("XDG_STATE_HOME", tmpDir)

	// Create dir with loose permissions
	subDir := filepath.Join(tmpDir, "loose")
	require.NoError(t, os.MkdirAll(subDir, 0o777)) // #nosec G301: Intentional loose perms for audit test

	fPath := filepath.Join(subDir, "file.txt")
	require.NoError(t, os.WriteFile(fPath, []byte("data"), 0o666)) // #nosec G306: Intentional loose perms for audit test

	// Audit
	require.NoError(t, AuditPermissions(ctx, slog.Default(), tmpDir))

	// Verify hardening
	info, err := os.Stat(subDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	tp, err := ResolveTracePath()
	require.NoError(t, err)
	assert.Contains(t, tp, tmpDir)
}
