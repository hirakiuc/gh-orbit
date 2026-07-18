package tui

import (
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestKeyMap_HandledBindingHelpAndDisablement(t *testing.T) {
	cfg := config.DefaultConfig()
	keys := NewKeyMap(cfg)
	assert.True(t, keys.ToggleHandled.Enabled())
	assert.Equal(t, "x", keys.ToggleHandled.Help().Key)
	assert.Contains(t, keys.ShortHelp(), keys.ToggleRead)
	assert.Contains(t, keys.ShortHelp(), keys.ToggleHandled)
	assert.Contains(t, keys.FullHelp()[1], keys.ToggleRead)
	assert.Contains(t, keys.FullHelp()[1], keys.ToggleHandled)

	cfg.Keys.ToggleHandled = nil
	disabled := NewKeyMap(cfg)
	assert.False(t, disabled.ToggleHandled.Enabled())
	assert.Empty(t, disabled.ToggleHandled.Keys())
}
