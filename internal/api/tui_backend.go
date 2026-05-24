package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// TUIBackendClient adapts the existing in-process services to the TUI-facing
// backend contract used by the architecture-v2 migration.
type TUIBackendClient struct {
	UserID   string
	Store    types.NotificationStore
	Client   github.Client
	Syncer   types.Syncer
	Enricher types.Enricher
}

func NewTUIBackendClient(userID string, store types.NotificationStore, syncer types.Syncer, enricher types.Enricher, client github.Client) (*TUIBackendClient, error) {
	if userID == "" {
		return nil, fmt.Errorf("user ID is required for TUIBackendClient")
	}
	if store == nil {
		return nil, fmt.Errorf("notification store is required for TUIBackendClient")
	}
	if syncer == nil {
		return nil, fmt.Errorf("syncer is required for TUIBackendClient")
	}
	if enricher == nil {
		return nil, fmt.Errorf("enricher is required for TUIBackendClient")
	}

	return &TUIBackendClient{
		UserID:   userID,
		Store:    store,
		Client:   client,
		Syncer:   syncer,
		Enricher: enricher,
	}, nil
}

func (b *TUIBackendClient) ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error) {
	return b.Store.ListNotifications(ctx)
}

func (b *TUIBackendClient) Sync(ctx context.Context, force bool) (models.RateLimitInfo, error) {
	return b.Syncer.Sync(ctx, b.UserID, force)
}

func (b *TUIBackendClient) MarkRead(ctx context.Context, id string, read bool) (types.MarkReadResult, error) {
	if err := b.Store.MarkReadLocally(ctx, id, read); err != nil {
		return types.MarkReadResult{}, err
	}
	if read && b.Client != nil {
		if err := b.Client.MarkThreadAsRead(ctx, id); err != nil {
			return types.MarkReadResult{Status: types.MarkReadRemoteFailure, Err: err}, nil
		}
	}
	return types.MarkReadResult{Status: types.MarkReadSuccess}, nil
}

func (b *TUIBackendClient) SetPriority(ctx context.Context, id string, priority int) error {
	return b.Store.SetPriority(ctx, id, priority)
}

func (b *TUIBackendClient) FetchDetail(ctx context.Context, u string, subjectType string, force bool) (models.EnrichmentResult, error) {
	return b.Enricher.FetchDetail(ctx, u, subjectType, force)
}

func (b *TUIBackendClient) PersistFetchedDetail(ctx context.Context, id, sourceURL string, res models.EnrichmentResult) error {
	return b.Enricher.PersistFetchedDetail(ctx, id, sourceURL, res)
}

func (b *TUIBackendClient) FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState, force bool) map[string]models.EnrichmentResult {
	return b.Enricher.FetchHybridBatch(ctx, notifications, force)
}

func (b *TUIBackendClient) BridgeStatus() types.BridgeStatus {
	return b.Syncer.BridgeStatus()
}

func (b *TUIBackendClient) Shutdown(ctx context.Context) {
	if b.Syncer != nil {
		b.Syncer.Shutdown(ctx)
	}
	if b.Enricher != nil {
		b.Enricher.Shutdown(ctx)
	}
}

func IsRemoteMarkReadFailure(err error) bool {
	return err != nil && strings.Contains(err.Error(), "failed to mark read on GitHub")
}
