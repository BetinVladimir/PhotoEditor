package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent advisory lock and owns rollback. This creates
// only a blocked task/draft/reservation; transport, canonical registration and
// dedicated target sealing are separate and must precede any execution.
func (s SQLTransferSourceReader) beginNativeTargetDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetDetectorPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, err
	}
	rebuilt, err := s.compileNativeTargetDetectorPlan(ctx, who, plan.anchor, plan.detector.prepared, plan.detector.policy, plan.detector.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || rebuilt.detector.prepared.source.iccSHA != color.sha {
		return zero, v2input.ErrConflict
	}
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, d, rebuilt.anchor); err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(rebuilt.detector.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	if err := s.lockSourceDetectorPreparation(ctx, tx, who, rebuilt.anchor.request.TenantID, rebuilt.anchor.entry.Binding, color, rebuilt.detector.prepared, selected["face.detect"]); err != nil {
		return zero, err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", rebuilt.anchor.request.TenantID, s.ProjectID, who.UserID, who.SessionID, rebuilt.detector.policy.body).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, rebuilt.detector.shell, rebuilt.detector.key, rebuilt.anchor.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != rebuilt.detector.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
