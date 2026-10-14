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

// Selected original labels metadata only. Manifest/Params/detector byte checks,
// bounded uint16 NPY validation and qualification are separate obligations.
func (s SQLTransferSourceReader) faceParsePreparationPixels(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, parse, detection, pixels v2input.Ref) (defectEvidenceObject, error) {
	empty := defectEvidenceObject{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if pixels.ArtifactID == uuid.Nil || pixels.ArtifactID == binding.Source.ArtifactID || pixels.ArtifactID == parse.ArtifactID || pixels.ArtifactID == detection.ArtifactID {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(pixels.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	parser, err := s.facePreparationObjects(ctx, who, tenantID, binding, parse, "face.parse")
	if err != nil {
		return empty, err
	}
	detector, err := s.facePreparationObjects(ctx, who, tenantID, binding, detection, "face.detect")
	if err != nil {
		return empty, err
	}
	seen := map[uuid.UUID]bool{binding.Source.ArtifactID: true, pixels.ArtifactID: true}
	for _, entry := range []defectEvidenceObject{parser[0], parser[1], detector[0], detector[1]} {
		if seen[entry.ref.ArtifactID] {
			return empty, v2input.ErrInvalid
		}
		seen[entry.ref.ArtifactID] = true
	}
	result := defectEvidenceObject{ref: pixels}
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_proposal_face_parse_preparation_pixels($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, parse.ArtifactID, parse.SHA256, detection.ArtifactID, detection.SHA256, pixels.ArtifactID, pixels.SHA256)
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
		var bucket string
		if err := rows.Scan(&bucket, &result.object.Key, &result.object.VersionID, &result.object.Bytes); err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrConflict
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if bucket != s.Bucket || result.object.Key == "" || result.object.VersionID == "" || result.object.VersionID == "null" || result.object.Bytes < 32 || result.object.Bytes > 400065548 {
			return v2input.ErrInvalid
		}
		result.object.SHA256 = pixels.SHA256
		result.object.ContentType = "application/octet-stream"
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
