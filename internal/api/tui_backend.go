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

// AppBackend is the in-process application-facing backend owner for TUI
// operations. It composes narrower services but owns direct mutation semantics
// and their corresponding event publication.
type AppBackend struct {
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

// AppBackendParams groups the concrete dependencies and local wiring hooks for
// the in-process AppBackend owner without introducing a broader builder
// abstraction.
type AppBackendParams struct {
	UserID   string
	Store    types.NotificationStore
	Client   github.Client
	Syncer   types.Syncer
	Enricher types.Enricher

	ResolveUserID func(context.Context) (string, error)

	PublishNotificationsChanged func()
	PublishEnrichmentUpdated    func()
}

func NewAppBackend(params AppBackendParams) (*AppBackend, error) {
	if params.Store == nil {
		return nil, fmt.Errorf("notification store is required for AppBackend")
	}
	if params.Syncer == nil {
		return nil, fmt.Errorf("syncer is required for AppBackend")
	}
	if params.Enricher == nil {
		return nil, fmt.Errorf("enricher is required for AppBackend")
	}
	if params.UserID == "" && params.ResolveUserID == nil {
		return nil, fmt.Errorf("user ID or resolver is required for AppBackend")
	}
	if params.PublishNotificationsChanged == nil {
		params.PublishNotificationsChanged = func() {}
	}
	if params.PublishEnrichmentUpdated == nil {
		params.PublishEnrichmentUpdated = func() {}
	}

	return &AppBackend{
		UserID:                      params.UserID,
		Store:                       params.Store,
		Client:                      params.Client,
		Syncer:                      params.Syncer,
		Enricher:                    params.Enricher,
		resolveUserID:               params.ResolveUserID,
		publishNotificationsChanged: params.PublishNotificationsChanged,
		publishEnrichmentUpdated:    params.PublishEnrichmentUpdated,
	}, nil
}

// NewTUIBackendClient is a temporary constructor alias kept only for
// compatibility while the architecture-v2 migration lands. It must not own any
// behavior beyond constructing the real AppBackend type.
func NewTUIBackendClient(
	userID string,
	store types.NotificationStore,
	syncer types.Syncer,
	enricher types.Enricher,
	client github.Client,
) (*AppBackend, error) {
	return NewAppBackend(AppBackendParams{
		UserID:   userID,
		Store:    store,
		Client:   client,
		Syncer:   syncer,
		Enricher: enricher,
	})
}

func (b *AppBackend) ListNotifications(ctx context.Context) ([]triage.NotificationWithState, error) {
	return b.Store.ListNotifications(ctx)
}

func (b *AppBackend) Sync(ctx context.Context, force bool) (models.RateLimitInfo, error) {
	userID, err := b.boundUserID(ctx)
	if err != nil {
		return models.RateLimitInfo{}, err
	}
	return b.Syncer.Sync(ctx, userID, force)
}

func (b *AppBackend) SetRead(ctx context.Context, id string, read bool) (types.ReadUpdateResult, error) {
	return b.updateRead(ctx, id, read, b.Store.SetReadLocally, withReadState)
}

// MarkReadLegacy preserves the coupled read/handled behavior required by old MCP clients.
func (b *AppBackend) MarkReadLegacy(ctx context.Context, id string, read bool) (types.ReadUpdateResult, error) {
	return b.updateRead(ctx, id, read, b.Store.MarkReadLocally, withLegacyReadState)
}

// MarkRead is retained for source compatibility outside the TUI-facing interface.
func (b *AppBackend) MarkRead(ctx context.Context, id string, read bool) (types.ReadUpdateResult, error) {
	return b.MarkReadLegacy(ctx, id, read)
}

func (b *AppBackend) updateRead(
	ctx context.Context,
	id string,
	read bool,
	persist func(context.Context, string, bool) error,
	applyFallback func([]triage.NotificationWithState, string, bool) []triage.NotificationWithState,
) (types.ReadUpdateResult, error) {
	before, _ := b.Store.ListNotifications(ctx)

	if err := persist(ctx, id, read); err != nil {
		notifications, reloadErr := b.Store.ListNotifications(ctx)
		if reloadErr != nil {
			if before != nil {
				return types.ReadUpdateResult{
					Status:        types.MarkReadLocalFailure,
					Notifications: before,
					Toast:         "Failed to update read state",
					Err:           err,
				}, nil
			}
			return types.ReadUpdateResult{}, fmt.Errorf("reload notifications after local read failure: %w (original error: %v)", reloadErr, err)
		}
		return types.ReadUpdateResult{
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
				notifications = applyFallback(before, id, read)
			}
			return types.ReadUpdateResult{
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
			notifications = applyFallback(before, id, read)
		} else {
			return types.ReadUpdateResult{}, err
		}
	}

	return types.ReadUpdateResult{
		Status:        types.MarkReadSuccess,
		Notifications: notifications,
	}, nil
}

func (b *AppBackend) SetHandled(ctx context.Context, id string, handled bool) (types.HandledUpdateResult, error) {
	before, _ := b.Store.ListNotifications(ctx)

	if err := b.Store.SetHandledLocally(ctx, id, handled); err != nil {
		notifications, reloadErr := b.Store.ListNotifications(ctx)
		if reloadErr != nil {
			if before != nil {
				return types.HandledUpdateResult{
					Status:        types.HandledUpdateFailure,
					Notifications: before,
					Toast:         "Failed to update handled state",
					Err:           err,
				}, nil
			}
			return types.HandledUpdateResult{}, fmt.Errorf("reload notifications after local handled failure: %w (original error: %v)", reloadErr, err)
		}
		return types.HandledUpdateResult{
			Status:        types.HandledUpdateFailure,
			Notifications: notifications,
			Toast:         "Failed to update handled state",
			Err:           err,
		}, nil
	}

	b.publishNotificationsChanged()
	notifications, err := b.Store.ListNotifications(ctx)
	if err != nil {
		if before == nil {
			return types.HandledUpdateResult{}, err
		}
		notifications = withHandledState(before, id, handled)
	}
	return types.HandledUpdateResult{
		Status:        types.HandledUpdateSuccess,
		Notifications: notifications,
	}, nil
}

func (b *AppBackend) SetPriority(ctx context.Context, id string, priority int) (types.PriorityUpdateResult, error) {
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

func (b *AppBackend) StartReviewWorkspace(ctx context.Context, request types.ReviewWorkspaceStartRequest) error {
	_ = ctx
	_ = request
	return types.ErrReviewWorkspaceUnsupported
}

func (b *AppBackend) FetchDetail(ctx context.Context, u string, subjectType string, force bool) (models.EnrichmentResult, error) {
	return b.Enricher.FetchDetail(ctx, u, subjectType, force)
}

func (b *AppBackend) PersistFetchedDetail(ctx context.Context, id, sourceURL string, res models.EnrichmentResult) error {
	if err := b.Enricher.PersistFetchedDetail(ctx, id, sourceURL, res); err != nil {
		return err
	}

	b.publishEnrichmentUpdated()
	return nil
}

func (b *AppBackend) FetchHybridBatch(ctx context.Context, notifications []triage.NotificationWithState, force bool) map[string]models.EnrichmentResult {
	return b.Enricher.FetchHybridBatch(ctx, notifications, force)
}

func (b *AppBackend) BridgeStatus() types.BridgeStatus {
	return b.Syncer.BridgeStatus()
}

// Shutdown is intentionally non-owning for the in-process AppBackend.
//
// Shared-service teardown in standalone mode belongs to CoreEngine, which owns
// the syncer, enricher, traffic controller, and alerter it constructs.
// AppBackend keeps this method only to satisfy the transport-agnostic TUIBackend
// seam while connected-mode adapters may still need local cleanup hooks.
func (b *AppBackend) Shutdown(ctx context.Context) {
	_ = ctx
}

func (b *AppBackend) boundUserID(ctx context.Context) (string, error) {
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

func withHandledState(notifications []triage.NotificationWithState, id string, handled bool) []triage.NotificationWithState {
	cloned := append([]triage.NotificationWithState(nil), notifications...)
	for idx := range cloned {
		if cloned[idx].GitHubID == id {
			cloned[idx].IsHandledLocally = handled
			break
		}
	}
	return cloned
}

func withLegacyReadState(notifications []triage.NotificationWithState, id string, read bool) []triage.NotificationWithState {
	cloned := withReadState(notifications, id, read)
	return withHandledState(cloned, id, read)
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
