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

type RenderDerivativeObjects interface {
	ArtifactOpener
	artifactjson.Objects
}

// Independent retries use durable intent/doc/task identities. Cursor advancement
// avoids one failing intent starving all later runs; wrap retries earlier work.
type RenderDerivativeCoordinator struct {
	DB                       *pgxpool.Pool
	Objects                  RenderDerivativeObjects
	Bucket, WorkSHA, SRGBSHA string
	after                    uuid.UUID
}

func (p *RenderDerivativeCoordinator) ProcessOne(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if p == nil || p.DB == nil || p.Objects == nil || p.Bucket == "" || domainDigestInvalid(p.WorkSHA) || domainDigestInvalid(p.SRGBSHA) {
		return false, objectstore.ErrInvalid
	}
	store := v2render.SQLRenderDerivativeIntents{DB: p.DB}
	// One fresh admission per iteration, then advance existing intents as well.
	// This prevents a continuous admission stream from starving materialization.
	var enqueued bool
	if err := p.DB.QueryRow(ctx, "SELECT photo.enqueue_next_v26_render_derivative($1)", p.SRGBSHA).Scan(&enqueued); err != nil {
		return false, err
	}
	// Admit semantic previews independently so a Render stream cannot starve them.
	var semanticEnqueued bool
	if err := p.DB.QueryRow(ctx, "SELECT photo.enqueue_next_semantic_exposure_preview($1)", p.SRGBSHA).Scan(&semanticEnqueued); err != nil {
		return false, err
	}
	enqueued = enqueued || semanticEnqueued
	intent, err := store.Next(ctx, p.after)
	if err != nil {
		return false, err
	}
	if intent == nil {
		p.after = uuid.Nil
		return enqueued, nil
	}
	p.after = intent.Run
	if intent.SRGBSHA != p.SRGBSHA {
		return false, objectstore.ErrIntegrity
	}
	root, image, err := (RenderDerivativeLoader{DB: p.DB, Objects: p.Objects, Bucket: p.Bucket}).Load(ctx, *intent)
	if err != nil {
		return false, err
	}
	working, err := review.ParseWorkingImage(image)
	if err != nil {
		return false, err
	}
	if working.Color.ICCSHA256 != p.WorkSHA {
		return false, objectstore.ErrIntegrity
	}
	accepted, err := store.Reserve(ctx, intent.Tenant, intent.Run)
	if err != nil {
		return false, err
	}
	if !accepted {
		return false, nil
	}
	documents, err := BuildRenderDerivativeDocuments(*intent, root, image)
	if err != nil {
		return false, err
	}
	tasks, err := PublishRenderDerivativeTasks(ctx, p.DB, p.Objects, p.Bucket, *intent, root, image)
	if err != nil {
		return false, err
	}
	for i, task := range tasks {
		if err = MaterializeRenderDerivativeTask(ctx, p.DB, intent.Tenant, documents[i], task, root, image); err != nil {
			return false, err
		}
	}
	if err = SealRenderDerivative(ctx, p.DB, intent.Tenant, intent.Run, root, image); err != nil {
		return false, err
	}
	return true, nil
}
