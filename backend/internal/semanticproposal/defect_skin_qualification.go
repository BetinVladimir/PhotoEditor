package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// ReadQualifiedDefectFaceMaskRoots composes complete original mask evidence with
// independently configured parse/detector qualification. Manual bindings retain
// their source/vector gates; qualification here covers skin producers only.
// It does not certify facial protection geometry or grant transaction admission.
func (s SQLTransferSourceReader) ReadQualifiedDefectFaceMaskRoots(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) ([]review.ProposalFaceMaskRoot, error) {
	roots, _, err := s.readQualifiedDefectFaceMaskEvidence(ctx, who, tenantID, binding, pins, reports, expected, color)
	return roots, err
}

func (s SQLTransferSourceReader) readQualifiedDefectFaceMaskEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) ([]review.ProposalFaceMaskRoot, []faceParsePreparationProof, error) {
	operations := [2]string{"face.parse", "face.detect"}
	var reportPins [2]personEvidenceObject
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
		if expected[i].Operation != operations[i] || expected[i].Version != "1" {
			return nil, nil, v2qualification.ErrUnqualified
		}
	}
	for i := range expected {
		if _, err := s.VerifyFaceQualificationTyped(ctx, who, tenantID, reports[i], expected[i]); err != nil {
			return nil, nil, err
		}
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, nil, err
		}
		reportPins[i] = pin
	}

	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return nil, nil, err
	}
	producers := make([]faceParsePreparationProof, 0)
	seen := map[v2input.Ref]faceParsePreparationProof{}
	sourceVerified := false
	result, err := s.readDefectFaceMaskRoots(ctx, who, tenantID, binding, pins, func(ctx context.Context, proof defectSkinReadProof) error {
		if proof.ParseModelLock != expected[0].ModelLockSHA || proof.ParseBackend != expected[0].Backend || proof.DetectionModelLock != expected[1].ModelLockSHA || proof.DetectionBackend != expected[1].Backend {
			return v2qualification.ErrUnqualified
		}
		if err := s.VerifyDefectSkinProducerProfiles(ctx, who, tenantID, binding, pins, proof.Pixel, proof.Parse, proof.Detection, expected); err != nil {
			return err
		}

		retained := faceParsePreparationProof{evidence: facePreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, expected: string(expectedBody), qualification: reportPins,
			objects: [2][2]defectEvidenceObject{{proof.ParseObjects.Manifest, proof.ParseObjects.Params}, {proof.DetectionObjects.Detection, proof.DetectionObjects.Params}}}, pixels: proof.ParseObjects.Pixels}
		if !validFaceParsePreparationProofScope(s, who, tenantID, binding, retained) {
			return v2input.ErrInvalid
		}
		if previous, exists := seen[proof.Parse]; exists {
			if previous != retained {
				return v2input.ErrConflict
			}
		} else {
			seen[proof.Parse] = retained
			producers = append(producers, retained)
		}
		if !sourceVerified {
			if _, err := s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
				return err
			}
			sourceVerified = true
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	// Repeat source authority after all dependency IO; manual-only inventories
	// keep their own source contract and do not acquire this skin-specific gate.
	if sourceVerified {
		if _, err := s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
			return nil, nil, err
		}
	}
	for _, proof := range producers {
		if err := s.recheckFaceParsePreparationProof(ctx, who, tenantID, binding, proof); err != nil {
			return nil, nil, err
		}
	}
	for i := range expected {
		current, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, nil, err
		}
		if current != reportPins[i] {
			return nil, nil, v2qualification.ErrUnqualified
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return result, producers, nil
}
