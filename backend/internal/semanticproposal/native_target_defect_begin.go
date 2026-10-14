package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Caller holds original parent advisory lock and rolls back every error. All
// physical dependency/report/Params reads must finish before this transaction.
func (s SQLTransferSourceReader) beginNativeTargetDefectDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	owned, err := s.compileNativeTargetDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal || dependencies.preparation.seal != owned.prepared.preparation.seal {
		return zero, v2input.ErrConflict
	}
	a := owned.anchor
	stage, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, a.manifest, a.entry.Ordinal, "defects.propose")
	if err != nil {
		return zero, err
	}
	if stage.seal != a.seal || color.sha != owned.prepared.preparation.landmarksPlan.detector.detector.prepared.source.iccSHA {
		return zero, v2input.ErrConflict
	}
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, original, stage); err != nil {
		return zero, err
	}
	if err := s.lockNativeTargetDefectDependenciesInTransaction(ctx, tx, who, original, dependencies); err != nil {
		return zero, err
	}
	q := owned.prepared.qualification
	selected, err := selectQualificationPolicies(q.policy, []string{"defects.propose"})
	if err != nil {
		return zero, err
	}
	if err := s.lockDefectQualificationPreparation(ctx, tx, who, a.request.TenantID, q.report, selected["defects.propose"]); err != nil {
		return zero, err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", a.request.TenantID, s.ProjectID, who.UserID, who.SessionID, q.policy.body).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	params := owned.prepared.params
	obj := params.object
	if obj.Bytes != int64(len(owned.prepared.preparation.params.params)) || obj.SHA256 != params.ref.SHA256 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return zero, v2input.ErrConflict
	}
	pin, err := canonical.Marshal(map[string]any{"key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType, "bucket": s.Bucket})
	if err != nil {
		return zero, err
	}
	b := a.entry.Binding
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", a.request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, targetDefectParamsDocument(a.entry.Namespace), params.ref.ArtifactID, params.ref.SHA256, string(pin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, owned.shell, owned.key, original.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != owned.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, ctx.Err()
}
