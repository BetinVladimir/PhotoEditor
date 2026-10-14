package semanticproposal

import (
	"photoeditor/backend/internal/review"
	"sync"
	"testing"
)

func TestManualRasterAggregateBudgetAtomic(t *testing.T) {
	work := review.ManualRasterWork{Pixels: 1, ShapeBoundPointChecks: 3, SampleEdgeEvaluations: 4, NPYPayloadBytes: 8}
	meter := &manualRasterBudget{remaining: ManualRasterVerificationLimits{PointChecks: 6, EdgeEvaluations: 8, PayloadBytes: 16}}
	if err := meter.consume(work); err != nil {
		t.Fatal(err)
	}
	old := meter.remaining
	tooLarge := work
	tooLarge.SampleEdgeEvaluations = 5
	if err := meter.consume(tooLarge); err == nil || meter.remaining != old {
		t.Fatal("failed reservation changed budget")
	}
	if err := meter.consume(work); err != nil {
		t.Fatal(err)
	}
	if err := meter.consume(work); err == nil {
		t.Fatal("budget reset")
	}
	meter = &manualRasterBudget{remaining: ManualRasterVerificationLimits{PointChecks: 3, EdgeEvaluations: 4, PayloadBytes: 8}}
	var wg sync.WaitGroup
	success := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); success <- meter.consume(work) == nil }()
	}
	wg.Wait()
	close(success)
	n := 0
	for ok := range success {
		if ok {
			n++
		}
	}
	if n != 1 || meter.remaining != (ManualRasterVerificationLimits{}) {
		t.Fatal("concurrent reservations overspent", n, meter.remaining)
	}
	reader := SQLTransferSourceReader{ManualRasterBatchLimits: ManualRasterVerificationLimits{PayloadBytes: 8}}
	first := reader.withManualRasterScope()
	nested := first.withManualRasterScope()
	second := reader.withManualRasterScope()
	if first.manualRasterBudget != nested.manualRasterBudget || first.manualRasterBudget == second.manualRasterBudget {
		t.Fatal("invalid budget scope sharing")
	}
}
