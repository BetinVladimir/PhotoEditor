package review

import "context"

// A verified preview body is held in memory until the second authority read.
// Four concurrent 52 MiB bodies cap admitted reads; allocator and decoder
// overhead still require separate process-memory headroom.
var reviewMediaSlots = make(chan struct{}, 4)

func acquireReviewMediaSlot(ctx context.Context, slots chan struct{}) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		<-slots
		return nil, err
	}
	return func() { <-slots }, nil
}
