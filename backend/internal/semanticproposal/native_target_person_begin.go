package semanticproposal

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func lockNativeTargetAnchorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, a nativeTargetAnchor) error {
	if tx == nil {
		return v2input.ErrInvalid
	}
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return err
	}
	if err := verifyNativeTargetAnchor(ctx, a); err != nil {
		return err
	}
	expected, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, a.manifest, a.entry.Ordinal, a.stage.Operation)
	if err != nil {
		return err
	}
	if expected.seal != a.seal {
		return v2input.ErrConflict
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", d.request.TenantID.String()); err != nil {
		return err
	}
	packet := nativeAdmissionEvidence{SchemaVersion: 1, Operation: "face.detect", Tenant: d.request.TenantID, Project: d.published.Inventory.Scope.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: d.shell.Run, ParentRun: d.request.RunID, RequestSHA: d.request.RequestSHA, ConfigText: string(d.shell.Config), Anchor: defectAdmissionAnchor{IntentText: string(d.request.Body), SnapshotText: string(d.anchor.snapshotBody)}, Binding: d.request.Intent.Reference}
	evidence, err := canonical.Marshal(packet)
	if err != nil {
		return err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.lock_v27_target_preparation_entry($1,$2,$3,$4,$5,$6)", d.request.TenantID, d.shell.Run, json.RawMessage(evidence), a.manifest.sha, a.entry.Ordinal, a.stage.Operation).Scan(&raw); err != nil {
		return err
	}
	if len(raw) == 0 {
		return v2input.ErrConflict
	}
	var entry nativeTargetPreparationEntry
	if json.Unmarshal(raw, &entry) != nil {
		return v2input.ErrConflict
	}
	actual, err := canonical.Marshal(entry)
	if err != nil {
		return err
	}
	wanted, err := canonical.Marshal(a.entry)
	if err != nil {
		return err
	}
	if string(actual) != string(wanted) {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// Caller holds original parent advisory lock and owns rollback. This creates
// only a blocked task/draft/reservation; transport, canonical registration and
// dedicated target sealing are separate and must precede any execution.
func (s SQLTransferSourceReader) beginNativeTargetPersonInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetPersonPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
		return zero, err
	}
	rebuilt, err := s.compileNativeTargetPersonPlan(ctx, who, plan.anchor, plan.person.prepared, plan.person.policy, plan.person.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || rebuilt.person.prepared.source.iccSHA != color.sha {
		return zero, v2input.ErrConflict
	}
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, d, rebuilt.anchor); err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(rebuilt.person.policy, []string{"mask.person"})
	if err != nil {
		return zero, err
	}
	if err := s.lockSourcePersonPreparation(ctx, tx, who, rebuilt.anchor.request.TenantID, rebuilt.anchor.entry.Binding, color, rebuilt.person.prepared, selected["mask.person"]); err != nil {
		return zero, err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", rebuilt.anchor.request.TenantID, s.ProjectID, who.UserID, who.SessionID, rebuilt.person.policy.body).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, rebuilt.person.shell, rebuilt.person.key, rebuilt.anchor.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != rebuilt.person.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
