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

// Keep the physical publication phase alive. The goroutine is joined before
// the caller performs the terminal retained-root SQL write, whose success may
// consume the expansion lease.
func withDeliveryAssemblyLease(ctx context.Context, store delivery.MatrixObservationStore,
	lease delivery.ExpansionLease, interval time.Duration,
	work func(context.Context) (deliveryAssemblyIOResult, error),
) (deliveryAssemblyIOResult, error) {
	var zero deliveryAssemblyIOResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if store == nil || !lease.Valid() || interval <= 0 || interval > 10*time.Second || work == nil {
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
				if heartbeat.Err() != nil {
					return
				}
				if err != nil {
					cancel(err)
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
