package workflow

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
)

// MaterializeMultiPreviewTasks atomically registers both exact media tasks as
// blocked. The database repeats current source/document/quota authority; this
// method does not seal the graph or authorize dispatch.
func MaterializeMultiPreviewTasks(ctx context.Context, db *pgxpool.Pool, plan MultiPreviewPlan, content v2render.MultiResultContent, tasks []PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMultiPreviewPlan(plan); err != nil {
		return err
	}
	if db == nil || len(tasks) != 2 {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompileMultiPreviewPlan(plan.source, content, plan.srgb)
	if err != nil {
		return err
	}
	if rebuilt.run != plan.run {
		return objectstore.ErrInvalid
	}
	documents := rebuilt.Documents()
	bodies, err := renderDerivativeTaskBodies(documents, tasks)
	if err != nil {
		return err
	}
	root, image, err := content.PreviewDocuments(plan.source)
	if err != nil {
		return err
	}
	return withMultiPreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		for i, d := range documents {
			var accepted bool
			if err := tx.QueryRow(ctx, "SELECT photo.materialize_v29_multi_preview_task($1,$2,$3,$4,$5,$6::jsonb)", plan.source.Tenant, plan.run, d.Variant, string(root), string(image), string(bodies[i])).Scan(&accepted); err != nil {
				return err
			}
			if !accepted {
				return artifactjson.ErrStale
			}
		}
		return nil
	})
}
