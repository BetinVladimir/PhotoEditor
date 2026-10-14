package semanticproposal

import (
	"context"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// VerifySelectionSeparation requires a server-selected source protection root.
// Both masks are independently authorized/validated before paired streaming.
func (s SQLTransferSourceReader) VerifySelectionSeparation(ctx context.Context, who auth.Identity, request Request, protectionRoot v2input.Ref) (int64, error) {
	selected, err := s.VerifySelectedMaskPixels(ctx, who, request)
	if err != nil {
		return 0, err
	}
	protection, err := s.ReadProtection(ctx, who, request.TenantID, request.Intent.Reference, protectionRoot)
	if err != nil {
		return 0, err
	}
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return 0, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	selectedNode := v2draft.LayerRef{Ref: selected, Contract: "MaskPixels", ParentRef: request.Intent.ReferenceMask}
	protectedNode := v2draft.LayerRef{Ref: protection.Pixels, Contract: "MaskPixels", ParentRef: protection.Root}
	selectedObject, err := catalog.Lookup(ctx, who, request.TenantID, selectedNode)
	if err != nil {
		return 0, err
	}
	protectedObject, err := catalog.Lookup(ctx, who, request.TenantID, protectedNode)
	if err != nil {
		return 0, err
	}
	left, err := s.Objects.Open(ctx, selectedObject)
	if err != nil {
		return 0, err
	}
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		left.Close()
		return 0, err
	}
	count, verifyErr := masknpy.VerifyMaskSeparation(ctx, masknpy.PinnedMask{Reader: left, Bytes: selectedObject.Bytes, SHA256: selected.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
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
	current, err := catalog.Lookup(ctx, who, request.TenantID, selectedNode)
	if err != nil {
		return 0, err
	}
	if current != selectedObject {
		return 0, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, request.TenantID, protectedNode)
	if err != nil {
		return 0, err
	}
	if current != protectedObject {
		return 0, v2input.ErrConflict
	}
	if err := s.VerifyProtectionOrigin(ctx, who, request.TenantID, request.Intent.Reference, protection.Root, protection.Pixels); err != nil {
		return 0, err
	}
	if _, err := ReadInventory(ctx, s.DB, who, request); err != nil {
		return 0, err
	}
	return count, nil
}
