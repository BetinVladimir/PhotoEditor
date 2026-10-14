package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2render"
)

// Prepares the immutable master quality task after scoped producer and pixel verification.
type PerspectiveQualityCoordinator struct {
	DB                 *pgxpool.Pool
	Objects            PerspectivePreviewObjects
	Bucket, WorkingSHA string
}

func (c PerspectiveQualityCoordinator) Prepare(ctx context.Context, tenant, producer uuid.UUID) (PerspectiveQualityReceipt, error) {
	var zero PerspectiveQualityReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Objects == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || domainDigestInvalid(c.WorkingSHA) || tenant == uuid.Nil || producer == uuid.Nil {
		return zero, objectstore.ErrInvalid
	}
	sources := v2render.SQLPerspectivePreviewSource{DB: c.DB}
	bundle, err := v2render.LoadPerspectivePreviewBundle(ctx, sources, v2render.SQLPerspectivePreviewObjects{DB: c.DB, Bucket: c.Bucket}, c.Objects, tenant, producer)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(bundle.Documents()[bundle.Result().WorkingImage.ArtifactID.String()])
	if err != nil {
		return zero, err
	}
	if image.Color.ICCSHA256 != c.WorkingSHA {
		return zero, objectstore.ErrIntegrity
	}
	plan, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		return zero, err
	}
	store := PerspectiveQualityIntentStore{DB: c.DB}
	if receipt, found, err := store.ReadReceipt(ctx, plan); err != nil {
		return zero, err
	} else if found {
		return receipt, nil
	}
	verified, err := v2render.VerifyPerspectivePreviewInputs(ctx, sources, v2render.SQLPerspectivePreviewArrays{DB: c.DB, Bucket: c.Bucket}, c.Objects, bundle)
	if err != nil {
		return zero, err
	}
	if _, err = store.Save(ctx, plan); err != nil {
		return zero, err
	}
	reserved, err := store.Reserve(ctx, plan)
	if err != nil {
		return zero, err
	}
	if !reserved {
		return zero, artifactjson.ErrStale
	}
	task, err := PublishPerspectiveQualityTask(ctx, c.DB, c.Objects, c.Bucket, plan, bundle)
	if err != nil {
		return zero, err
	}
	if err = MaterializePerspectiveQualityTask(ctx, c.DB, plan, bundle, task); err != nil {
		return zero, err
	}
	if err = SealPerspectiveQuality(ctx, c.DB, plan, bundle, verified); err != nil {
		return zero, err
	}
	receipt, found, err := store.ReadReceipt(ctx, plan)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, artifactjson.ErrStale
	}
	return receipt, nil
}
