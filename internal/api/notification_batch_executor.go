package api

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/hirakiuc/gh-orbit/internal/types"
)

const notificationBatchRemoteLimit = 3

type notificationBatchPlan struct {
	Request types.NotificationBatchRequest
	Prepare func(context.Context) error
	Remote  func(context.Context, string) error
}

type notificationBatchExecution struct {
	Committed bool
	Outcomes  []types.NotificationBatchItemResult
	Err       error
}

// notificationBatchExecutor is deliberately consumer-owned and narrow. The
// controller implementation is the sole production implementation.
type notificationBatchExecutor interface {
	RunNotificationBatch(context.Context, notificationBatchPlan) (notificationBatchExecution, error)
}

func (c *APITrafficController) RunNotificationBatch(ctx context.Context, plan notificationBatchPlan) (notificationBatchExecution, error) {
	if plan.Prepare == nil {
		return notificationBatchExecution{}, errors.New("notification batch prepare function is required")
	}
	taskCtx, cleanup := c.composeTaskContext(ctx)
	task := &apiTask{
		id:       atomicAddUint64(&c.taskCounter),
		priority: PriorityUser,
		resp:     make(chan any, 1),
		ctx:      taskCtx,
		cleanup:  cleanup,
		batch:    &plan,
	}

	c.stateMu.RLock()
	select {
	case <-c.done:
		c.stateMu.RUnlock()
		cleanup()
		return notificationBatchExecution{}, errors.New("traffic controller shutdown")
	default:
	}
	select {
	case <-taskCtx.Done():
		c.stateMu.RUnlock()
		cleanup()
		return notificationBatchExecution{}, taskCtx.Err()
	case c.high <- task:
		c.stateMu.RUnlock()
	default:
		c.stateMu.RUnlock()
		cleanup()
		return notificationBatchExecution{}, ErrTrafficQueueFull
	}

	result := <-task.resp
	if result == nil {
		return notificationBatchExecution{}, taskCtx.Err()
	}
	execution, ok := result.(notificationBatchExecution)
	if !ok {
		return notificationBatchExecution{}, errors.New("invalid notification batch execution result")
	}
	return execution, nil
}

func atomicAddUint64(value *uint64) uint64 {
	return atomic.AddUint64(value, 1)
}

func (c *APITrafficController) executeNotificationBatch(ctx context.Context, plan notificationBatchPlan) notificationBatchExecution {
	if err := plan.Prepare(ctx); err != nil {
		return notificationBatchExecution{Err: err}
	}
	execution := notificationBatchExecution{Committed: true}
	if !plan.Request.Operation.RequiresRemoteRead() || plan.Remote == nil {
		for _, id := range plan.Request.IDs {
			execution.Outcomes = append(execution.Outcomes, types.NotificationBatchItemResult{ID: id, Status: types.NotificationRemoteNotRequired})
		}
		return execution
	}

	type childResult struct {
		item types.NotificationBatchItemResult
	}
	results := make(chan childResult, len(plan.Request.IDs))
	var workers sync.WaitGroup
	next := 0
	active := 0
	for next < len(plan.Request.IDs) || active > 0 {
		for next < len(plan.Request.IDs) && active < notificationBatchRemoteLimit && ctx.Err() == nil {
			if !c.acquireCapacity(ctx) {
				break
			}
			id := plan.Request.IDs[next]
			next++
			active++
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer c.releaseCapacity()
				item := types.NotificationBatchItemResult{ID: id, Status: types.NotificationRemoteSucceeded}
				if err := plan.Remote(ctx, id); err != nil {
					item.Err = err
					item.ErrorCode = "remote_failed"
					item.Status = types.NotificationRemoteFailed
					if ctx.Err() != nil {
						item.ErrorCode = "canceled"
						item.Status = types.NotificationRemoteCanceled
					}
				}
				results <- childResult{item: item}
			}()
		}
		if active > 0 {
			result := <-results
			execution.Outcomes = append(execution.Outcomes, result.item)
			active--
			continue
		}
		break
	}
	for ; next < len(plan.Request.IDs); next++ {
		execution.Outcomes = append(execution.Outcomes, types.NotificationBatchItemResult{ID: plan.Request.IDs[next], Status: types.NotificationRemoteNotAttempted})
	}
	workers.Wait()
	sort.Slice(execution.Outcomes, func(i, j int) bool { return execution.Outcomes[i].ID < execution.Outcomes[j].ID })
	return execution
}
