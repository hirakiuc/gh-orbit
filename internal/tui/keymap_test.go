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

	cfg.Keys.ToggleHandled = []string{"m", "h"}
	partialCollision := NewKeyMap(cfg)
	assert.Equal(t, []string{"h"}, partialCollision.ToggleHandled.Keys())
	assert.Equal(t, []string{"m", "h"}, cfg.Keys.ToggleHandled, "runtime collision filtering must not mutate persisted intent")

	cfg.Keys.ToggleHandled = []string{"m"}
	fullyCollided := NewKeyMap(cfg)
	assert.False(t, fullyCollided.ToggleHandled.Enabled())
}

func TestKeyMap_BatchBindingsRespectEstablishedCollisions(t *testing.T) {
	cfg := config.DefaultConfig()
	keys := NewKeyMap(cfg)
	assert.True(t, keys.SelectionMode.Enabled())
	assert.True(t, keys.SelectNotification.Enabled())
	assert.True(t, keys.BatchRead.Enabled())
	assert.Contains(t, keys.FullHelp()[2], keys.BatchHandled)

	cfg.Keys.SelectionMode = []string{"m"}
	collided := NewKeyMap(cfg)
	assert.False(t, collided.SelectionMode.Enabled(), "existing scalar binding must win")
}
