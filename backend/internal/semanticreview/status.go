package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
)

type ReviewStatus struct {
	Kind       *string   `json:"kind,omitempty"`
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	Revision   string    `json:"revision"`
	State      string    `json:"state"`
	PlanSHA    *string   `json:"plan_sha"`
	Remaining  int       `json:"remaining"`
	CanClose   bool      `json:"can_close"`
}

// ReadStatus returns an advisory parent fence under current editor authority.
// A missing/expired source plan disables closure without hiding terminal state.
func ReadStatus(ctx context.Context, db *pgxpool.Pool, who auth.Identity, tenant, intent uuid.UUID) (ReviewStatus, error) {
	if tenant == uuid.Nil || intent == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return ReviewStatus{}, ErrInvalid
	}
	if db == nil {
		return ReviewStatus{}, ErrUnavailable
	}
	var result ReviewStatus
	err := db.QueryRow(ctx, `SELECT run_id,workflow_id,revision,state,plan_sha,remaining,can_close FROM photo.semantic_review_status($1,$2,$3,$4)`, tenant, intent, who.UserID, who.SessionID).Scan(&result.RunID, &result.WorkflowID, &result.Revision, &result.State, &result.PlanSHA, &result.Remaining, &result.CanClose)
	if err != nil {
		return ReviewStatus{}, err
	}
	if result.RunID == uuid.Nil || result.WorkflowID == uuid.Nil || !validRevision(result.Revision) || result.Remaining < 0 || result.Remaining > 5000 || result.PlanSHA != nil && !shaPattern.MatchString(*result.PlanSHA) {
		return ReviewStatus{}, ErrInvalid
	}
	switch result.State {
	case "queued", "running", "waiting_review", "cancelling", "cancelled", "completed", "failed":
	default:
		return ReviewStatus{}, ErrInvalid
	}
	if result.CanClose && (result.State != "waiting_review" || result.Remaining != 0 || result.PlanSHA == nil || result.Revision == "9007199254740991") {
		return ReviewStatus{}, ErrInvalid
	}
	return result, nil
}
