package workflow

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2render"
)

// MaterializePerspectivePreviewTasks registers both tasks as blocked. It never
// authorizes dispatch, and the SQL pin independently repeats source and quota.
func MaterializePerspectivePreviewTasks(ctx context.Context, db *pgxpool.Pool, plan PerspectivePreviewPlan, bundle v2render.PerspectivePreviewBundle, tasks []PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePerspectivePreviewIntent(plan); err != nil {
		return err
	}
	if db == nil || bundle.Source() != plan.source || len(tasks) != 2 {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompilePerspectivePreviewPlan(bundle, plan.srgb,plan.policy)
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
	return withPerspectivePreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		for i, d := range documents {
			var accepted bool
			if err := tx.QueryRow(ctx, "SELECT photo.materialize_v29_perspective_preview_task($1,$2,$3,$4,$5,$6::jsonb)", plan.source.Tenant, plan.run, d.Variant, root, image, string(bodies[i])).Scan(&accepted); err != nil {
				return err
			}
			if !accepted {
				return artifactjson.ErrStale
			}
		}
		return nil
	})
}

func renderDerivativeTaskBodies(documents []RenderDerivativeDocument, tasks []PreparedTask) ([][]byte, error) {
	if len(documents) != 2 || len(tasks) != 2 {
		return nil, objectstore.ErrInvalid
	}
	bodies := make([][]byte, 2)
	for i, d := range documents {
		expected, err := PrepareRenderDerivativeTask(d, tasks[i].Params)
		if err != nil {
			return nil, err
		}
		body, err := canonical.Marshal(tasks[i])
		if err != nil {
			return nil, err
		}
		want, err := canonical.Marshal(expected)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(body, want) {
			return nil, objectstore.ErrInvalid
		}
		bodies[i] = bytes.Clone(body)
	}
	return bodies, nil
}

// Kept for existing perspective call sites; both producers share exact media task binding.
func perspectivePreviewTaskBodies(documents []RenderDerivativeDocument, tasks []PreparedTask) ([][]byte, error) {
	return renderDerivativeTaskBodies(documents, tasks)
}
