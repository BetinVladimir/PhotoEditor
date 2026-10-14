package workflow

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func perspectiveLinesWorkflowProof(plan PerspectiveLinesPlan, policy PerspectiveLinesExecutionPolicy, verified VerifiedPerspectiveLinesInputs, bucket string) (json.RawMessage, json.RawMessage, error) {
	raw, err := policy.ForPlan(plan)
	if err != nil {
		return nil, nil, err
	}
	witness, err := verified.AdmissionJSON(plan, bucket)
	if err != nil {
		return nil, nil, err
	}
	return raw, witness, nil
}

// CreatePerspectiveLinesWorkflow freezes the operator policy after current IO
// authority is repeated in SQL. It does not seal tasks or enable dispatch.
func CreatePerspectiveLinesWorkflow(ctx context.Context, db *pgxpool.Pool, plan PerspectiveLinesPlan, policy PerspectiveLinesExecutionPolicy, verified VerifiedPerspectiveLinesInputs, bucket string) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	raw, witness, err := perspectiveLinesWorkflowProof(plan, policy, verified, bucket)
	if err != nil {
		return uuid.Nil, err
	}
	if db == nil {
		return uuid.Nil, objectstore.ErrInvalid
	}
	expected, err := v2input.WorkflowID(plan.scope.Run)
	if err != nil {
		return uuid.Nil, err
	}
	var saved *uuid.UUID
	err = withPerspectivePreviewDocumentTx(ctx, db, plan.scope.Tenant, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT photo.create_v29_perspective_lines_workflow($1,$2,$3,$4::jsonb)", plan.scope.Tenant, plan.scope.Run, string(raw), string(witness)).Scan(&saved); err != nil {
			return err
		}
		if saved == nil || *saved != expected {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	return expected, nil
}
