package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Independent target person authority and complete probability-preserving
// output coverage, outside SQL. The caller separately proves output producer,
// canonical result, immutable catalog pins and Params/report consistency.
func (s SQLTransferSourceReader) verifyNativeTargetDefectPersonOutput(ctx context.Context, plan nativeTargetDefectDraftPlan, declared []review.ProtectionMask, output defectEvidenceObject) error {
	p := plan.prepared.preparation
	mask, err := projectNativeTargetPersonProtection(ctx, p.personPlan, p.person, []byte(p.landmarks.detector.completed.sourceBody))
	if err != nil {
		return err
	}
	if len(declared) != 1 || !reflect.DeepEqual(declared[0], mask) || s.Objects == nil {
		return v2input.ErrConflict
	}
	obj := output.object
	if obj.SHA256 != output.ref.SHA256 || obj.Bytes < 1 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || (obj.ContentType != "application/x-npy" && obj.ContentType != "application/octet-stream") {
		return v2input.ErrInvalid
	}
	left, err := s.Objects.Open(ctx, plan.prepared.personPixels.object)
	if err != nil {
		return err
	}
	right, err := s.Objects.Open(ctx, obj)
	if err != nil {
		_ = left.Close()
		return err
	}
	_, verifyErr := masknpy.VerifyProbabilityMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: plan.prepared.personPixels.object.Bytes, SHA256: plan.prepared.personPixels.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: obj.Bytes, SHA256: output.ref.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if leftErr != nil {
		return leftErr
	}
	if rightErr != nil {
		return rightErr
	}
	return ctx.Err()
}
