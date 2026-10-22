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

func (s SQLTransferSourceReader) verifyFaceParsePreparationProfiles(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parse, detection, pixels v2input.Ref, expected [2]v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	for i, value := range expected {
		if value.Operation != [2]string{"face.parse", "face.detect"}[i] || value.Version != "1" || (value.Backend != "cpu" && value.Backend != "cuda" && value.Backend != "mps") {
			return v2qualification.ErrUnqualified
		}
		for _, digest := range []string{value.ModelLockSHA, value.RuntimeLockSHA, value.DeviceProfileSHA} {
			if _, err := domain.ParseDigest(digest); err != nil {
				return v2qualification.ErrUnqualified
			}
		}
		expected[i] = ownQualificationExpected(value)
	}
	if _, err := s.faceParsePreparationPixels(ctx, who, tenantID, binding, parse, detection, pixels); err != nil {
		return err
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_face_parse_preparation_profiles_match($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, parse.ArtifactID, parse.SHA256, detection.ArtifactID, detection.SHA256, pixels.ArtifactID, pixels.SHA256, body).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
}
