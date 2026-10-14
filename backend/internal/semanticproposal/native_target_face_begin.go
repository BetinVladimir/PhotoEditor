package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// The caller owns a scheduler transaction and must roll back every failure.
// Only a blocked draft is persisted. No object IO, registration or queueing
// occurs here; final admission must repeat all these authority barriers.
func (s SQLTransferSourceReader) beginNativeTargetFaceDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetFaceDraftPlan, color TransferColorProfile, detectorBuilt v2input.Built) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	rebuilt, err := s.compileNativeTargetFaceDraftPlan(ctx, who, plan.detector, plan.result, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || rebuilt.result.completed.catalog != rebuilt.prepared.detector {
		return zero, v2input.ErrConflict
	}
	paramsPin, reportPin, err := marshalNativeFacePreparationPins(ctx, rebuilt.prepared)
	if err != nil {
		return zero, err
	}
	parent := rebuilt.detector.detector
	stageAnchor, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, rebuilt.detector.anchor.manifest, rebuilt.detector.anchor.entry.Ordinal, rebuilt.prepared.operation)
	if err != nil {
		return zero, err
	}
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, original, stageAnchor); err != nil {
		return zero, err
	}
	if color.sha != parent.prepared.source.iccSHA {
		return zero, v2input.ErrConflict
	}
	if err := s.lockNativeTargetDetectorResult(ctx, tx, who, original, rebuilt.detector, detectorBuilt, rebuilt.result); err != nil {
		return zero, err
	}
	query := "SELECT photo.lock_semantic_proposal_face_qualification($1,$2,$3,$4,$5,$6)"
	if rebuilt.prepared.operation == "face.parse" {
		query = "SELECT photo.lock_semantic_proposal_face_parse_qualification($1,$2,$3,$4,$5,$6)"
	}
	var ready bool
	if err := tx.QueryRow(ctx, query, parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, rebuilt.prepared.expected, string(reportPin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	binding := rebuilt.detector.anchor.entry.Binding
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, rebuilt.prepared.document, rebuilt.prepared.params.ArtifactID, rebuilt.prepared.params.SHA256, string(paramsPin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, parent.policy.body).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, rebuilt.shell, rebuilt.key, parent.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != rebuilt.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
