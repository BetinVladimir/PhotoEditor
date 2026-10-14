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

// VerifyDefectFaceProducerProfiles checks stored task/attempt locks against an
// independently selected deployment policy (landmarks first, detector second).
// It does not verify qualification report bytes or live worker measurements.
func (s SQLTransferSourceReader) VerifyDefectFaceProducerProfiles(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, landmark, detection v2input.Ref, expected [2]v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	operations := [2]string{"face.landmarks", "face.detect"}
	for i, value := range expected {
		if value.Operation != operations[i] || value.Version != "1" || (value.Backend != "cpu" && value.Backend != "cuda" && value.Backend != "mps") {
			return v2qualification.ErrUnqualified
		}
		for _, digest := range []string{value.ModelLockSHA, value.RuntimeLockSHA, value.DeviceProfileSHA} {
			if _, err := domain.ParseDigest(digest); err != nil {
				return v2qualification.ErrUnqualified
			}
		}
		expected[i] = ownQualificationExpected(value)
	}
	if _, err := s.defectDetectionObject(ctx, who, tenantID, binding, pins, landmark, detection); err != nil {
		return err
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	return tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.semantic_proposal_defect_face_profiles_match($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, landmark.ArtifactID, landmark.SHA256, detection.ArtifactID, detection.SHA256, body).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
}
