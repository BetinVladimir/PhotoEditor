package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Private proof exists only after successful nonempty, zero-overlap verification.
// It retains both selection objects and the registered protection authority.
type nativeSelectionSeparationProof struct {
	registered               registeredNativeProtectionProof
	root, pixels             v2input.Ref
	rootObject, pixelsObject objectstore.Object
	count                    int64
}

// Requires exact-parent registered protection, nonempty selection and zero
// positive overlap. This gate does not select/qualify policy or admit transfer.
func (s SQLTransferSourceReader) VerifyNativeSelectionSeparation(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root v2input.Ref) (int64, error) {
	count, _, err := s.verifyNativeSelectionSeparation(ctx, who, request, key, intent, snapshot, task, root)
	return count, err
}

func (s SQLTransferSourceReader) verifyNativeSelectionSeparation(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root v2input.Ref) (int64, nativeSelectionSeparationProof, error) {
	protection, proof, err := s.readRegisteredNativeDefectProtection(ctx, who, request, key, intent, snapshot, task, root)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: request.Intent.Reference.AssetVersionID}
	rootNode := v2draft.LayerRef{Ref: request.Intent.ReferenceMask, Contract: "MaskRef"}
	rootObject, err := catalog.Lookup(ctx, who, request.TenantID, rootNode)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	selected, err := s.VerifySelectedMaskPixels(ctx, who, request)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	node := v2draft.LayerRef{Ref: selected, Contract: "MaskPixels", ParentRef: request.Intent.ReferenceMask}
	selectedObject, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	left, err := s.Objects.Open(ctx, selectedObject)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	protectedObject := proof.objects[1].object
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		_ = left.Close()
		return 0, nativeSelectionSeparationProof{}, err
	}
	count, verifyErr := masknpy.VerifyMaskSeparation(ctx, masknpy.PinnedMask{Reader: left, Bytes: selectedObject.Bytes, SHA256: selected.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return 0, nativeSelectionSeparationProof{}, verifyErr
	}
	if leftErr != nil {
		return 0, nativeSelectionSeparationProof{}, leftErr
	}
	if rightErr != nil {
		return 0, nativeSelectionSeparationProof{}, rightErr
	}
	current, err := catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	if current != selectedObject {
		return 0, nativeSelectionSeparationProof{}, v2input.ErrConflict
	}
	currentSelected, err := s.VerifySelectedMaskPixels(ctx, who, request)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	if currentSelected != selected {
		return 0, nativeSelectionSeparationProof{}, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, request.TenantID, node)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	if current != selectedObject {
		return 0, nativeSelectionSeparationProof{}, v2input.ErrConflict
	}
	currentPins, err := s.registeredNativeProtectionObjects(ctx, who, request, task, root, proof.parent)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	if currentPins != proof.objects {
		return 0, nativeSelectionSeparationProof{}, v2input.ErrConflict
	}
	currentRoot, err := catalog.Lookup(ctx, who, request.TenantID, rootNode)
	if err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	if currentRoot != rootObject {
		return 0, nativeSelectionSeparationProof{}, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return 0, nativeSelectionSeparationProof{}, err
	}
	return count, nativeSelectionSeparationProof{registered: proof, root: request.Intent.ReferenceMask, pixels: selected, rootObject: rootObject, pixelsObject: selectedObject, count: count}, nil
}
