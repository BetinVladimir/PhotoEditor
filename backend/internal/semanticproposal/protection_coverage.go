package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// VerifyProtectionCoverage checks server-selected required coverage against a
// protection root for the same exact source version/canvas/transform. Both roots
// require canonical producer authority, complete pinned mask bytes and repeated
// live authority after IO. This does not select person/defect evidence, prove
// analysis qualification or replace the final transactional policy lock.
func (s SQLTransferSourceReader) VerifyProtectionCoverage(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, requiredRoot, protectionRoot v2input.Ref) (int64, error) {
	required, err := s.ReadProtection(ctx, who, tenantID, binding, requiredRoot)
	if err != nil {
		return 0, err
	}
	protection, err := s.ReadProtection(ctx, who, tenantID, binding, protectionRoot)
	if err != nil {
		return 0, err
	}
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return 0, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	requiredNode := v2draft.LayerRef{Ref: required.Pixels, Contract: "MaskPixels", ParentRef: required.Root}
	protectedNode := v2draft.LayerRef{Ref: protection.Pixels, Contract: "MaskPixels", ParentRef: protection.Root}
	requiredObject, err := catalog.Lookup(ctx, who, tenantID, requiredNode)
	if err != nil {
		return 0, err
	}
	protectedObject, err := catalog.Lookup(ctx, who, tenantID, protectedNode)
	if err != nil {
		return 0, err
	}
	left, err := s.Objects.Open(ctx, requiredObject)
	if err != nil {
		return 0, err
	}
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		left.Close()
		return 0, err
	}
	count, verifyErr := masknpy.VerifyMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: requiredObject.Bytes, SHA256: required.Pixels.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return 0, verifyErr
	}
	if leftErr != nil {
		return 0, leftErr
	}
	if rightErr != nil {
		return 0, rightErr
	}
	current, err := catalog.Lookup(ctx, who, tenantID, requiredNode)
	if err != nil {
		return 0, err
	}
	if current != requiredObject {
		return 0, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, tenantID, protectedNode)
	if err != nil {
		return 0, err
	}
	if current != protectedObject {
		return 0, v2input.ErrConflict
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenantID, binding, protection.Root, protection.Pixels); err != nil {
		return 0, err
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenantID, binding, required.Root, required.Pixels); err != nil {
		return 0, err
	}
	return count, nil
}
