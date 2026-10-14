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

// manualPreparationObjects resolves original canonical manual manifest/Params/pixel
// before a defect producer exists. It provides catalog pins, not pixel authority.
func (s SQLTransferSourceReader) manualPreparationObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pixel v2input.Ref) (defectMaskObjects, error) {
	empty := defectMaskObjects{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.DB == nil || s.Objects == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || pixel.ArtifactID == uuid.Nil || s.Bucket == "" {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(pixel.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(binding.Source.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	var result defectMaskObjects
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_proposal_manual_preparation_objects($1,$2,$3,$4,$5,$6,$7,$8,$9)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pixel.ArtifactID, pixel.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return v2input.ErrInvalid
		}
		var buckets [3]string
		result.Pixel.ref = pixel
		err = rows.Scan(&result.Root.ref.ArtifactID, &result.Root.ref.SHA256, &buckets[0], &result.Root.object.Key, &result.Root.object.VersionID, &result.Root.object.Bytes, &result.Params.ref.ArtifactID, &result.Params.ref.SHA256, &buckets[1], &result.Params.object.Key, &result.Params.object.VersionID, &result.Params.object.Bytes, &buckets[2], &result.Pixel.object.Key, &result.Pixel.object.VersionID, &result.Pixel.object.Bytes)
		if err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrConflict
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !validDefectMaskObjects(result, buckets, s.Bucket) {
			return v2input.ErrInvalid
		}
		result.Root.object.SHA256 = result.Root.ref.SHA256
		result.Root.object.ContentType = "application/json"
		result.Params.object.SHA256 = result.Params.ref.SHA256
		result.Params.object.ContentType = "application/json"
		result.Pixel.object.SHA256 = pixel.SHA256
		result.Pixel.object.ContentType = "application/octet-stream"
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
