package workflow

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type RestorationPreviewObjects interface {
	artifactjson.Objects
	RestorationPreviewImageObjects
}

// Prepare retains the pending review's before/after identity throughout the
// physical verification, durable publication, blocked materialization and seal.
type RestorationPreviewCoordinator struct {
	DB                          *pgxpool.Pool
	Objects                     RestorationPreviewObjects
	Bucket, WorkingSHA, SRGBSHA string
}

func (c RestorationPreviewCoordinator) Prepare(ctx context.Context,
	tenant, producer uuid.UUID) (RestorationPreviewReceipt, error) {
	var zero RestorationPreviewReceipt
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if c.DB == nil || c.Objects == nil || len(c.Bucket) < 3 || len(c.Bucket) > 63 ||
		domainDigestInvalid(c.WorkingSHA) || domainDigestInvalid(c.SRGBSHA) ||
		tenant == uuid.Nil || producer == uuid.Nil {
		return zero, objectstore.ErrInvalid
	}
	sources := SQLRestorationPreviewSource{DB: c.DB}
	pin, before, after, err := LoadRestorationPreviewImages(ctx, sources,
		SQLRestorationPreviewObjects{DB: c.DB, Bucket: c.Bucket}, c.Objects,
		tenant, producer)
	if err != nil {
		return zero, err
	}
	for _, raw := range [][]byte{before, after} {
		image, err := review.ParseWorkingImage(raw)
		if err != nil {
			return zero, err
		}
		if image.Color.ICCSHA256 != c.WorkingSHA {
			return zero, objectstore.ErrIntegrity
		}
	}
	plan, err := CompileRestorationPreviewPlan(pin, c.SRGBSHA, before, after)
	if err != nil {
		return zero, err
	}
	store := RestorationPreviewIntentStore{DB: c.DB}
	if receipt, found, err := store.ReadReceipt(ctx, plan); err != nil {
		return zero, err
	} else if found {
		return receipt, nil
	}
	verified, err := VerifyRestorationPreviewInputs(ctx, sources,
		SQLRestorationPreviewArrays{DB: c.DB, Bucket: c.Bucket}, c.Objects,
		plan, before, after)
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
	tasks, err := PublishRestorationPreviewTasks(ctx, c.DB, c.Objects, c.Bucket,
		plan, before, after)
	if err != nil {
		return zero, err
	}
	if err = MaterializeRestorationPreviewTasks(ctx, c.DB, plan,
		before, after, tasks); err != nil {
		return zero, err
	}
	if err = SealRestorationPreview(ctx, c.DB, plan, before, after,
		c.Bucket, verified); err != nil {
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
