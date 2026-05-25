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
	before, _ := b.Store.ListNotifications(ctx)

	if err := b.Store.MarkReadLocally(ctx, id, read); err != nil {
		notifications, reloadErr := b.Store.ListNotifications(ctx)
		if reloadErr != nil {
			if before != nil {
				return types.MarkReadResult{
					Status:        types.MarkReadLocalFailure,
					Notifications: before,
					Toast:         "Failed to update read state",
					Err:           err,
				}, nil
			}
			return types.MarkReadResult{}, fmt.Errorf("reload notifications after local read failure: %w (original error: %v)", reloadErr, err)
		}
		return types.MarkReadResult{
			Status:        types.MarkReadLocalFailure,
			Notifications: notifications,
			Toast:         "Failed to update read state",
			Err:           err,
		}, nil
	}

	b.publishNotificationsChanged()

	if read && b.Client != nil {
		if err := b.Client.MarkThreadAsRead(ctx, id); err != nil {
			notifications, reloadErr := b.Store.ListNotifications(ctx)
			if reloadErr != nil {
				notifications = withReadState(before, id, read)
			}
			return types.MarkReadResult{
				Status:        types.MarkReadRemoteFailure,
				Notifications: notifications,
				Toast:         "Marked read locally; GitHub sync failed",
				Err:           err,
			}, nil
		}
	}

	notifications, err := b.Store.ListNotifications(ctx)
	if err != nil {
		if before != nil {
			notifications = withReadState(before, id, read)
		} else {
			return types.MarkReadResult{}, err
		}
	}

	return types.MarkReadResult{
		Status:        types.MarkReadSuccess,
		Notifications: notifications,
	}, nil
}

func (b *Backend) SetPriority(ctx context.Context, id string, priority int) (types.PriorityUpdateResult, error) {
	before, _ := b.Store.ListNotifications(ctx)

	if err := b.Store.SetPriority(ctx, id, priority); err != nil {
		notifications, reloadErr := b.Store.ListNotifications(ctx)
		if reloadErr != nil {
			if before != nil {
				return types.PriorityUpdateResult{
					Status:        types.PriorityUpdateFailure,
					Notifications: before,
					Err:           err,
				}, nil
			}
			return types.PriorityUpdateResult{}, fmt.Errorf("reload notifications after priority update failure: %w (original error: %v)", reloadErr, err)
		}
		return types.PriorityUpdateResult{
			Status:        types.PriorityUpdateFailure,
			Notifications: notifications,
			Err:           err,
		}, nil
	}

	b.publishNotificationsChanged()

	notifications, err := b.Store.ListNotifications(ctx)
	if err != nil {
		if before != nil {
			notifications = withPriority(before, id, priority)
		} else {
			return types.PriorityUpdateResult{}, err
		}
	}

	return types.PriorityUpdateResult{
		Status:        types.PriorityUpdateSuccess,
		Notifications: notifications,
		Toast:         priorityToast(priority),
	}, nil
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

// Shutdown is intentionally non-owning for the in-process Backend.
//
// Shared-service teardown in standalone mode belongs to CoreEngine, which owns
// the syncer, enricher, traffic controller, and alerter it constructs.
// Backend keeps this method only to satisfy the transport-agnostic TUIBackend
// seam while connected-mode adapters may still need local cleanup hooks.
func (b *Backend) Shutdown(ctx context.Context) {
	_ = ctx
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

func withReadState(notifications []triage.NotificationWithState, id string, read bool) []triage.NotificationWithState {
	cloned := append([]triage.NotificationWithState(nil), notifications...)
	for idx := range cloned {
		if cloned[idx].GitHubID == id {
			cloned[idx].IsReadLocally = read
			break
		}
	}
	return cloned
}

func withPriority(notifications []triage.NotificationWithState, id string, priority int) []triage.NotificationWithState {
	cloned := append([]triage.NotificationWithState(nil), notifications...)
	for idx := range cloned {
		if cloned[idx].GitHubID == id {
			cloned[idx].Priority = priority
			break
		}
	}
	return cloned
}

func priorityToast(priority int) string {
	switch priority {
	case 1:
		return "Priority set to Low"
	case 2:
		return "Priority set to Medium"
	case 3:
		return "Priority set to High"
	default:
		return "Priority cleared"
	}
}
