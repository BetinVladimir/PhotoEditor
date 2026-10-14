package v2portrait

import (
	"context"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sync"
)

// RelightManualRasterBudget is shared by all manual reads in one server request.
// Failed or revoked reads retain their charge; constructing a new budget for a
// retry would bypass the request bound and is not permitted by the coordinator.
type RelightManualRasterBudget struct {
	mu        sync.Mutex
	remaining RelightManualRasterLimits
}

func NewRelightManualRasterBudget(limits RelightManualRasterLimits) (*RelightManualRasterBudget, error) {
	if limits.PointChecks < 0 || limits.EdgeEvaluations < 0 || limits.PayloadBytes < 1 {
		return nil, v2input.ErrInvalid
	}
	return &RelightManualRasterBudget{remaining: limits}, nil
}

func (b *RelightManualRasterBudget) reserve(ctx context.Context, plan review.ManualRasterPlan, limits RelightManualRasterLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	work, err := plan.CheckStreamBudget(limits.PointChecks, limits.EdgeEvaluations, limits.PayloadBytes)
	if err != nil {
		return err
	}
	return b.consume(ctx, work)
}

func (b *RelightManualRasterBudget) consume(ctx context.Context, work review.ManualRasterWork) error {
	if b == nil {
		return v2input.ErrInvalid
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	r := b.remaining
	if work.Pixels < 1 || work.ShapeBoundPointChecks < 0 || work.SampleEdgeEvaluations < 0 || work.NPYPayloadBytes < 1 || r.PointChecks < work.ShapeBoundPointChecks || r.EdgeEvaluations < work.SampleEdgeEvaluations || r.PayloadBytes < work.NPYPayloadBytes {
		return v2input.ErrInvalid
	}
	b.remaining = RelightManualRasterLimits{PointChecks: r.PointChecks - work.ShapeBoundPointChecks, EdgeEvaluations: r.EdgeEvaluations - work.SampleEdgeEvaluations, PayloadBytes: r.PayloadBytes - work.NPYPayloadBytes}
	return nil
}
