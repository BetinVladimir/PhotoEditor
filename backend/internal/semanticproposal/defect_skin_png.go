package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

func (s SQLTransferSourceReader) defectSkinPNGObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, pixel v2input.Ref) (defectEvidenceObject, error) {
	empty := defectEvidenceObject{}
	if _, err := s.defectFaceMaskObjects(ctx, who, tenantID, binding, pins, pixel, "skin"); err != nil {
		return empty, err
	}
	var result defectEvidenceObject
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT png_id,png_sha,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_skin_png_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, pixel.ArtifactID, pixel.SHA256, "skin")
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			count++
			if count > 1 {
				return v2input.ErrConflict
			}
			var bucket string
			if err := rows.Scan(&result.ref.ArtifactID, &result.ref.SHA256, &bucket, &result.object.Key, &result.object.VersionID, &result.object.Bytes); err != nil {
				return err
			}
			if _, err := domain.ParseDigest(result.ref.SHA256); err != nil {
				return v2input.ErrInvalid
			}
			if result.ref.ArtifactID == uuid.Nil || bucket != s.Bucket || result.object.Key == "" || result.object.VersionID == "" || result.object.VersionID == "null" || result.object.Bytes < 57 || result.object.Bytes > 613048576 {
				return v2input.ErrInvalid
			}
			result.object.SHA256 = result.ref.SHA256
			result.object.ContentType = "image/png"
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 1 {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	return result, nil
}
func (s SQLTransferSourceReader) verifyProbabilityPNG(ctx context.Context, pixels, png defectEvidenceObject, width, height int64) error {
	left, err := s.Objects.Open(ctx, pixels.object)
	if err != nil {
		return err
	}
	right, err := s.Objects.Open(ctx, png.object)
	if err != nil {
		_ = left.Close()
		return err
	}
	verifyErr := masknpy.VerifyProbabilityMaskPNG(ctx, masknpy.PinnedMask{Reader: left, Bytes: pixels.object.Bytes, SHA256: pixels.ref.SHA256}, masknpy.PinnedMask{Reader: right, Bytes: png.object.Bytes, SHA256: png.ref.SHA256}, width, height)
	leftErr, rightErr := left.Close(), right.Close()
	if verifyErr != nil {
		return verifyErr
	}
	if leftErr != nil {
		return leftErr
	}
	return rightErr
}
