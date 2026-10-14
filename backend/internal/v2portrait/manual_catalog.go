// Package v2portrait assembles V28 inputs; no public operation is installed yet.
package v2portrait

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type ManualPairOrigin struct {
	Root   v2input.Ref
	Params v2input.Ref
}

// ReadManualPairOrigin returns catalog locators only. Caller must independently
// read/verify original bytes, compile a complete closure and recheck admission.
// Scheduler credentials do not replace the supplied live editor identity.
func ReadManualPairOrigin(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenantID, projectID, snapshotID uuid.UUID, base v2input.SessionVersionBinding,
	target, guard, targetPixel, guardPixel v2input.Ref) (ManualPairOrigin, error) {
	result, _, _, err := readManualPairCatalog(ctx, db, who, tenantID, projectID, snapshotID, base, target, guard, targetPixel, guardPixel, nil)
	return result, err
}

// ManualPairObjects contains immutable storage locators for independently
// verifying original documents. It is not permission for arbitrary child reads.
type ManualPairObjects struct {
	Origin ManualPairOrigin
	Root   objectstore.Object
	Params objectstore.Object
}

func ReadManualPairObjects(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenantID, projectID, snapshotID uuid.UUID, base v2input.SessionVersionBinding,
	target, guard, targetPixel, guardPixel v2input.Ref, bucket string) (ManualPairObjects, error) {
	if bucket == "" {
		return ManualPairObjects{}, v2input.ErrInvalid
	}
	origin, root, params, err := readManualPairCatalog(ctx, db, who, tenantID, projectID, snapshotID, base, target, guard, targetPixel, guardPixel, &bucket)
	if err != nil {
		return ManualPairObjects{}, err
	}
	return ManualPairObjects{Origin: origin, Root: root, Params: params}, nil
}

func readManualPairCatalog(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenantID, projectID, snapshotID uuid.UUID, base v2input.SessionVersionBinding,
	target, guard, targetPixel, guardPixel v2input.Ref, bucket *string) (ManualPairOrigin, objectstore.Object, objectstore.Object, error) {
	var zero ManualPairOrigin
	if err := ctx.Err(); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	if db == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenantID == uuid.Nil || projectID == uuid.Nil || snapshotID == uuid.Nil || base.AssetVersionID == uuid.Nil {
		return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{base.Source, target, guard, targetPixel, guardPixel} {
		if ref.ArtifactID == uuid.Nil {
			return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
		}
	}
	if _, err := domain.ParseDigest(base.CanvasHash); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(base)
	if err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = work
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	defer func() {
		rollback, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_ = tx.Rollback(rollback)
	}()
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	if role != "photo_scheduler" {
		return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID.String()); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	var result ManualPairOrigin
	var rootObject, paramsObject objectstore.Object
	err = tx.QueryRow(ctx, `SELECT root_id,root_sha,params_id,params_sha FROM photo.v28_manual_pair_origin($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14)`,
		tenantID, projectID, snapshotID, who.UserID, who.SessionID, string(body), target.ArtifactID, target.SHA256, guard.ArtifactID, guard.SHA256, targetPixel.ArtifactID, targetPixel.SHA256, guardPixel.ArtifactID, guardPixel.SHA256).Scan(&result.Root.ArtifactID, &result.Root.SHA256, &result.Params.ArtifactID, &result.Params.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrConflict
	}
	if err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	for _, ref := range []v2input.Ref{result.Root, result.Params} {
		if ref.ArtifactID == uuid.Nil {
			return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrConflict
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrConflict
		}
	}
	if err = ctx.Err(); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	if bucket != nil {
		for _, item := range []struct {
			ref    v2input.Ref
			object *objectstore.Object
		}{{result.Root, &rootObject}, {result.Params, &paramsObject}} {
			var actualBucket string
			err = tx.QueryRow(ctx, `SELECT bytes,bucket,object_key,object_version,media_type,sha256 FROM photo.v28_manual_pair_object($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, tenantID, projectID, snapshotID, who.UserID, who.SessionID, string(body), target.ArtifactID, target.SHA256, guard.ArtifactID, guard.SHA256, targetPixel.ArtifactID, targetPixel.SHA256, guardPixel.ArtifactID, guardPixel.SHA256, item.ref.ArtifactID, item.ref.SHA256).Scan(&item.object.Bytes, &actualBucket, &item.object.Key, &item.object.VersionID, &item.object.ContentType, &item.object.SHA256)
			if err != nil {
				return zero, objectstore.Object{}, objectstore.Object{}, err
			}
			if actualBucket != *bucket || item.object.Key == "" {
				return zero, objectstore.Object{}, objectstore.Object{}, v2input.ErrInvalid
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	if err = ctx.Err(); err != nil {
		return zero, objectstore.Object{}, objectstore.Object{}, err
	}
	return result, rootObject, paramsObject, nil
}
