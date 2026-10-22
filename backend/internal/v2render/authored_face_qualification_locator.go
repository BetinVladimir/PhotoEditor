package v2render

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Retain the exact original-version catalog locator around the independently
// verified report read. This pin is used by the existing locked SQL report gate,
// rather than reconstructing a caller-supplied report location during staging.
func (s SQLAuthoredFaceQualification) locator(ctx context.Context, who auth.Identity, tenantID, project uuid.UUID, expected v2qualification.Expected, report v2input.Ref) (objectstore.Object, error) {
	var out objectstore.Object
	if s.API == nil || s.Objects == nil || s.Bucket == "" {
		return out, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return out, err
	}
	err = tenant.WithTenant(ctx, s.API, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_face_qualification_object($1,$2,$3,$4,$5)", tenantID, project, who.UserID, who.SessionID, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var ref v2input.Ref
			var bucket string
			var obj objectstore.Object
			if err := rows.Scan(&ref.ArtifactID, &ref.SHA256, &bucket, &obj.Key, &obj.VersionID, &obj.Bytes); err != nil {
				return err
			}
			if ref != report {
				continue
			}
			if found || bucket != s.Bucket || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" || obj.Bytes < 2 || obj.Bytes > 8<<20 {
				return v2qualification.ErrUnqualified
			}
			found = true
			obj.SHA256 = ref.SHA256
			obj.ContentType = "application/json"
			out = obj
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if !found {
			return v2qualification.ErrUnqualified
		}
		return nil
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	return out, ctx.Err()
}
