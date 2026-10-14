package workflow

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
)

// Caller must retain this transaction through activation. A successful check
// must never be cached as source permission or used after committing the tx.
func perspectiveLinesSealedSourceInTx(ctx context.Context, tx pgx.Tx, plan PerspectiveLinesPlan, verified VerifiedPerspectiveLinesInputs, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	witness, err := verified.AdmissionJSON(plan, bucket)
	if err != nil {
		return err
	}
	if tx == nil {
		return objectstore.ErrInvalid
	}
	var accepted bool
	if err := tx.QueryRow(ctx, "SELECT photo.v29_perspective_lines_sealed_source_ready($1,$2,$3::jsonb)", plan.scope.Tenant, plan.scope.Run, string(witness)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}
