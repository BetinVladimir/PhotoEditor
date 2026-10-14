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
func MaterializeMultiQualityTask(ctx context.Context, db *pgxpool.Pool, plan MultiQualityPlan, content v2render.MultiResultContent, task PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateMultiQualityIntent(plan); err != nil {
		return err
	}
	if db == nil {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompileMultiQualityPlan(plan.source, content)
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
	body, err := multiQualityTaskBody(rebuilt, task)
	if err != nil {
		return err
	}
	root, image, err := content.PreviewDocuments(plan.source)
	if err != nil {
		return err
	}
	return withMultiPreviewDocumentTx(ctx, db, plan.source.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		var accepted bool
		err := tx.QueryRow(ctx, "SELECT photo.materialize_v29_multi_quality_task($1,$2,$3,$4,$5,$6::jsonb)", plan.source.Tenant, plan.run, "master", string(root), string(image), string(body)).Scan(&accepted)
		if err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
}
func multiQualityTaskBody(plan MultiQualityPlan, task PreparedTask) ([]byte, error) {
	expected, err := PrepareMultiQualityTask(plan, task.Params)
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
