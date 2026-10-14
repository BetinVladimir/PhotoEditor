package workflow

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

// Registers a blocked task; this does not seal the run or permit dispatch.
func MaterializeRenderDerivativeTask(ctx context.Context, db *pgxpool.Pool, tenant uuid.UUID, document RenderDerivativeDocument, task PreparedTask, root, image []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil {
		return objectstore.ErrInvalid
	}
	expected, err := PrepareRenderDerivativeTask(document, task.Params)
	if err != nil {
		return err
	}
	body, err := canonical.Marshal(task)
	if err != nil {
		return err
	}
	want, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	if string(body) != string(want) {
		return objectstore.ErrInvalid
	}
	var accepted bool
	err = db.QueryRow(ctx, "SELECT photo.materialize_v26_render_derivative_task($1,$2,$3,$4,$5,$6::jsonb)", tenant, document.Run, document.Variant, string(root), string(image), string(body)).Scan(&accepted)
	if err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}

// SealRenderDerivative makes the exact two validated preview tasks dispatchable
// atomically. Replay does not change revision or reactivate completed tasks.
func SealRenderDerivative(ctx context.Context, db *pgxpool.Pool, tenant, run uuid.UUID, root, image []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if db == nil || tenant == uuid.Nil || run == uuid.Nil || len(root) < 2 || len(root) > 1048576 || len(image) < 2 || len(image) > 1048576 {
		return objectstore.ErrInvalid
	}
	var sealed bool
	if err := db.QueryRow(ctx, "SELECT photo.seal_v26_render_derivative($1,$2,$3,$4)", tenant, run, string(root), string(image)).Scan(&sealed); err != nil {
		return err
	}
	if !sealed {
		return artifactjson.ErrStale
	}
	return nil
}
