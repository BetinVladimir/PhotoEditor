package workflow

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
)

// MaterializeLensPreviewTasks registers both tasks as blocked. It never
// authorizes dispatch, and the SQL pin independently repeats source and quota.
func MaterializeLensPreviewTasks(ctx context.Context, db *pgxpool.Pool, plan LensPreviewPlan, bundle v2render.LensPreviewBundle, tasks []PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateLensPreviewIntent(plan); err != nil {
		return err
	}
	if db == nil || bundle.Source() != plan.source || len(tasks) != 2 {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompileLensPreviewPlan(bundle, plan.srgb,plan.policy)
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
	originals := bundle.Documents()
	result := bundle.Result()
	root, image := string(originals[plan.source.Result.ArtifactID.String()]), string(originals[result.WorkingImage.ArtifactID.String()])
	return withLensPreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		for i, d := range documents {
			var accepted bool
			if err := tx.QueryRow(ctx, "SELECT photo.materialize_v25_lens_preview_task($1,$2,$3,$4,$5,$6::jsonb)", plan.source.Tenant, plan.run, d.Variant, root, image, string(bodies[i])).Scan(&accepted); err != nil {
				return err
			}
			if !accepted {
				return artifactjson.ErrStale
			}
		}
		return nil
	})
}
