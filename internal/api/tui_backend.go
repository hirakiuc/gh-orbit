package api

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hirakiuc/gh-orbit/internal/github"
	"github.com/hirakiuc/gh-orbit/internal/models"
	"github.com/hirakiuc/gh-orbit/internal/triage"
	"github.com/hirakiuc/gh-orbit/internal/types"
)

// Backend is the in-process application-facing backend owner for TUI operations.
// It composes narrower services but owns direct mutation semantics and their
// corresponding event publication.
type Backend struct {
	UserID   string
	Store    types.NotificationStore
	Client   github.Client
	Syncer   types.Syncer
	Enricher types.Enricher

	resolveUserID func(context.Context) (string, error)
	userMu        sync.RWMutex

	publishNotificationsChanged func()
	publishEnrichmentUpdated    func()
}

func NewBackend(
	userID string,
	store types.NotificationStore,
	syncer types.Syncer,
	enricher types.Enricher,
	client github.Client,
	resolveUserID func(context.Context) (string, error),
	publishNotificationsChanged func(),
	publishEnrichmentUpdated func(),
) (*Backend, error) {
	if store == nil {
		return nil, fmt.Errorf("notification store is required for Backend")
	}
	if syncer == nil {
		return nil, fmt.Errorf("syncer is required for Backend")
	}
	if enricher == nil {
		return nil, fmt.Errorf("enricher is required for Backend")
	}
	if userID == "" && resolveUserID == nil {
		return nil, fmt.Errorf("user ID or resolver is required for Backend")
	}
	if publishNotificationsChanged == nil {
		publishNotificationsChanged = func() {}
	}
	if publishEnrichmentUpdated == nil {
		publishEnrichmentUpdated = func() {}
	}

	return &Backend{
		UserID:                      userID,
		Store:                       store,
		Client:                      client,
		Syncer:                      syncer,
		Enricher:                    enricher,
		resolveUserID:               resolveUserID,
		publishNotificationsChanged: publishNotificationsChanged,
		publishEnrichmentUpdated:    publishEnrichmentUpdated,
	}, nil
}

// NewTUIBackendClient is a temporary constructor alias kept only for
// compatibility while the architecture-v2 migration lands. It must not own any
// behavior beyond constructing the real Backend type.
func NewTUIBackendClient(
	userID string,
	store types.NotificationStore,
	syncer types.Syncer,
	enricher types.Enricher,
	client github.Client,
) (*Backend, error) {
	return NewBackend(userID, store, syncer, enricher, client, nil, nil, nil)
}

func (b *Backend) ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error) {
	return b.Store.ListNotifications(ctx)
}

func (b *Backend) Sync(ctx context.Context, force bool) (models.RateLimitInfo, error) {
	userID, err := b.boundUserID(ctx)
	if err != nil {
		return models.RateLimitInfo{}, err
	}
	return b.Syncer.Sync(ctx, userID, force)
}

func (b *Backend) MarkRead(ctx context.Context, id string, read bool) (types.MarkReadResult, error) {
	if err := b.Store.MarkReadLocally(ctx, id, read); err != nil {
		return types.MarkReadResult{}, err
	}

	b.publishNotificationsChanged()

	if read && b.Client != nil {
		if err := b.Client.MarkThreadAsRead(ctx, id); err != nil {
			return types.MarkReadResult{Status: types.MarkReadRemoteFailure, Err: err}, nil
		}
	}

	return types.MarkReadResult{Status: types.MarkReadSuccess}, nil
}

func (b *Backend) SetPriority(ctx context.Context, id string, priority int) error {
	if err := b.Store.SetPriority(ctx, id, priority); err != nil {
		return err
	}

	b.publishNotificationsChanged()
	return nil
}

func (b *Backend) FetchDetail(ctx context.Context, u string, subjectType string, force bool) (models.EnrichmentResult, error) {
	return b.Enricher.FetchDetail(ctx, u, subjectType, force)
}

func (b *Backend) PersistFetchedDetail(ctx context.Context, id, sourceURL string, res models.EnrichmentResult) error {
	if err := b.Enricher.PersistFetchedDetail(ctx, id, sourceURL, res); err != nil {
		return err
	}

	b.publishEnrichmentUpdated()
	return nil
}

func (b *Backend) FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState, force bool) map[string]models.EnrichmentResult {
	return b.Enricher.FetchHybridBatch(ctx, notifications, force)
}

func (b *Backend) BridgeStatus() types.BridgeStatus {
	return b.Syncer.BridgeStatus()
}

func (b *Backend) Shutdown(ctx context.Context) {
	if b.Syncer != nil {
		b.Syncer.Shutdown(ctx)
	}
	if b.Enricher != nil {
		b.Enricher.Shutdown(ctx)
	}
}

func (b *Backend) boundUserID(ctx context.Context) (string, error) {
	b.userMu.RLock()
	if b.UserID != "" {
		defer b.userMu.RUnlock()
		return b.UserID, nil
	}
	b.userMu.RUnlock()

	if b.resolveUserID == nil {
		return "", fmt.Errorf("backend user ID is unavailable")
	}

	userID, err := b.resolveUserID(ctx)
	if err != nil {
		return "", err
	}
	if userID == "" {
		return "", fmt.Errorf("backend user ID is empty")
	}

	b.userMu.Lock()
	defer b.userMu.Unlock()
	if b.UserID == "" {
		b.UserID = userID
	}
	return b.UserID, nil
}

func IsRemoteMarkReadFailure(err error) bool {
	return err != nil && strings.Contains(err.Error(), "failed to mark read on GitHub")
}
