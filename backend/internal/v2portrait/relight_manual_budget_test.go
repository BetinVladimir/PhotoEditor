package v2portrait

import (
	"context"
	"photoeditor/backend/internal/review"
	"sync"
	"sync/atomic"
	"testing"
)

func TestRelightManualAggregateBudget(t *testing.T) {
	ctx := context.Background()
	work := review.ManualRasterWork{Pixels: 25, ShapeBoundPointChecks: 15, SampleEdgeEvaluations: 100, NPYPayloadBytes: 200}
	limits := RelightManualRasterLimits{PointChecks: 30, EdgeEvaluations: 200, PayloadBytes: 400}
	budget, err := NewRelightManualRasterBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	// A rejected request must not consume any dimension partially.
	denied := work
	denied.SampleEdgeEvaluations = 201
	if budget.consume(ctx, denied) == nil || budget.remaining != limits {
		t.Fatal("partial charge")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if budget.consume(canceled, work) != context.Canceled || budget.remaining != limits {
		t.Fatal("cancelled charge")
	}
	if err = budget.consume(ctx, work); err != nil {
		t.Fatal(err)
	}
	if err = budget.consume(ctx, work); err != nil {
		t.Fatal(err)
	}
	if budget.consume(ctx, work) == nil {
		t.Fatal("unlimited retries")
	}
	if budget.remaining != (RelightManualRasterLimits{}) {
		t.Fatal(budget.remaining)
	}
	var absent *RelightManualRasterBudget
	if absent.consume(ctx, work) == nil {
		t.Fatal("missing budget accepted")
	}
	for _, invalid := range []RelightManualRasterLimits{{}, {PointChecks: -1, PayloadBytes: 1}, {EdgeEvaluations: -1, PayloadBytes: 1}} {
		if b, err := NewRelightManualRasterBudget(invalid); err == nil || b != nil {
			t.Fatal("invalid limits")
		}
	}
	budget, _ = NewRelightManualRasterBudget(limits)
	var accepted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if budget.consume(ctx, work) == nil {
				accepted.Add(1)
			}
		}()
	}
	wg.Wait()
	if accepted.Load() != 2 || budget.remaining != (RelightManualRasterLimits{}) {
		t.Fatal("concurrent overcharge", accepted.Load(), budget.remaining)
	}
}
