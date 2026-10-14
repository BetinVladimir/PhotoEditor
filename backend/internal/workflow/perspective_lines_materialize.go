package workflow

import (
	"bytes"
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

func perspectiveLinesTaskBody(plan PerspectiveLinesPlan, task PreparedTask) ([]byte, error) {
	expected, err := PreparePerspectiveLinesTask(plan, task.Params)
	if err != nil {
		return nil, err
	}
	body, err := canonical.Marshal(task)
	if err != nil {
		return nil, err
	}
	exact, err := canonical.Marshal(expected)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(body, exact) {
		return nil, objectstore.ErrIntegrity
	}
	return bytes.Clone(body), nil
}

// Materialize stores a blocked task only. Original object verification, typed
// closure publication and seal must complete before any dispatch is enabled.
func MaterializePerspectiveLinesTask(ctx context.Context, db *pgxpool.Pool, plan PerspectiveLinesPlan, task PreparedTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := perspectiveLinesTaskBody(plan, task)
	if err != nil {
		return err
	}
	if db == nil {
		return objectstore.ErrInvalid
	}
	return withPerspectivePreviewDocumentTx(ctx, db, plan.scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		var accepted bool
		err := tx.QueryRow(ctx, "SELECT photo.materialize_v29_perspective_line_detection_task($1,$2,$3::jsonb)", plan.scope.Tenant, plan.scope.Run, string(body)).Scan(&accepted)
		if err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
}
