package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func (s SQLTransferSourceReader) verifyFacePreparationProfiles(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, landmark, detection v2input.Ref, expected [2]v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for i, value := range expected {
		if value.Operation != [2]string{"face.landmarks", "face.detect"}[i] || value.Version != "1" || (value.Backend != "cpu" && value.Backend != "cuda" && value.Backend != "mps") {
			return v2qualification.ErrUnqualified
		}
		for _, digest := range []string{value.ModelLockSHA, value.RuntimeLockSHA, value.DeviceProfileSHA} {
			if _, err := domain.ParseDigest(digest); err != nil {
				return v2qualification.ErrUnqualified
			}
		}
		expected[i] = ownQualificationExpected(value)
	}
	if _, err := s.facePreparationObjects(ctx, who, tenantID, binding, landmark, "face.landmarks"); err != nil {
		return err
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_face_preparation_profiles_match($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, landmark.ArtifactID, landmark.SHA256, detection.ArtifactID, detection.SHA256, body).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
}

// ReadQualifiedFacePreparation applies independently selected operation reports,
// exact producer profiles and original source color/pixels to one face collection.
// It is preflight; complete discovery and final evidence locks are separate.
func (s SQLTransferSourceReader) ReadQualifiedFacePreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, landmark, detection v2input.Ref, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) ([]review.Face68, error) {
	faces, _, err := s.readQualifiedFacePreparationProof(ctx, who, tenantID, binding, landmark, detection, reports, expected, color)
	return faces, err
}

func (s SQLTransferSourceReader) readQualifiedFacePreparationProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, landmark, detection v2input.Ref, reports [2]v2input.Ref, expected [2]v2qualification.Expected, color TransferColorProfile) ([]review.Face68, facePreparationProof, error) {
	if err := ctx.Err(); err != nil {
		return nil, facePreparationProof{}, err
	}
	for i := range expected {
		expected[i] = ownQualificationExpected(expected[i])
	}
	if err := s.verifyFacePreparationProfiles(ctx, who, tenantID, binding, landmark, detection, expected); err != nil {
		return nil, facePreparationProof{}, err
	}
	var qualificationPins [2]personEvidenceObject
	for i := range expected {
		pin, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		if _, err = v2qualification.VerifyTyped(raw, pin.ref.SHA256, expected[i]); err != nil {
			return nil, facePreparationProof{}, err
		}
		qualificationPins[i] = pin
	}
	if _, err := s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
		return nil, facePreparationProof{}, err
	}
	var original [2][2]defectEvidenceObject
	for i, ref := range []v2input.Ref{landmark, detection} {
		pins, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, expected[i].Operation)
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		original[i] = pins
	}
	faces, err := s.ReadFacePreparation(ctx, who, tenantID, binding, landmark)
	if err != nil {
		return nil, facePreparationProof{}, err
	}
	for i, ref := range []v2input.Ref{landmark, detection} {
		pins, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, expected[i].Operation)
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		if pins != original[i] {
			return nil, facePreparationProof{}, v2input.ErrConflict
		}
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pins[0].object, pins[0].ref)
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		var claim struct {
			Model   string `json:"model_lock_id"`
			Backend string `json:"backend"`
		}
		if json.Unmarshal(raw, &claim) != nil || claim.Model != expected[i].ModelLockSHA || claim.Backend != expected[i].Backend {
			return nil, facePreparationProof{}, v2qualification.ErrUnqualified
		}
		if i == 0 {
			var link struct {
				Faces v2input.Ref `json:"faces"`
			}
			if json.Unmarshal(raw, &link) != nil || link.Faces != detection {
				return nil, facePreparationProof{}, v2qualification.ErrUnqualified
			}
		}
	}
	if _, err := s.VerifyQualifiedFaceSourcePixels(ctx, who, tenantID, binding, color); err != nil {
		return nil, facePreparationProof{}, err
	}
	for i, ref := range []v2input.Ref{landmark, detection} {
		pins, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, expected[i].Operation)
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		if pins != original[i] {
			return nil, facePreparationProof{}, v2input.ErrConflict
		}
		current, err := s.faceQualificationObject(ctx, who, tenantID, expected[i], reports[i])
		if err != nil {
			return nil, facePreparationProof{}, err
		}
		if current != qualificationPins[i] {
			return nil, facePreparationProof{}, v2input.ErrConflict
		}
	}
	if err := s.verifyFacePreparationProfiles(ctx, who, tenantID, binding, landmark, detection, expected); err != nil {
		return nil, facePreparationProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, facePreparationProof{}, err
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return nil, facePreparationProof{}, err
	}
	proof := facePreparationProof{tenant: tenantID, project: s.ProjectID, actor: who.UserID, session: who.SessionID, bucket: s.Bucket, binding: binding, objects: original, qualification: qualificationPins, expected: string(expectedBody)}
	if !validFacePreparationProofScope(s, who, tenantID, binding, proof) {
		return nil, facePreparationProof{}, v2input.ErrInvalid
	}
	return faces, proof, nil
}
