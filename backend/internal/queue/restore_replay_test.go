package queue

import (
	"context"
	"errors"
	"testing"
)

type restoreDispatchFunc func(context.Context, string, int) (int, error)

func (f restoreDispatchFunc) Dispatch(ctx context.Context, kind string, limit int) (int, error) {
	return f(ctx, kind, limit)
}

func TestRestoreTaskReplayBoundedDrainAndRefusals(t *testing.T) {
	for _, mode := range []string{"valid", "empty", "guard", "publish_error", "stalled", "invalid_receipt", "growth", "ack_loss", "budget", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			remaining := int64(205)
			if mode == "empty" {
				remaining = 0
			}
			initial := remaining
			calls := 0
			guards := 0
			if mode == "cancelled" {
				cancel()
			}
			guard := func(context.Context) error {
				guards++
				if mode == "guard" {
					return errors.New("not isolated")
				}
				return nil
			}
			pending := func(context.Context) (int64, error) {
				if mode == "growth" && calls > 0 {
					return initial + 1, nil
				}
				if mode == "ack_loss" && calls > 0 {
					return 0, nil
				}
				return remaining, nil
			}
			dispatch := restoreDispatchFunc(func(ctx context.Context, kind string, limit int) (int, error) {
				calls++
				if kind != "task" || limit < 1 || limit > 100 {
					t.Fatal("mail or unbounded replay")
				}
				if mode == "publish_error" {
					return 0, errors.New("confirmation unknown")
				}
				if mode == "stalled" {
					return 0, nil
				}
				if mode == "invalid_receipt" {
					return limit + 1, nil
				}
				remaining -= int64(limit)
				return limit, nil
			})
			maximum := int64(300)
			if mode == "budget" {
				maximum = 100
			}
			result, err := drainRestoredTasks(ctx, dispatch, pending, guard, "isolated", maximum)
			if mode == "valid" || mode == "empty" {
				if err != nil || result.Confirmed != initial || result.InitiallyPending != initial || result.Database != "isolated" || result.CompletedAt.IsZero() {
					t.Fatalf("invalid evidence: %#v %v", result, err)
				}
				if mode == "valid" && calls != 3 {
					t.Fatal("did not drain bounded batches")
				}
			} else if err == nil || !result.CompletedAt.IsZero() {
				t.Fatal("failure granted completion")
			}
			if (mode == "guard" || mode == "budget" || mode == "cancelled") && calls != 0 {
				t.Fatal("dispatch before authority")
			}
			if mode != "cancelled" && guards == 0 {
				t.Fatal("missing guard")
			}
		})
	}
}

// A stale or partially replayed recovery report cannot dispatch any additional
// events. Operator reconciliation must produce fresh evidence before retry.
func TestRestoreReplayExpectedRecoveryCountBeforeDispatch(t *testing.T) {
	for _, mode := range []string{"matching", "empty", "queue_larger", "queue_smaller", "negative", "over_budget"} {
		t.Run(mode, func(t *testing.T) {
			remaining := int64(3)
			expected := int64(3)
			switch mode {
			case "empty":
				remaining = 0
				expected = 0
			case "queue_larger":
				remaining = 4
			case "queue_smaller":
				remaining = 2
			case "negative":
				expected = -1
			case "over_budget":
				expected = 11
			}
			calls := 0
			dispatch := restoreDispatchFunc(func(_ context.Context, kind string, limit int) (int, error) {
				calls++
				if kind != "task" {
					t.Fatal("foreign event family")
				}
				remaining -= int64(limit)
				return limit, nil
			})
			evidence, err := drainRestoredTasks(context.Background(), dispatch, func(context.Context) (int64, error) { return remaining, nil }, func(context.Context) error { return nil }, "isolated", 10, &expected)
			if mode == "matching" || mode == "empty" {
				if err != nil || evidence.InitiallyPending != expected || evidence.Confirmed != expected {
					t.Fatalf("invalid evidence %#v %v", evidence, err)
				}
				if mode == "empty" && calls != 0 {
					t.Fatal("empty queue dispatched")
				}
			} else if err == nil || calls != 0 || !evidence.CompletedAt.IsZero() {
				t.Fatal("mismatch dispatched or completed")
			}
		})
	}
}
