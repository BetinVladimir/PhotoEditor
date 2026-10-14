package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type DraftAdmission struct {
	RunID   uuid.UUID
	Outcome string
}

// BeginDraftInTransaction creates only blocked draft tasks. Caller rolls back
// on error; transport publication and graph sealing follow after commit.
// A queued HTTP ticket is forbidden until independent input admission finishes.
func BeginDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, key string, published PublishedIntent, plan WorkflowPlan, reservedBytes int64) (DraftAdmission, error) {
	var empty DraftAdmission
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if tx == nil || who.UserID != request.ActorID || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	owned, err := CompileRequest(request.TenantID, request.ActorID, key, request.Body)
	if err != nil {
		return empty, err
	}
	if owned.RunID != request.RunID || owned.WorkflowID != request.WorkflowID || owned.RequestSHA != request.RequestSHA || plan.RunID() != owned.RunID || plan.WorkflowID() != owned.WorkflowID || plan.RequestSHA() != owned.RequestSHA || plan.tenant != owned.TenantID || plan.actor != owned.ActorID || plan.IntentRef() != published.Ref {
		return empty, v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(plan.ConfigJSON(), &config) != nil || config.Semantic.AuthSessionID != who.SessionID {
		return empty, v2input.ErrInvalid
	}
	shell, err := plan.DraftShell(reservedBytes)
	if err != nil {
		return empty, err
	}
	inventory := plan.Inventory()
	if published.Inventory.Scope != inventory.Scope || len(published.Inventory.Targets) != len(inventory.Targets) {
		return empty, v2input.ErrInvalid
	}
	for i := range inventory.Targets {
		if published.Inventory.Targets[i] != inventory.Targets[i] {
			return empty, v2input.ErrInvalid
		}
	}
	if err := LockPreparedSourcesInTransaction(ctx, tx, who, owned, plan); err != nil {
		return empty, err
	}
	if err := LockInventoryInTransaction(ctx, tx, who, owned, inventory); err != nil {
		return empty, err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, owned, published); err != nil {
		return empty, err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, owned, plan); err != nil {
		return empty, err
	}
	if err := LockQualificationInTransaction(ctx, tx, who, owned, plan); err != nil {
		return empty, err
	}
	if err := LockPreparedSourcesInTransaction(ctx, tx, who, owned, plan); err != nil {
		return empty, err
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, shell, key, owned.RequestSHA)
	if err != nil {
		return empty, err
	}
	if outcome == "conflict" {
		return empty, v2input.ErrConflict
	}
	if outcome == "expired" {
		return empty, v2input.ErrExpired
	}
	if run != owned.RunID {
		return empty, v2input.ErrInvalid
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
