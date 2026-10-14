package interchange

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type SQLImportMaskCandidatePixelSource struct {
	DB               *pgxpool.Pool
	Tenant, Workflow uuid.UUID
	Who              auth.Identity
	Bucket           string
	Parsed           []byte
}

func (s SQLImportMaskCandidatePixelSource) Lookup(ctx context.Context, original uuid.UUID, staged v2input.Ref) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	if s.DB == nil || s.Tenant == uuid.Nil || s.Workflow == uuid.Nil || s.Who.UserID == uuid.Nil || s.Who.SessionID == uuid.Nil || original == uuid.Nil || !validImportRef(staged) || len(s.Bucket) < 3 || len(s.Bucket) > 63 || len(s.Parsed) < 2 || len(s.Parsed) > 8<<20 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	owned := append([]byte(nil), s.Parsed...)
	normalized, err := canonical.Bytes(owned)
	if err != nil || !bytes.Equal(normalized, owned) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	var object objectstore.Object
	err = tenant.WithTenant(ctx, s.DB, s.Who, s.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.v30_import_mask_pixel_object($1,$2,$3,$4,$5,$6,$7,$8)`, s.Tenant, s.Who.UserID, s.Who.SessionID, s.Workflow, string(owned), original, staged.ArtifactID, staged.SHA256)
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
		var artifact uuid.UUID
		var bucket string
		if err := rows.Scan(&artifact, &object.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType); err != nil {
			return err
		}
		if rows.Next() {
			return v2input.ErrInvalid
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if artifact != staged.ArtifactID || bucket != s.Bucket || object.SHA256 != staged.SHA256 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.Bytes < 10 || object.Bytes > 800065548 || object.ContentType != "application/octet-stream" {
			return objectstore.ErrIntegrity
		}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return objectstore.Object{}, artifactjson.ErrStale
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	return object, ctx.Err()
}
