package semanticproposal

import (
	"context"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Caller must roll back every error. Persists only blocked metadata under all
// original source/intent/policy/model and canonical parser dependency locks.
func (s SQLTransferSourceReader) beginNativeSkinDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeSkinDraftPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeSkinPlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	owned, err := s.compileNativeSkinDraftPlan(ctx, who, plan.parser, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal {
		return zero, v2input.ErrConflict
	}
	parent := owned.parser.detector
	if err := LockInventoryInTransaction(ctx, tx, who, parent.request, parent.published.Inventory); err != nil {
		return zero, err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, parent.request, parent.published); err != nil {
		return zero, err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, parent.request, parent.anchor); err != nil {
		return zero, err
	}
	selected, err := selectQualificationPolicies(parent.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	if err := s.lockSourceDetectorPreparation(ctx, tx, who, parent.request.TenantID, parent.request.Intent.Reference, color, parent.prepared, selected["face.detect"]); err != nil {
		return zero, err
	}
	_, reportPin, err := marshalNativeFaceDraftPins(ctx, owned.parser)
	if err != nil {
		return zero, err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_face_parse_qualification($1,$2,$3,$4,$5,$6)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, owned.parser.prepared.expected, string(reportPin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	if err := s.lockNativeSkinDependencyInTransaction(ctx, tx, who, owned); err != nil {
		return zero, err
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
	b := parent.request.Intent.Reference
	p := owned.prepared.params
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, p.document, p.ref.ArtifactID, p.ref.SHA256, string(pin)).Scan(&ready); err != nil {
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
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, owned.shell, owned.key, parent.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != owned.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}
