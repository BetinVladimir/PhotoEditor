package review

import (
	"context"
	"errors"
	"testing"
)

func TestReviewMediaSlotBoundsAndCancellation(t *testing.T) {
	slots := make(chan struct{}, 1)
	release, err := acquireReviewMediaSlot(context.Background(), slots)
	if err != nil || len(slots) != 1 {
		t.Fatal("slot not acquired", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := acquireReviewMediaSlot(ctx, slots); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled waiter acquired slot", err)
	}
	if len(slots) != 1 {
		t.Fatal("cancelled waiter changed capacity")
	}
	release()
	if len(slots) != 0 {
		t.Fatal("slot leaked")
	}
	release, err = acquireReviewMediaSlot(context.Background(), slots)
	if err != nil {
		t.Fatal("released capacity not reusable", err)
	}
	release()
}
