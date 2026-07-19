package engine

import (
	"testing"

	"github.com/hirakiuc/gh-orbit/internal/mocks"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestWireRateLimitReporterRegistersEngineOwnedTrafficCallback(t *testing.T) {
	client := mocks.NewMockClient(t)
	var installed func(models.RateLimitInfo)
	client.EXPECT().SetRateLimitReporter(mock.Anything).Run(func(fn func(models.RateLimitInfo)) {
		installed = fn
	}).Once()

	var reported models.RateLimitInfo
	wireRateLimitReporter(client, func(info models.RateLimitInfo) { reported = info })
	assert.NotNil(t, installed)
	want := models.RateLimitInfo{Remaining: 321}
	installed(want)
	assert.Equal(t, want, reported)
}
