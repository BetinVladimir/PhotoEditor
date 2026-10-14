package workflow

import (
	"bytes"
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func restorationPreviewTaskBodies(documents []RestorationPreviewDocument,
	tasks []PreparedTask) ([][]byte, error) {
	if len(documents) != 4 || len(tasks) != 4 {
		return nil, objectstore.ErrInvalid
	}
	bodies := make([][]byte, 4)
	for i, doc := range documents {
		expected, err := PrepareRestorationPreviewTask(doc, tasks[i].Params)
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

// MaterializeRestorationPreviewTasks atomically registers four blocked media
// tasks. The SQL pin repeats current authority and quota for each task.
func MaterializeRestorationPreviewTasks(ctx context.Context, db *pgxpool.Pool,
	plan RestorationPreviewPlan, baseRaw, candidateRaw []byte, tasks []PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateRestorationPreviewIntent(plan); err != nil {
		return err
	}
	if db == nil || len(tasks) != 4 {
		return objectstore.ErrInvalid
	}
	rebuilt, err := CompileRestorationPreviewPlan(plan.source, plan.srgb,
		baseRaw, candidateRaw)
	if err != nil {
		return err
	}
	if rebuilt.run != plan.run {
		return v2input.ErrInvalid
	}
	documents := rebuilt.Documents()
	for i := range documents {
		if documents[i].ID != plan.docs[i].ID ||
			!bytes.Equal(documents[i].Params, plan.docs[i].Params) ||
			!bytes.Equal(documents[i].Profile, plan.docs[i].Profile) ||
			!restorationPreviewInputsEqual(documents[i].Inputs, plan.docs[i].Inputs) {
			return v2input.ErrInvalid
		}
	}
	bodies, err := restorationPreviewTaskBodies(documents, tasks)
	if err != nil {
		return err
	}
	return withPerspectivePreviewDocumentTx(ctx, db, plan.source.Tenant,
		func(ctx context.Context, tx pgx.Tx) error {
			for i, doc := range documents {
				image := baseRaw
				if doc.Side == "after" {
					image = candidateRaw
				}
				var accepted bool
				if err := tx.QueryRow(ctx,
					"SELECT photo.materialize_v28_restoration_preview_task($1,$2,$3,$4,$5,$6::jsonb)",
					plan.source.Tenant, plan.run, doc.Side, doc.Variant,
					string(image), string(bodies[i])).Scan(&accepted); err != nil {
					return err
				}
				if !accepted {
					return artifactjson.ErrStale
				}
			}
			return nil
		})
}
