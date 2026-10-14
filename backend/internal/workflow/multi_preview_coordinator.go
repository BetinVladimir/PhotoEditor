package workflow

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
)

type MultiPreviewCoordinator struct {
	DB                          *pgxpool.Pool
	Objects                     PerspectivePreviewObjects
	Bucket, WorkingSHA, SRGBSHA string
}

func (c MultiPreviewCoordinator) Prepare(ctx context.Context, tenant, producer uuid.UUID, operation string) (MultiPreviewReceipt, error) {
	var zero MultiPreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Objects == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 || domainDigestInvalid(c.WorkingSHA) || domainDigestInvalid(c.SRGBSHA) || tenant == uuid.Nil || producer == uuid.Nil || (operation != "optical.panorama" && operation != "optical.focus_stack") {
		return zero, objectstore.ErrInvalid
	}
	pin, err := (v2render.SQLMultiPreviewSource{DB: c.DB}).Resolve(ctx, tenant, producer, operation)
	if err != nil {
		return zero, err
	}
	catalogue := v2render.SQLMultiPreviewCatalogue{DB: c.DB, Source: pin, Bucket: c.Bucket}
	content, err := v2render.LoadAdoptedMultiContent(ctx, catalogue, c.Objects, pin.Principal, tenant, pin.Project, producer, operation, pin.Binding.Result, c.Bucket)
	if err != nil {
		return zero, err
	}
	image, err := content.Image()
	if err != nil {
		return zero, err
	}
	if image.Color.ICCSHA256 != c.WorkingSHA {
		return zero, objectstore.ErrIntegrity
	}
	plan, err := CompileMultiPreviewPlan(pin, content, c.SRGBSHA)
	if err != nil {
		return zero, err
	}
	store := MultiPreviewIntentStore{DB: c.DB}
	if receipt, found, err := store.ReadReceipt(ctx, plan); err != nil {
		return zero, err
	} else if found {
		return receipt, nil
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
	tasks, err := PublishMultiPreviewTasks(ctx, c.DB, c.Objects, c.Bucket, plan, content)
	if err != nil {
		return zero, err
	}
	if err = MaterializeMultiPreviewTasks(ctx, c.DB, plan, content, tasks); err != nil {
		return zero, err
	}
	if err = SealMultiPreview(ctx, c.DB, plan, content); err != nil {
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
