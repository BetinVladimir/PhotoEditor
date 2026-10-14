package v2portrait

import (
	"context"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// RelightManualRasterLimits bounds one verification pass. The coordinator must
// also charge repeated passes against its independently selected batch budget.
type RelightManualRasterLimits struct{ PointChecks, EdgeEvaluations, PayloadBytes int64 }

// VerifyRelightManualPixels compares both original NPY streams with exact vector
// replay. Objects must come from authorized producer discovery, and plan from
// ResolveRelightManualPlan. Success is pixel evidence, not admission authority.
func VerifyRelightManualPixels(ctx context.Context, objects v2input.SessionAnalysisObjectReader, plan review.ManualRasterPlan, target, guard objectstore.Object, limits RelightManualRasterLimits) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if objects == nil || target.Key == "" || guard.Key == "" || target.VersionID == "" || guard.VersionID == "" || target.VersionID == "null" || guard.VersionID == "null" || target.Bytes < 1 || guard.Bytes < 1 {
		return v2input.ErrInvalid
	}
	if _, err := plan.CheckStreamBudget(limits.PointChecks, limits.EdgeEvaluations, limits.PayloadBytes); err != nil {
		return err
	}
	canvas := plan.Canvas()
	for _, object := range []objectstore.Object{target, guard} {
		if _, err := domain.ParseDigest(object.SHA256); err != nil {
			return v2input.ErrInvalid
		}
		if object.Bytes > 4*canvas.Width*canvas.Height+65536 {
			return v2input.ErrInvalid
		}
	}
	left, err := objects.Open(ctx, target)
	if err != nil {
		return err
	}
	right, err := objects.Open(ctx, guard)
	if err != nil {
		_ = left.Close()
		return err
	}
	_, verifyErr := masknpy.VerifyRasterProbabilityNPY(ctx, masknpy.PinnedMask{Reader: left, Bytes: target.Bytes, SHA256: target.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: guard.Bytes, SHA256: guard.SHA256}, canvas.Width, canvas.Height, func(ctx context.Context, x, y, n int64) ([]float32, []float32, error) {
		return plan.RasterizeRegion(ctx, x, y, n, 1)
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
