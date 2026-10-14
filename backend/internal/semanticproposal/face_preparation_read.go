package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// ReadFacePreparation verifies original source/landmark/detection/Params bytes
// before defects.propose exists. It is preflight, not model qualification or
// a final admission certificate. Every failure returns no partial face set.
func (s SQLTransferSourceReader) ReadFacePreparation(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, landmark v2input.Ref) ([]review.Face68, error) {
	lm, err := s.facePreparationObjects(ctx, who, tenantID, binding, landmark, "face.landmarks")
	if err != nil {
		return nil, err
	}
	sourceRaw, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return nil, err
	}
	source := image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})
	landmarkRaw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, lm[0].object, lm[0].ref)
	if err != nil {
		return nil, err
	}
	var projection struct {
		Faces v2input.Ref `json:"faces"`
	}
	if json.Unmarshal(landmarkRaw, &projection) != nil {
		return nil, v2input.ErrInvalid
	}
	detection := projection.Faces
	det, err := s.facePreparationObjects(ctx, who, tenantID, binding, detection, "face.detect")
	if err != nil {
		return nil, err
	}
	seen := map[uuid.UUID]bool{binding.Source.ArtifactID: true}
	for _, entry := range []defectEvidenceObject{lm[0], lm[1], det[0], det[1]} {
		if seen[entry.ref.ArtifactID] {
			return nil, v2input.ErrInvalid
		}
		seen[entry.ref.ArtifactID] = true
	}
	if err = s.verifyFacePreparationLink(ctx, who, tenantID, binding, landmark, detection); err != nil {
		return nil, err
	}
	detectionRaw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, det[0].object, det[0].ref)
	if err != nil {
		return nil, err
	}
	var params [2][]byte
	for i, entry := range []defectEvidenceObject{lm[1], det[1]} {
		params[i], err = v2input.ReadPinnedSemanticDocument(ctx, s.Objects, entry.object, entry.ref)
		if err != nil {
			return nil, err
		}
	}
	asReview := func(ref v2input.Ref) review.ArtifactRef {
		return review.ArtifactRef{ID: ref.ArtifactID, SHA256: ref.SHA256}
	}
	if err = review.VerifyLandmarkDetection(landmarkRaw, asReview(detection), detectionRaw, source); err != nil {
		return nil, err
	}
	if err = review.VerifyLandmarkParams(asReview(lm[1].ref), params[0], landmarkRaw, source); err != nil {
		return nil, err
	}
	if err = review.VerifyFaceDetectParams(asReview(det[1].ref), params[1], detectionRaw, source); err != nil {
		return nil, err
	}
	faces, err := review.ParseFaceLandmarkCollection(landmarkRaw, source)
	if err != nil {
		return nil, err
	}
	for i, ref := range []v2input.Ref{landmark, detection} {
		current, err := s.facePreparationObjects(ctx, who, tenantID, binding, ref, []string{"face.landmarks", "face.detect"}[i])
		if err != nil {
			return nil, err
		}
		expected := lm
		if i == 1 {
			expected = det
		}
		if current != expected {
			return nil, v2input.ErrConflict
		}
	}
	currentSource, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(currentSource, sourceRaw) {
		return nil, v2input.ErrConflict
	}
	if err = s.verifyFacePreparationLink(ctx, who, tenantID, binding, landmark, detection); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return faces, nil
}
func (s SQLTransferSourceReader) verifyFacePreparationLink(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, landmark, detection v2input.Ref) error {
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_face_preparation_link_matches($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, landmark.ArtifactID, landmark.SHA256, detection.ArtifactID, detection.SHA256).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrInvalid
		}
		return nil
	})
}
