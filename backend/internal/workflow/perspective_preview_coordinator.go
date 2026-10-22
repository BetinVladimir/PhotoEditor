package workflow

import (
	"photoeditor/backend/internal/media"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

type PerspectivePreviewObjects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}

// Prepares immutable preview tasks after scoped producer and pixel verification.
type PerspectivePreviewCoordinator struct {
	DB                          *pgxpool.Pool
	Objects                     PerspectivePreviewObjects
	Bucket, WorkingSHA, SRGBSHA string
}

func (c PerspectivePreviewCoordinator) Prepare(ctx context.Context, tenant, producer uuid.UUID) (PerspectivePreviewReceipt, error) {
	var zero PerspectivePreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Objects == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || domainDigestInvalid(c.WorkingSHA) || domainDigestInvalid(c.SRGBSHA) || tenant == uuid.Nil || producer == uuid.Nil {
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
	plan, err := CompilePerspectivePreviewPlan(bundle, c.SRGBSHA)
	if err != nil {
		return zero, err
	}
	policy,err:=media.PolicyForPreview(ctx,c.DB,tenant,plan.run,producer);if err!=nil{return zero,err}
	plan,err=CompilePerspectivePreviewPlan(bundle,c.SRGBSHA,policy);if err!=nil{return zero,err}
	store := PerspectivePreviewIntentStore{DB: c.DB}
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
	tasks, err := PublishPerspectivePreviewTasks(ctx, c.DB, c.Objects, c.Bucket, plan, bundle)
	if err != nil {
		return zero, err
	}
	if err = MaterializePerspectivePreviewTasks(ctx, c.DB, plan, bundle, tasks); err != nil {
		return zero, err
	}
	if err = SealPerspectivePreview(ctx, c.DB, plan, bundle, verified); err != nil {
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
