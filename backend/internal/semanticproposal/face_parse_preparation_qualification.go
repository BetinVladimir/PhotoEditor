package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// ReadQualifiedFaceParsePreparation verifies independently selected trusted
// reports, source pixels/color, original parser/detector profiles and content.
// This is selected preflight; it does not queue work or replace admission locks.
func (s SQLTransferSourceReader) ReadQualifiedFaceParsePreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parse, detection, pixels v2input.Ref, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) (review.FaceLabels19, [19]int64, error) {
	labels, counts, _, err := s.readQualifiedFaceParsePreparationProof(ctx, who, tenantID, binding, parse, detection, pixels, reports, expected, color)
	return labels, counts, err
}

func (s SQLTransferSourceReader) readQualifiedFaceParsePreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parse, detection, pixels v2input.Ref, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) (review.FaceLabels19, [19]int64, faceParsePreparationProof, error) {
	fail := func(err error) (review.FaceLabels19, [19]int64, faceParsePreparationProof, error) {
		return review.FaceLabels19{}, [19]int64{}, faceParsePreparationProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
	}
	if err := s.verifyFaceParsePreparationProfiles(ctx, who, tenantID, binding, parse, detection, pixels, expected); err != nil {
		return fail(err)
	}
	var reportPins [2]personEvidenceObject
	var original [2][2]defectEvidenceObject
	refs := [2]v2input.Ref{parse, detection}
	for i := range expected {
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return fail(err)
		}
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return fail(err)
		}
		if _, err := v2qualification.VerifyTyped(raw, pin.ref.SHA256, expected[i]); err != nil {
			return fail(err)
		}
		reportPins[i] = pin
		objects, err := s.facePreparationObjects(ctx, who, tenantID, binding, refs[i], expected[i].Operation)
		if err != nil {
			return fail(err)
		}
		original[i] = objects
	}
	originalPixels, err := s.faceParsePreparationPixels(ctx, who, tenantID, binding, parse, detection, pixels)
	if err != nil {
		return fail(err)
	}
	if _, err := s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
		return fail(err)
	}
	labels, counts, err := s.ReadFaceParsePreparation(ctx, who, tenantID, binding, parse)
	if err != nil {
		return fail(err)
	}
	if labels.Labels.ID != pixels.ArtifactID || labels.Labels.SHA256 != pixels.SHA256 || labels.Faces.ID != detection.ArtifactID || labels.Faces.SHA256 != detection.SHA256 {
		return fail(v2input.ErrConflict)
	}
	for i := range expected {
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, original[i][0].object, original[i][0].ref)
		if err != nil {
			return fail(err)
		}
		var claim struct {
			Model   string `json:"model_lock_id"`
			Backend string `json:"backend"`
		}
		if json.Unmarshal(raw, &claim) != nil || claim.Model != expected[i].ModelLockSHA || claim.Backend != expected[i].Backend {
			return fail(v2qualification.ErrUnqualified)
		}
	}
	if _, err := s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
		return fail(err)
	}
	for i := range expected {
		objects, err := s.facePreparationObjects(ctx, who, tenantID, binding, refs[i], expected[i].Operation)
		if err != nil {
			return fail(err)
		}
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return fail(err)
		}
		if objects != original[i] || pin != reportPins[i] {
			return fail(v2input.ErrConflict)
		}
	}
	currentPixels, err := s.faceParsePreparationPixels(ctx, who, tenantID, binding, parse, detection, pixels)
	if err != nil {
		return fail(err)
	}
	if currentPixels != originalPixels {
		return fail(v2input.ErrConflict)
	}
	if err := s.verifyFaceParsePreparationProfiles(ctx, who, tenantID, binding, parse, detection, pixels, expected); err != nil {
		return fail(err)
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return fail(err)
	}
	proof := faceParsePreparationProof{evidence: facePreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, objects: original, qualification: reportPins, expected: string(body)}, pixels: originalPixels}
	if !validFaceParsePreparationProofScope(s, who, tenantID, binding, proof) {
		return fail(v2input.ErrInvalid)
	}
	return labels, counts, proof, nil
}
