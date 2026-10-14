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

// manualPreparationOutputObjects catalogs exact protected NPY/target PNG/protected
// PNG roles from the canonical original rasterizer before defect production.
func (s SQLTransferSourceReader) manualPreparationOutputObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pixel v2input.Ref, refs [3]v2input.Ref) (defectManualOutputs, error) {
	empty := defectManualOutputs{}
	if _, err := s.manualPreparationObjects(ctx, who, tenantID, binding, pixel); err != nil {
		return empty, err
	}
	seen := map[uuid.UUID]bool{pixel.ArtifactID: true}
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return empty, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return empty, err
		}
		seen[ref.ArtifactID] = true
	}
	var result defectManualOutputs
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		for i, role := range []string{"mask_protected", "mask_target_png", "mask_protected_png"} {
			entry := defectEvidenceObject{ref: refs[i]}
			var bucket string
			err := tx.QueryRow(ctx, "SELECT bucket,object_key,object_version,bytes,media_type FROM photo.semantic_proposal_manual_preparation_output_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pixel.ArtifactID, pixel.SHA256, refs[i].ArtifactID, refs[i].SHA256, role).Scan(&bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes, &entry.object.ContentType)
			if err != nil {
				return err
			}
			media, min, max := "image/png", int64(57), int64(613048576)
			if i == 0 {
				media, min, max = "application/octet-stream", 32, 800065548
			}
			if bucket != s.Bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" || entry.object.ContentType != media || entry.object.Bytes < min || entry.object.Bytes > max {
				return v2input.ErrInvalid
			}
			entry.object.SHA256 = refs[i].SHA256
			result[i] = entry
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
