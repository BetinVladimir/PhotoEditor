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

// This registers a blocked quality task. Pixel verification and seal are separate.
func MaterializePerspectiveQualityTask(ctx context.Context, db *pgxpool.Pool, plan PerspectiveQualityPlan, bundle v2render.PerspectivePreviewBundle, task PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validatePerspectiveQualityIntent(plan); err != nil {
		return err
	}
	if db == nil || bundle.Source() != plan.source {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompilePerspectiveQualityPlan(bundle)
	if err != nil {
		return err
	}
	if rebuilt.run != plan.run || !bytes.Equal(rebuilt.params, plan.params) || !bytes.Equal(rebuilt.profile, plan.profile) || len(rebuilt.inputs) != len(plan.inputs) {
		return objectstore.ErrIntegrity
	}
	for i, ref := range rebuilt.inputs {
		if ref != plan.inputs[i] {
			return objectstore.ErrIntegrity
		}
	}
	body, err := perspectiveQualityTaskBody(rebuilt, task)
	if err != nil {
		return err
	}
	docs := bundle.Documents()
	root := docs[plan.source.Result.ArtifactID.String()]
	image := docs[bundle.Result().WorkingImage.ArtifactID.String()]
	return withPerspectivePreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		var accepted bool
		err := tx.QueryRow(ctx, "SELECT photo.materialize_v29_perspective_quality_task($1,$2,$3,$4,$5,$6::jsonb)", plan.source.Tenant, plan.run, "master", string(root), string(image), string(body)).Scan(&accepted)
		if err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
}
func perspectiveQualityTaskBody(plan PerspectiveQualityPlan, task PreparedTask) ([]byte, error) {
	expected, err := PreparePerspectiveQualityTask(plan, task.Params)
	if err != nil {
		return nil, err
	}
	body, err := canonical.Marshal(task)
	if err != nil {
		return nil, err
	}
	want, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(body, want) {
		return nil, objectstore.ErrIntegrity
	}
	return bytes.Clone(body), nil
}
