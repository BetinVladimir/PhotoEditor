package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

// SealPerspectiveLinesTransport freezes wire inputs/profile on a blocked task.
// Exact blocked-seal replay is acknowledged from a durable SQL receipt.
// This does not authorize graph activation or replace fresh source checks.
func SealPerspectiveLinesTransport(ctx context.Context, db *pgxpool.Pool, plan PerspectiveLinesPlan, params review.ArtifactRef, closure PerspectiveLinesClosure, verified VerifiedPerspectiveLinesInputs, bucket string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	proof, witness, err := perspectiveLinesRegistrationProof(plan, params, closure, verified, bucket)
	if err != nil {
		return err
	}
	if db == nil {
		return objectstore.ErrInvalid
	}
	return withPerspectivePreviewDocumentTx(ctx, db, plan.scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		return sealPerspectiveLinesTransportInTx(ctx, tx, plan.scope.Tenant, plan.scope.Run, proof, witness)
	})
}

func sealPerspectiveLinesTransportInTx(ctx context.Context, tx pgx.Tx, tenant, run uuid.UUID, proof, witness json.RawMessage) error {
	var accepted bool
	if err := tx.QueryRow(ctx, "SELECT photo.seal_v29_perspective_lines_transport($1,$2,$3::jsonb,$4::jsonb)", tenant, run, string(proof), string(witness)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return artifactjson.ErrStale
	}
	return nil
}
