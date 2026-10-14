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

type defectMaskObjects struct{ Root, Params, Pixel defectEvidenceObject }

// defectFaceMaskObjects discovers the unique original root and Params through
// the pixel producer. It does not accept caller-selected document references.
// SQL repeats the declared exact-source parse lineage for skin producers.
func (s SQLTransferSourceReader) defectFaceMaskObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, pixel v2input.Ref, selector string) (defectMaskObjects, error) {
	empty := defectMaskObjects{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if (selector != "manual" && selector != "skin") || pixel.ArtifactID == uuid.Nil || s.Bucket == "" {
		return empty, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(pixel.SHA256); err != nil {
		return empty, v2input.ErrInvalid
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return empty, err
	}
	var result defectMaskObjects
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_proposal_defect_face_mask_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, pixel.ArtifactID, pixel.SHA256, selector)
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

func validDefectMaskObjects(objects defectMaskObjects, buckets [3]string, bucket string) bool {
	if bucket == "" {
		return false
	}
	seen := map[uuid.UUID]bool{}
	for i, entry := range []defectEvidenceObject{objects.Root, objects.Params, objects.Pixel} {
		if entry.ref.ArtifactID == uuid.Nil || seen[entry.ref.ArtifactID] || buckets[i] != bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" {
			return false
		}
		if _, err := domain.ParseDigest(entry.ref.SHA256); err != nil {
			return false
		}
		seen[entry.ref.ArtifactID] = true
		minimum, maximum := int64(2), int64(1048576)
		if i == 2 {
			minimum, maximum = 32, 800065548
		}
		if entry.object.Bytes < minimum || entry.object.Bytes > maximum {
			return false
		}
	}
	return true
}
