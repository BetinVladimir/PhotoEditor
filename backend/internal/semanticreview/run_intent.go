package semanticreview

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

// RunIntent resolves only an admitted semantic parent. IntentID remains null
// until review initialization; a workflow UUID is never an intent identifier.
type RunIntent struct {
	RunID      uuid.UUID  `json:"run_id"`
	WorkflowID uuid.UUID  `json:"workflow_id"`
	IntentID   *uuid.UUID `json:"intent_id"`
	Revision   string     `json:"revision"`
	State      string     `json:"state"`
}

func ReadRunIntent(ctx context.Context, db *pgxpool.Pool, who auth.Identity, tid, run uuid.UUID) (RunIntent, error) {
	if tid == uuid.Nil || run == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return RunIntent{}, ErrInvalid
	}
	if db == nil {
		return RunIntent{}, ErrUnavailable
	}
	var result RunIntent
	err := tenant.WithTenant(ctx, db, who, tid, domain.Editor, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT r.id,w.id,i.id,r.revision::text,r.status,w.project_id
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id
 LEFT JOIN photo.semantic_intents i ON i.tenant_id=w.tenant_id AND i.project_id=w.project_id AND i.workflow_id=w.id
 WHERE r.tenant_id=$1 AND r.id=$2 AND r.kind='v2' AND r.graph_sealed
 AND w.workflow_type='semantic' AND jsonb_typeof(r.config_snapshot->'v27_semantic')='object'`, tid, run).Scan(&result.RunID, &result.WorkflowID, &result.IntentID, &result.Revision, &result.State, &projectID)
		if err != nil {
			return err
		}
		_, err = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return RunIntent{}, domain.NewError("NOT_FOUND", 404)
	}
	if err != nil {
		return RunIntent{}, err
	}
	if result.RunID != run || result.WorkflowID == uuid.Nil || result.IntentID != nil && *result.IntentID == uuid.Nil || !validRevision(result.Revision) {
		return RunIntent{}, ErrInvalid
	}
	switch result.State {
	case "queued", "running", "waiting_review", "cancelling", "cancelled", "completed", "failed":
	default:
		return RunIntent{}, ErrInvalid
	}
	return result, nil
}
