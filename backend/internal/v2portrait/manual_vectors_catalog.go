package v2portrait

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"time"
)

// ReadManualVectorsObject requires canonical producer lineage and exact original
// declared inputs. Caller must additionally prove the original Params vectors
// field and independently verify the object bytes and rasterization.
func ReadManualVectorsObject(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding,
	target, guard, targetPixel, guardPixel v2input.Ref, origin ManualPairOrigin,
	vectors v2input.Ref, bucket string) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || bucket == "" || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || base.AssetVersionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{base.Source, target, guard, targetPixel, guardPixel, origin.Root, origin.Params, vectors} {
		if ref.ArtifactID == uuid.Nil {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	if _, err := domain.ParseDigest(base.CanvasHash); err != nil {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(base)
	if err != nil {
		return zero, err
	}
	work, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = work
	tx, err := db.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer func() {
		rollback, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_ = tx.Rollback(rollback)
	}()
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	var object objectstore.Object
	var actualBucket string
	err = tx.QueryRow(ctx, `SELECT bytes,bucket,object_key,object_version,media_type,sha256 FROM photo.v28_manual_vectors_object($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		tenant, project, snapshot, who.UserID, who.SessionID, string(body), target.ArtifactID, target.SHA256, guard.ArtifactID, guard.SHA256,
		targetPixel.ArtifactID, targetPixel.SHA256, guardPixel.ArtifactID, guardPixel.SHA256, origin.Root.ArtifactID, origin.Root.SHA256,
		origin.Params.ArtifactID, origin.Params.SHA256, vectors.ArtifactID, vectors.SHA256).Scan(&object.Bytes, &actualBucket, &object.Key, &object.VersionID, &object.ContentType, &object.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrConflict
	}
	if err != nil {
		return zero, err
	}
	if actualBucket != bucket || object.Key == "" || object.Bytes < 2 || object.Bytes > 1048576 || object.VersionID == "" || object.VersionID == "null" || object.ContentType != "application/json" || object.SHA256 != vectors.SHA256 {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return object, nil
}
