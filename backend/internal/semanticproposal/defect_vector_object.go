package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) defectManualVectorObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, pixel, vector v2input.Ref) (defectEvidenceObject, error) {
	empty := defectEvidenceObject{}
	if vector.ArtifactID == uuid.Nil || vector.ArtifactID == pixel.ArtifactID {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(vector.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	if _, err := s.defectFaceMaskObjects(ctx, who, tenantID, binding, pins, pixel, "manual"); err != nil {
		return empty, err
	}
	entry := defectEvidenceObject{ref: vector}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_manual_vector_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, pixel.ArtifactID, pixel.SHA256, "manual", vector.ArtifactID, vector.SHA256).Scan(&bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes)
		if err == pgx.ErrNoRows {
			return v2input.ErrInvalid
		}
		if err != nil {
			return err
		}
		if bucket != s.Bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.Bytes < 2 || entry.object.Bytes > 1048576 {
			return v2input.ErrInvalid
		}
		entry.object.SHA256 = vector.SHA256
		entry.object.ContentType = "application/json"
		return nil
	})
	if err != nil {
		return empty, err
	}
	return entry, nil
}
