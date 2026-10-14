package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

// ActivatePerspectiveLines repeats fresh SQL source authority in the graph
// transaction. Registry enablement remains a prerequisite, never changed here.
func ActivatePerspectiveLines(ctx context.Context, db *pgxpool.Pool, plan PerspectiveLinesPlan, verified VerifiedPerspectiveLinesInputs, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	witness, err := verified.AdmissionJSON(plan, bucket)
	if err != nil {
		return err
	}
	if db == nil {
		return objectstore.ErrInvalid
	}
	return withPerspectivePreviewDocumentTx(ctx, db, plan.scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return activatePerspectiveLinesInTx(ctx, tx, plan.scope.Tenant, plan.scope.Run, witness)
	})
}
func activatePerspectiveLinesInTx(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID, witness json.RawMessage) error {
	var accepted bool
	if err := tx.QueryRow(ctx, "SELECT photo.activate_v29_perspective_lines($1,$2,$3::jsonb)", tenant, run, string(witness)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}
