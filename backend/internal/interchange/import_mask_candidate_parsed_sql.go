package interchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// SQLImportMaskCandidateParsedSource resolves only the currently committed
// parse result of an active import workflow. Callers must re-resolve after IO.
type SQLImportMaskCandidateParsedSource struct {
	DB               *pgxpool.Pool
	Tenant, Workflow uuid.UUID
	Who              auth.Identity
	Bucket           string
}

func (s SQLImportMaskCandidateParsedSource) ResolveReviewParsed(ctx context.Context) (v2input.Ref, objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, objectstore.Object{}, err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Workflow == uuid.Nil || s.Who.UserID == uuid.Nil || s.Who.SessionID == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return v2input.Ref{}, objectstore.Object{}, v2input.ErrInvalid
	}
	var ref v2input.Ref
	var object objectstore.Object
	err := tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.v30_import_mask_parsed_object($1,$2,$3,$4)`, s.Tenant, s.Who.UserID, s.Who.SessionID, s.Workflow)
		if err != nil {
			return err
		}
		defer rows.Close()
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return err
			}
			return artifactjson.ErrStale
		}
		var bucket string
		if err := rows.Scan(&ref.ArtifactID, &ref.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType); err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrInvalid
		}
		if err := rows.Err(); err != nil {
			return err
		}
		object.SHA256 = ref.SHA256
		if bucket != s.Bucket || !validImportRef(ref) || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.Bytes < 2 || object.Bytes > 8<<20 || object.ContentType != "application/json" {
			return objectstore.ErrIntegrity
		}
		return nil
	})
	if err != nil {
		return v2input.Ref{}, objectstore.Object{}, err
	}
	return ref, object, ctx.Err()
}
