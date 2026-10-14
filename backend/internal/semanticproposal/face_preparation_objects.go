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

// facePreparationObjects provides selected catalog pins, never model/content
// authority. It has no dependency on completed defect producer outputs.
func (s SQLTransferSourceReader) facePreparationObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, ref v2input.Ref, operation string) ([2]defectEvidenceObject, error) {
	empty := [2]defectEvidenceObject{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil || ref.ArtifactID == uuid.Nil || (operation != "face.detect" && operation != "face.landmarks" && operation != "face.parse") {
		return empty, v2input.ErrInvalid
	}
	for _, digest := range []string{binding.Source.SHA256, binding.CanvasHash, ref.SHA256} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return empty, v2input.ErrInvalid
		}
	}
	var result [2]defectEvidenceObject
	result[0].ref = ref
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT * FROM photo.semantic_proposal_face_preparation_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, ref.ArtifactID, ref.SHA256, operation)
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
		var buckets [2]string
		if err := rows.Scan(&buckets[0], &result[0].object.Key, &result[0].object.VersionID, &result[0].object.Bytes, &result[1].ref.ArtifactID, &result[1].ref.SHA256, &buckets[1], &result[1].object.Key, &result[1].object.VersionID, &result[1].object.Bytes); err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrConflict
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !validFacePreparationObjects(result, buckets, s.Bucket, binding.Source) {
			return v2input.ErrInvalid
		}
		for i := range result {
			result[i].object.SHA256 = result[i].ref.SHA256
			result[i].object.ContentType = "application/json"
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

func validFacePreparationObjects(objects [2]defectEvidenceObject, buckets [2]string, bucket string, source v2input.Ref) bool {
	if bucket == "" || source.ArtifactID == uuid.Nil {
		return false
	}
	seen := map[uuid.UUID]bool{source.ArtifactID: true}
	for i, pin := range objects {
		if pin.ref.ArtifactID == uuid.Nil || seen[pin.ref.ArtifactID] || buckets[i] != bucket || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" || pin.object.Bytes < 2 || pin.object.Bytes > 1048576 {
			return false
		}
		if _, err := domain.ParseDigest(pin.ref.SHA256); err != nil {
			return false
		}
		seen[pin.ref.ArtifactID] = true
	}
	return true
}
