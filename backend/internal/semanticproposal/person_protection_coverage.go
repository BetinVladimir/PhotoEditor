package semanticproposal

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// VerifyPersonProtectionCoverage binds complete scoped person evidence to
// protection support. Model scientific qualification and policy inventory remain
// independent; positive tiny probabilities conservatively require protection.
func (s SQLTransferSourceReader) VerifyPersonProtectionCoverage(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, requiredRoot, protectionRoot v2input.Ref, modelLock, backend string) (int64, error) {
	count, _, err := s.verifyPersonProtectionCoveragePinned(ctx, who, tenantID, binding, requiredRoot, protectionRoot, modelLock, backend)
	return count, err
}

func (s SQLTransferSourceReader) verifyPersonProtectionCoveragePinned(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, requiredRoot, protectionRoot v2input.Ref, modelLock, backend string) (int64, []personEvidenceObject, error) {
	required, evidencePins, err := s.readPersonEvidencePinned(ctx, who, tenantID, binding, requiredRoot, modelLock, backend)
	if err != nil {
		return 0, nil, err
	}
	protection, err := s.ReadProtection(ctx, who, tenantID, binding, protectionRoot)
	if err != nil {
		return 0, nil, err
	}
	mask, err := review.ParseMaskRef(protection.Body)
	if err != nil {
		return 0, nil, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	requiredNode := v2draft.LayerRef{Ref: required.Pixels, Contract: "MaskPixels", ParentRef: required.Root}
	protectedNode := v2draft.LayerRef{Ref: protection.Pixels, Contract: "MaskPixels", ParentRef: protection.Root}
	requiredObject, err := catalog.Lookup(ctx, who, tenantID, requiredNode)
	if err != nil {
		return 0, nil, err
	}
	protectedObject, err := catalog.Lookup(ctx, who, tenantID, protectedNode)
	if err != nil {
		return 0, nil, err
	}
	left, err := s.Objects.Open(ctx, requiredObject)
	if err != nil {
		return 0, nil, err
	}
	right, err := s.Objects.Open(ctx, protectedObject)
	if err != nil {
		left.Close()
		return 0, nil, err
	}
	count, verifyErr := masknpy.VerifyMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: requiredObject.Bytes, SHA256: required.Pixels.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: protectedObject.Bytes, SHA256: protection.Pixels.SHA256}, int64(mask.Canvas.Width), int64(mask.Canvas.Height))
	leftErr, rightErr := left.Close(), right.Close()
	if err := classifyPersonCoverageError(verifyErr); err != nil {
		return 0, nil, err
	}
	if leftErr != nil {
		return 0, nil, leftErr
	}
	if rightErr != nil {
		return 0, nil, rightErr
	}
	current, err := catalog.Lookup(ctx, who, tenantID, requiredNode)
	if err != nil {
		return 0, nil, err
	}
	if current != requiredObject {
		return 0, nil, v2input.ErrConflict
	}
	current, err = catalog.Lookup(ctx, who, tenantID, protectedNode)
	if err != nil {
		return 0, nil, err
	}
	if current != protectedObject {
		return 0, nil, v2input.ErrConflict
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenantID, binding, protection.Root, protection.Pixels); err != nil {
		return 0, nil, err
	}
	if err := s.VerifyPersonMaskOrigin(ctx, who, tenantID, binding, required.Root, required.Pixels); err != nil {
		return 0, nil, err
	}
	currentEvidence, err := s.personEvidenceObjects(ctx, who, tenantID, binding, required)
	if err != nil {
		return 0, nil, err
	}
	for i, pin := range evidencePins {
		if pin != currentEvidence[i] {
			return 0, nil, v2input.ErrConflict
		}
	}
	return count, evidencePins, nil
}

// Valid but uncovered person support is an unavailable dependency. Corrupt masks,
// IO and cancellation retain their original classification and never qualify.
func classifyPersonCoverageError(err error) error {
	if errors.Is(err, masknpy.ErrCoverageMissing) {
		return fmt.Errorf("%w: %w", v2qualification.ErrUnqualified, err)
	}
	return err
}
