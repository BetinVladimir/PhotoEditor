package semanticproposal

import (
	"context"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Caller owns original parent advisory lock and rolls back every error. Physical
// reads/publication precede this transaction; only blocked metadata is persisted.
func (s SQLTransferSourceReader) beginNativeTargetSkinDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, original detectorDraftPlan, plan nativeTargetSkinDraftPlan, color TransferColorProfile, parserBuilt, detectorBuilt v2input.Built) (DraftAdmission, error) {
	var zero DraftAdmission
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	owned, err := s.compileNativeTargetSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal {
		return zero, v2input.ErrConflict
	}
	parser := owned.parser
	a := parser.detector.anchor
	stage, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, a.manifest, a.entry.Ordinal, "mask.face_skin")
	if err != nil {
		return zero, err
	}
	if err := lockNativeTargetAnchorInTransaction(ctx, tx, who, original, stage); err != nil {
		return zero, err
	}
	if color.sha != parser.detector.detector.prepared.source.iccSHA {
		return zero, v2input.ErrConflict
	}
	if err := s.lockNativeTargetFaceResult(ctx, tx, who, original, parser, parserBuilt, detectorBuilt, owned.prepared.params.parser); err != nil {
		return zero, err
	}
	var completed bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_target_skin_parser_completed($1,$2,$3,$4,$5,$6,$7)", parser.shell.Tenant, s.ProjectID, who.UserID, who.SessionID, parser.shell.Run, parser.shell.Tasks[0].ID, owned.prepared.params.parser.catalog.ResultID).Scan(&completed); err != nil {
		return zero, err
	}
	if !completed {
		return zero, v2input.ErrConflict
	}
	obj := owned.prepared.paramsObject.object
	count, err := owned.prepared.frozen.ParamsByteCount()
	if err != nil {
		return zero, err
	}
	if obj.Bytes != count || obj.SHA256 != owned.prepared.params.ref.SHA256 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return zero, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType, "bucket": s.Bucket})
	if err != nil || len(pin) > 16384 {
		return zero, v2input.ErrInvalid
	}
	b := a.entry.Binding
	p := owned.prepared.params
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", owned.shell.Tenant, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.document, p.ref.ArtifactID, p.ref.SHA256, string(pin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", owned.shell.Tenant, s.ProjectID, who.UserID, who.SessionID, parser.detector.detector.policy.body).Scan(&ready); err != nil {
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
