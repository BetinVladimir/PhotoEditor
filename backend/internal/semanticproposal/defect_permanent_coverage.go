package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// VerifyDefectPermanentCoverage requires a server-selected, canonical protected
// mask to be the exact permanent input declared in the original producer Params.
// It proves positive pixel containment, not facial protection completeness,
// scientific qualification, policy selection or transactional admission.
func (s SQLTransferSourceReader) VerifyDefectPermanentCoverage(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, requiredRoot v2input.Ref) (int64, error) {
	evidence, err := s.ReadDefectEvidence(ctx, who, tenantID, binding, pins)
	if err != nil {
		return 0, err
	}
	required, err := s.ReadProtection(ctx, who, tenantID, binding, requiredRoot)
	if err != nil {
		return 0, err
	}
	if err := matchDefectPermanent(evidence.Params, required.Body); err != nil {
		return 0, err
	}
	requiredMask, err := review.ParseMaskRef(required.Body)
	if err != nil {
		return 0, err
	}
	catalog := v2draft.SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket, ProjectID: s.ProjectID, AssetVersionID: binding.AssetVersionID}
	node := v2draft.LayerRef{Ref: required.Pixels, Contract: "MaskPixels", ParentRef: required.Root}
	requiredObject, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return 0, err
	}
	defectObjects, err := s.defectEvidenceObjects(ctx, who, tenantID, binding, pins)
	if err != nil {
		return 0, err
	}
	left, err := s.Objects.Open(ctx, requiredObject)
	if err != nil {
		return 0, err
	}
	right, err := s.Objects.Open(ctx, defectObjects[4].object)
	if err != nil {
		left.Close()
		return 0, err
	}
	count, verifyErr := masknpy.VerifyMaskCoverage(ctx, masknpy.PinnedMask{Reader: left, Bytes: requiredObject.Bytes, SHA256: required.Pixels.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: defectObjects[4].object.Bytes, SHA256: pins.Protection.SHA256}, requiredMask.Canvas.Width, requiredMask.Canvas.Height)
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
	current, err := catalog.Lookup(ctx, who, tenantID, node)
	if err != nil {
		return 0, err
	}
	if current != requiredObject {
		return 0, v2input.ErrConflict
	}
	if err := s.VerifyProtectionOrigin(ctx, who, tenantID, binding, required.Root, required.Pixels); err != nil {
		return 0, err
	}
	currentDefect, err := s.defectEvidenceObjects(ctx, who, tenantID, binding, pins)
	if err != nil {
		return 0, err
	}
	for i, pin := range defectObjects {
		if pin != currentDefect[i] {
			return 0, v2input.ErrConflict
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func matchDefectPermanent(params, required []byte) error {
	var projection struct {
		Permanent json.RawMessage `json:"permanent"`
	}
	if json.Unmarshal(params, &projection) != nil || len(projection.Permanent) == 0 || bytes.Equal(projection.Permanent, []byte("null")) {
		return v2input.ErrInvalid
	}
	declared, err := review.ParseMaskRef(projection.Permanent)
	if err != nil {
		return v2input.ErrInvalid
	}
	actual, err := review.ParseMaskRef(required)
	if err != nil {
		return v2input.ErrInvalid
	}
	a, err := canonical.Marshal(declared)
	if err != nil {
		return v2input.ErrInvalid
	}
	b, err := canonical.Marshal(actual)
	if err != nil || !bytes.Equal(a, b) {
		return v2input.ErrInvalid
	}
	return nil
}
