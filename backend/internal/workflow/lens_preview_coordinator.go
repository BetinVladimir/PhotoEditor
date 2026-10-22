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

type LensPreviewObjects interface {
	artifactjson.Objects
	v2input.SessionAnalysisObjectReader
}

// Prepares immutable preview tasks after scoped producer and pixel verification.
type LensPreviewCoordinator struct {
	DB                          *pgxpool.Pool
	Objects                     LensPreviewObjects
	Bucket, WorkingSHA, SRGBSHA string
}

func (c LensPreviewCoordinator) Prepare(ctx context.Context, tenant, producer uuid.UUID) (LensPreviewReceipt, error) {
	var zero LensPreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Objects == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || domainDigestInvalid(c.WorkingSHA) || domainDigestInvalid(c.SRGBSHA) || tenant == uuid.Nil || producer == uuid.Nil {
		return zero, objectstore.ErrInvalid
	}
	sources := v2render.SQLLensPreviewSource{DB: c.DB}
	bundle, err := v2render.LoadLensPreviewBundle(ctx, sources, v2render.SQLLensPreviewObjects{DB: c.DB, Bucket: c.Bucket}, c.Objects, tenant, producer)
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
	plan, err := CompileLensPreviewPlan(bundle, c.SRGBSHA)
	if err != nil {
		return zero, err
	}
	policy,err:=media.PolicyForPreview(ctx,c.DB,tenant,plan.run,producer);if err!=nil{return zero,err}
	plan,err=CompileLensPreviewPlan(bundle,c.SRGBSHA,policy);if err!=nil{return zero,err}
	store := LensPreviewIntentStore{DB: c.DB}
	if receipt, found, err := store.ReadReceipt(ctx, plan); err != nil {
		return zero, err
	} else if found {
		return receipt, nil
	}
	verified, err := v2render.VerifyLensPreviewInputs(ctx, sources, v2render.SQLLensPreviewArrays{DB: c.DB, Bucket: c.Bucket}, c.Objects, bundle)
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
	tasks, err := PublishLensPreviewTasks(ctx, c.DB, c.Objects, c.Bucket, plan, bundle)
	if err != nil {
		return zero, err
	}
	if err = MaterializeLensPreviewTasks(ctx, c.DB, plan, bundle, tasks); err != nil {
		return zero, err
	}
	if err = SealLensPreview(ctx, c.DB, plan, bundle, verified); err != nil {
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
