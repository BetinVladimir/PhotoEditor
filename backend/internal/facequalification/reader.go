package facequalification

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// Reader is source-independent trusted face-report authority. It cannot admit a
// model task or substitute master-only face observation authority.
type Reader struct {
	DB        *pgxpool.Pool
	Objects   v2input.SessionAnalysisObjectReader
	Bucket    string
	ProjectID uuid.UUID
}
type Pin struct {
	Contract string
	Ref      v2input.Ref
	Object   objectstore.Object
	Backend  string
}

func (s Reader) Object(ctx context.Context, who auth.Identity, tenantID uuid.UUID, expected v2qualification.Expected, report v2input.Ref) (Pin, error) {
	empty := Pin{}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || (expected.Operation != "face.landmarks" && expected.Operation != "face.detect" && expected.Operation != "face.parse") || expected.Version != "1" {
		return empty, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return empty, err
	}
	var result Pin
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_face_qualification_object($1,$2,$3,$4,$5)", tenantID, s.ProjectID, who.UserID, who.SessionID, body)
		if err != nil {
			return err
		}
		defer rows.Close()
		found := false
		for rows.Next() {
			var e Pin
			var bucket string
			if err := rows.Scan(&e.Ref.ArtifactID, &e.Ref.SHA256, &bucket, &e.Object.Key, &e.Object.VersionID, &e.Object.Bytes); err != nil {
				return err
			}
			if e.Ref != report {
				continue
			}
			if found || bucket != s.Bucket {
				return v2qualification.ErrUnqualified
			}
			e.Object.SHA256 = e.Ref.SHA256
			e.Object.ContentType = "application/json"
			e.Contract = "QualificationReport"
			e.Backend = expected.Backend
			result = e
			found = true
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
		return empty, err
	}
	return result, nil
}
