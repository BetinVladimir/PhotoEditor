package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// ExportPersonOriginCurrentChecker repeats current producer authority after a
// signed receipt has been verified. Signature alone cannot keep a revoked or
// replaced producer current through export adoption.
type ExportPersonOriginCurrentChecker interface {
	RecheckExportPersonOrigin(context.Context, PortablePersonOriginClaim) error
}

type SQLExportPersonOriginCurrentChecker struct{ Locator PortablePersonOriginLocator }

func (s SQLExportPersonOriginCurrentChecker) RecheckExportPersonOrigin(ctx context.Context,
	claim PortablePersonOriginClaim) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Locator == nil || !validPersonOriginClaim(claim) {
		return v2input.ErrInvalid
	}
	current, err := s.Locator.LookupPortablePersonOrigin(ctx, claim.Source, claim.MaskRef, claim.MaskPixels)
	if err != nil {
		return err
	}
	if current.Task != claim.ProducerTaskID || current.Attempt != claim.ProducerAttempt ||
		current.Fence != claim.ProducerFence || current.Operation != claim.Operation ||
		current.Version != claim.OperationVersion || current.Root.Ref != claim.MaskRef ||
		current.Report.Ref != claim.QualificationReport || current.Mask.SHA256 != claim.MaskPixels.SHA256 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
