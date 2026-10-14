package semanticreview

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

// VerifyApprovedExposureMaskSeparation checks canonical mask/protection
// authority before and after lockstep physical verification. No array or partial
// coverage proof is returned on refusal. Admission must repeat live scope.
func VerifyApprovedExposureMaskSeparation(ctx context.Context, db ApprovedProtectionQueryer, objects planObjectStore, bucket string,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision, targetID uuid.UUID,
	selected, protected PlanDocument) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if len(selected.Body) > 8388608 || len(protected.Body) > 8388608 || objects == nil {
		return 0, ErrInvalid
	}
	selected.Body = append([]byte(nil), selected.Body...)
	protected.Body = append([]byte(nil), protected.Body...)
	mask, err := review.ParseMaskRef(selected.Body)
	if err != nil {
		return 0, ErrInvalid
	}
	protection, err := review.ParseMaskRef(protected.Body)
	if err != nil || mask.Protected || !protection.Protected || mask.Canvas != protection.Canvas {
		return 0, ErrInvalid
	}
	chainA, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return 0, ErrInvalid
	}
	chainB, err := canonical.Bytes(protection.TransformChain)
	if err != nil || !bytes.Equal(chainA, chainB) {
		return 0, ErrInvalid
	}
	load := func() (PlanSource, PlanSource, error) {
		a, err := verifyApprovedMaskBinary(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, targetID, selected, false)
		if err != nil {
			return PlanSource{}, PlanSource{}, err
		}
		b, err := verifyApprovedProtectionBinary(ctx, db, objects, bucket, tenant, intent, actor, session, prepared, targets, request, targetID, protected, false)
		if err != nil {
			return PlanSource{}, PlanSource{}, err
		}
		return a, b, nil
	}
	a, b, err := load()
	if err != nil {
		return 0, err
	}
	object := func(s PlanSource) objectstore.Object {
		return objectstore.Object{Key: s.Key, VersionID: s.Version, SHA256: s.Ref.SHA256, Bytes: s.Bytes, ContentType: s.MediaType}
	}
	first, err := objects.Open(ctx, object(a))
	if err != nil {
		return 0, err
	}
	if first == nil {
		return 0, ErrInvalid
	}
	second, err := objects.Open(ctx, object(b))
	if err != nil {
		first.Close()
		return 0, err
	}
	if second == nil {
		first.Close()
		return 0, ErrInvalid
	}
	count, verifyErr := masknpy.VerifyMaskSeparation(ctx, masknpy.PinnedMask{Reader: first, Bytes: a.Bytes, SHA256: a.Ref.SHA256}, masknpy.PinnedMask{Reader: second, Bytes: b.Bytes, SHA256: b.Ref.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	firstClose, secondClose := first.Close(), second.Close()
	if errors.Is(verifyErr, masknpy.ErrInvalid) {
		return 0, ErrInvalid
	}
	if verifyErr != nil {
		return 0, verifyErr
	}
	if firstClose != nil {
		return 0, firstClose
	}
	if secondClose != nil {
		return 0, secondClose
	}
	afterA, afterB, err := load()
	if err != nil {
		return 0, err
	}
	if a != afterA || b != afterB {
		return 0, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}
