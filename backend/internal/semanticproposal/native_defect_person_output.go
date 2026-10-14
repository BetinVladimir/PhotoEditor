package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Independent original person authority and complete probability-preserving
// output coverage, outside SQL. The caller separately proves output producer,
// canonical result, immutable catalog pins and Params/report consistency.
func (s SQLTransferSourceReader) verifyNativeDefectPersonOutput(ctx context.Context, who auth.Identity, prepared nativeDefectFacePreparation, declared []review.ProtectionMask, output defectEvidenceObject) error {
	if err := s.recheckNativeDefectPersonInput(ctx, who, prepared); err != nil {
		return err
	}
	if prepared.person == nil {
		if len(declared) != 0 {
			return v2input.ErrInvalid
		}
		return ctx.Err()
	}
	p := *prepared.person
	mask, err := projectNativePersonProtectionInput(ctx, p, []byte(prepared.landmarks.detector.sourceBody))
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
	left, err := s.Objects.Open(ctx, p.objects[1])
	if err != nil {
		return err
	}
	right, err := s.Objects.Open(ctx, obj)
	if err != nil {
		_ = left.Close()
		return err
	}
	_, verifyErr := masknpy.VerifyProbabilityMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: p.objects[1].Bytes, SHA256: p.pixels.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: obj.Bytes, SHA256: output.ref.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
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
	return s.recheckNativeDefectPersonInput(ctx, who, prepared)
}
