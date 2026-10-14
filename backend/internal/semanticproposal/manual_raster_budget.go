package semanticproposal

import (
	"errors"
	"photoeditor/backend/internal/review"
	"sync"
)

type manualRasterBudget struct {
	mu        sync.Mutex
	remaining ManualRasterVerificationLimits
}

// Each top-level call owns a scope; nested/repeated readers share its meter.
func (s SQLTransferSourceReader) withManualRasterScope() SQLTransferSourceReader {
	if s.manualRasterBudget == nil {
		s.manualRasterBudget = &manualRasterBudget{remaining: s.ManualRasterBatchLimits}
	}
	return s
}
func (b *manualRasterBudget) consume(work review.ManualRasterWork) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.remaining
	if work.Pixels < 1 || work.ShapeBoundPointChecks < 0 || work.SampleEdgeEvaluations < 0 || work.NPYPayloadBytes < 1 || r.PointChecks < work.ShapeBoundPointChecks || r.EdgeEvaluations < work.SampleEdgeEvaluations || r.PayloadBytes < work.NPYPayloadBytes {
		return errors.New("manual raster aggregate budget exceeded")
	}
	b.remaining = ManualRasterVerificationLimits{PointChecks: r.PointChecks - work.ShapeBoundPointChecks, EdgeEvaluations: r.EdgeEvaluations - work.SampleEdgeEvaluations, PayloadBytes: r.PayloadBytes - work.NPYPayloadBytes}
	return nil
}
func (s SQLTransferSourceReader) reserveManualRasterBudget(plan review.ManualRasterPlan) error {
	work, err := plan.CheckStreamBudget(s.ManualRasterLimits.PointChecks, s.ManualRasterLimits.EdgeEvaluations, s.ManualRasterLimits.PayloadBytes)
	if err != nil {
		return err
	}
	if s.manualRasterBudget == nil {
		return errors.New("missing manual raster budget scope")
	}
	return s.manualRasterBudget.consume(work)
}
