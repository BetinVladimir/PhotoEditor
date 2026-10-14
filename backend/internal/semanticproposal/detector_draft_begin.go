package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Persists only a blocked child draft after full retained scope/inventory locks.
// Caller owns scheduler transaction and must roll back every failure. Transport
// publication, graph registration, sealing and receipt activation are separate.
func (s SQLTransferSourceReader) beginDetectorDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan detectorDraftPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	// Recompile original owned anchor/config/tasks, never trust mutable shell data.
	rebuilt, err := s.recompileDetectorDraftPlan(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	a, err := canonical.Marshal(plan.shell)
	if err != nil {
		return zero, err
	}
	b, err := canonical.Marshal(rebuilt.shell)
	if err != nil || string(a) != string(b) || plan.key != rebuilt.key {
		return zero, v2input.ErrConflict
	}
	if err := LockInventoryInTransaction(ctx, tx, who, rebuilt.request, rebuilt.published.Inventory); err != nil {
		return zero, err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, rebuilt.request, rebuilt.published); err != nil {
		return zero, err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, rebuilt.request, rebuilt.anchor); err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(rebuilt.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	if err := s.lockSourceDetectorPreparation(ctx, tx, who, rebuilt.request.TenantID, rebuilt.request.Intent.Reference, color, rebuilt.prepared, selected["face.detect"]); err != nil {
		return zero, err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", rebuilt.request.TenantID, s.ProjectID, who.UserID, who.SessionID, rebuilt.policy.body).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, rebuilt.shell, rebuilt.key, rebuilt.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != rebuilt.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
