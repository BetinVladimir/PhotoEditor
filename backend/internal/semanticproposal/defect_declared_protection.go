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

func (s SQLTransferSourceReader) defectDeclaredProtectionObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, required v2input.Ref) (defectEvidenceObject, error) {
	empty := defectEvidenceObject{}
	if s.Objects == nil || s.Bucket == "" || required.ArtifactID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(required.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{pins.Params, pins.Proposal, pins.Report, pins.Labels, pins.Protection} {
		if ref.ArtifactID == required.ArtifactID {
			return empty, v2input.ErrInvalid
		}
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return empty, err
	}
	entry := defectEvidenceObject{ref: required}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var bucket string
		err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes,media_type FROM photo.semantic_proposal_defect_declared_protection_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, required.ArtifactID, required.SHA256).Scan(&bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType)
		if err != nil {
			return err
		}
		if bucket != s.Bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.ContentType != "application/octet-stream" || entry.object.Bytes < 32 || entry.object.Bytes > 800065548 {
			return v2input.ErrInvalid
		}
		entry.object.SHA256 = required.SHA256
		return nil
	})
	if err != nil {
		return empty, err
	}
	return entry, nil
}
