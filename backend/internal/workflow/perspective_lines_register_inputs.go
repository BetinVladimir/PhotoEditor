package workflow

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

func perspectiveLinesRegistrationProof(plan PerspectiveLinesPlan, params review.ArtifactRef, closure PerspectiveLinesClosure, verified VerifiedPerspectiveLinesInputs, bucket string) (json.RawMessage, json.RawMessage, error) {
	proof, err := closure.AdmissionJSON(plan, params)
	if err != nil {
		return nil, nil, err
	}
	witness, err := verified.AdmissionJSON(plan, bucket)
	if err != nil {
		return nil, nil, err
	}
	return proof, witness, nil
}

// Register complete typed rows only. The dedicated workflow must already exist;
// transport profile sealing, registry enablement and dispatch are separate.
func RegisterPerspectiveLinesInputs(ctx context.Context, db *pgxpool.Pool, plan PerspectiveLinesPlan, params review.ArtifactRef, closure PerspectiveLinesClosure, verified VerifiedPerspectiveLinesInputs, bucket string) error {
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
		var accepted bool
		if err := tx.QueryRow(ctx, "SELECT photo.register_v29_perspective_lines_inputs($1,$2,$3::jsonb,$4::jsonb)", plan.scope.Tenant, plan.scope.Run, string(proof), string(witness)).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
}
