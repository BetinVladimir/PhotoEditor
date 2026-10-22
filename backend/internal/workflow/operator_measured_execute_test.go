package workflow

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPrerequisiteComputeOwnsDurationAndRejectsLateSuccess(t *testing.T) {
	for _, op := range []string{"scene.parse", "face.detect", "face.parse", "mask.prompt", "mask.interactive"} {
		t.Run(op, func(t *testing.T) {
			began := time.Now()
			got, err := executeMeasuredOperator(context.Background(), PreparedTask{}, func(context.Context, PreparedTask) (OperatorMeasuredTask, error) {
				// Measure actual bounded work; an empty callback may take zero clock ticks.
				time.Sleep(time.Millisecond)
				return OperatorMeasuredTask{Elapsed: 24 * time.Hour}, nil
			})
			if err != nil || got.Elapsed <= 0 || got.Elapsed > time.Since(began) || got.Elapsed == 24*time.Hour {
				t.Fatalf("not host measured: %v %v", got.Elapsed, err)
			}
		})
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, err := executeMeasuredOperator(ctx, PreparedTask{}, func(context.Context, PreparedTask) (OperatorMeasuredTask, error) { return OperatorMeasuredTask{}, nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("late nil accepted", err)
	}
	original := errors.New("execute failed")
	_, err = executeMeasuredOperator(ctx, PreparedTask{}, func(context.Context, PreparedTask) (OperatorMeasuredTask, error) {
		return OperatorMeasuredTask{}, original
	})
	if err != original {
		t.Fatal("original error replaced", err)
	}
}
