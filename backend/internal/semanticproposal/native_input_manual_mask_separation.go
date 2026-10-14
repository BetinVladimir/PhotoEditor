package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Private proof exists only after successful nonempty, zero-overlap verification.
// It retains both selection objects and the registered protection authority.
type nativeInputManualSelectionSeparationProof struct {
	registered               registeredNativeInputManualInitialProtection
	root, pixels             v2input.Ref
	rootObject, pixelsObject objectstore.Object
	count                    int64
}

func (s SQLTransferSourceReader) verifyNativeInputManualSelectionSeparation(ctx context.Context, who auth.Identity, request Request, continuation nativeInputManualSourceContinuation, color TransferColorProfile) (int64, nativeInputManualSelectionSeparationProof, error) {
	if _, err := composeNativeInputManualSourceContinuation(ctx, who, request, continuation.source, continuation.protection, color); err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	proof, err := s.readRegisteredNativeInputManualInitialProtection(ctx, who, continuation.protection.published, color)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if !sameRegisteredNativeInputManualProtection(ctx, proof, continuation.protection) {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrConflict
	}
	protection := proof.verified
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	rootNode := v2draft.LayerRef{Ref: request.Intent.ReferenceMask, Contract: "MaskRef"}
	rootObject, err := catalog.Lookup(ctx, who, request.TenantID, rootNode)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	selected, err := s.VerifySelectedMaskPixels(ctx, who, request)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	node := v2draft.LayerRef{Ref: selected, Contract: "MaskPixels", ParentRef: request.Intent.ReferenceMask}
	selectedObject, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	left, err := s.Objects.Open(ctx, selectedObject)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	protectedObject := proof.objects[1].object
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		_ = left.Close()
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	count, verifyErr := masknpy.VerifyMaskSeparation(ctx, masknpy.PinnedMask{Reader: left, Bytes: selectedObject.Bytes, SHA256: selected.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, verifyErr
	}
	if leftErr != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, leftErr
	}
	if rightErr != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, rightErr
	}
	current, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if current != selectedObject {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrConflict
	}
	currentSelected, err := s.VerifySelectedMaskPixels(ctx, who, request)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if currentSelected != selected {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if current != selectedObject {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrConflict
	}
	currentProof, err := s.readRegisteredNativeInputManualInitialProtection(ctx, who, proof.published, color)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if !sameRegisteredNativeInputManualProtection(ctx, currentProof, proof) {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrConflict
	}
	currentRoot, err := catalog.Lookup(ctx, who, request.TenantID, rootNode)
	if err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if currentRoot != rootObject {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrConflict
	}
	if count <= 0 {
		return 0, nativeInputManualSelectionSeparationProof{}, v2input.ErrInvalid
	}
	if err := s.recheckSourcePreparationProof(ctx, who, request.TenantID, request.Intent.Reference, color, false, continuation.source); err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return 0, nativeInputManualSelectionSeparationProof{}, err
	}
	return count, nativeInputManualSelectionSeparationProof{registered: proof, root: request.Intent.ReferenceMask, pixels: selected, rootObject: rootObject, pixelsObject: selectedObject, count: count}, nil
}
