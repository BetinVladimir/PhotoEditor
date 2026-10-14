package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
)

// ManualRasterVerificationLimits are deployment-selected bounds for one pass.
// Zero limits refuse work; no unqualified unlimited fallback is supplied.
type ManualRasterVerificationLimits struct{ PointChecks, EdgeEvaluations, PayloadBytes int64 }

func (s SQLTransferSourceReader) checkManualRasterBudget(plan review.ManualRasterPlan) error {
	_, err := plan.CheckStreamBudget(s.ManualRasterLimits.PointChecks, s.ManualRasterLimits.EdgeEvaluations, s.ManualRasterLimits.PayloadBytes)
	return err
}

func (s SQLTransferSourceReader) verifyManualRaster(ctx context.Context, target, protected defectEvidenceObject, plan review.ManualRasterPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.checkManualRasterBudget(plan); err != nil {
		return err
	}
	left, err := s.Objects.Open(ctx, target.object)
	if err != nil {
		return err
	}
	right, err := s.Objects.Open(ctx, protected.object)
	if err != nil {
		_ = left.Close()
		return err
	}
	canvas := plan.Canvas()
	_, verifyErr := masknpy.VerifyRasterProbabilityNPY(ctx, masknpy.PinnedMask{Reader: left, Bytes: target.object.Bytes, SHA256: target.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protected.object.Bytes, SHA256: protected.ref.SHA256}, canvas.Width, canvas.Height, func(ctx context.Context, x, y, length int64) ([]float32, []float32, error) {
		return plan.RasterizeRegion(ctx, x, y, length, 1)
	})
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if leftErr != nil {
		return leftErr
	}
	return rightErr
}
