package workflow

import (
	"context"
	"time"

	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type deliveryAssemblyIOResult struct {
	ref   v2input.Ref
	state string
}

type deliveryLeaseRenewer interface {
	Renew(context.Context, delivery.ExpansionLease) error
}

// Keep bounded physical work alive. The goroutine is joined before callers
// advance a cursor or perform a terminal SQL write that may consume the lease.
func withDeliveryPhysicalLease[T any](ctx context.Context, store deliveryLeaseRenewer,
	lease delivery.ExpansionLease, interval time.Duration,
	work func(context.Context) (T, error),
) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if store == nil || interval <= 0 || interval > 10*time.Second || work == nil {
		return zero, v2input.ErrInvalid
	}
	working, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	heartbeat, stop := context.WithCancel(working)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeat.Done():
				return
			case <-ticker.C:
				call, finish := context.WithTimeout(heartbeat, 5*time.Second)
				err := store.Renew(call, lease)
				finish()
				if err != nil {
					cancel(err)
					return
				}
				if heartbeat.Err() != nil {
					return
				}
			}
		}
	}()
	result, err := work(working)
	stop()
	<-done
	if cause := context.Cause(working); cause != nil {
		return zero, cause
	}
	if err != nil {
		return zero, err
	}
	return result, nil
}
