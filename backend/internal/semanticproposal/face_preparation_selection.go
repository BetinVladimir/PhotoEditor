package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type facePreparationCandidate struct{ landmark, detection v2input.Ref }

// Returns complete selected profile-matching catalog candidates up to the
// admission bound. It never chooses newest or silently truncates. Content and
// qualification reports plus final barrier discovery repeat remain obligations.
func (s SQLTransferSourceReader) facePreparationCandidates(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, expected [2]v2qualification.Expected) ([]facePreparationCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.DB == nil || s.Bucket == "" || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	for _, sha := range []string{binding.Source.SHA256, binding.CanvasHash} {
		if _, err := domain.ParseDigest(sha); err != nil {
			return nil, v2input.ErrInvalid
		}
	}
	for i, value := range expected {
		if value.Operation != [2]string{"face.landmarks", "face.detect"}[i] || value.Version != "1" || (value.Backend != "cpu" && value.Backend != "cuda") {
			return nil, v2qualification.ErrUnqualified
		}
		for _, sha := range []string{value.ModelLockSHA, value.RuntimeLockSHA, value.DeviceProfileSHA} {
			if _, err := domain.ParseDigest(sha); err != nil {
				return nil, v2qualification.ErrUnqualified
			}
		}
		expected[i] = ownQualificationExpected(value)
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	result := make([]facePreparationCandidate, 0)
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_proposal_face_preparation_candidates($1,$2,$3,$4,$5,$6,$7,$8)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if len(result) >= 64 {
				return v2input.ErrInvalid
			}
			var entry facePreparationCandidate
			if err := rows.Scan(&entry.landmark.ArtifactID, &entry.landmark.SHA256, &entry.detection.ArtifactID, &entry.detection.SHA256); err != nil {
				return err
			}
			if entry.landmark.ArtifactID == uuid.Nil || entry.detection.ArtifactID == uuid.Nil || entry.landmark.ArtifactID == entry.detection.ArtifactID {
				return v2input.ErrInvalid
			}
			for _, sha := range []string{entry.landmark.SHA256, entry.detection.SHA256} {
				if _, err := domain.ParseDigest(sha); err != nil {
					return v2input.ErrInvalid
				}
			}
			result = append(result, entry)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
