package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Persists blocked metadata only. Exact original person authority is supported;
// other protected/manual input families remain independently gated.
// Caller rolls back every error and does not expose an admission ticket here.
func (s SQLTransferSourceReader) beginNativeDefectDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeDefectDraftPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeDefectDraftPlan(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal {
		return zero, v2input.ErrConflict
	}
	prepared := owned.prepared.dependencies.preparation
	if err := validateNativeDefectBlockedInputAuthority(ctx, prepared, owned.prepared.additional); err != nil {
		return zero, err
	}
	parent := prepared.landmarksPlan.detector
	if err := LockInventoryInTransaction(ctx, tx, who, parent.request, parent.published.Inventory); err != nil {
		return zero, err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, parent.request, parent.published); err != nil {
		return zero, err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, parent.request, parent.anchor); err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(parent.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	if err := s.lockSourceDetectorPreparation(ctx, tx, who, parent.request.TenantID, parent.request.Intent.Reference, color, parent.prepared, selected["face.detect"]); err != nil {
		return zero, err
	}
	if err := s.lockNativeDefectStageInTransaction(ctx, tx, who, owned.prepared.dependencies, owned.prepared.qualification); err != nil {
		return zero, err
	}
	obj := owned.prepared.params.object
	if obj.Bytes != int64(len(prepared.params.params)) || obj.SHA256 != owned.prepared.params.ref.SHA256 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return zero, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType, "bucket": s.Bucket})
	if err != nil || len(pin) > 16384 {
		return zero, v2input.ErrInvalid
	}
	b := parent.request.Intent.Reference
	document := uuid.NewSHA1(parent.request.RunID, []byte("semantic.defect.params:reference"))
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, document, owned.prepared.params.ref.ArtifactID, owned.prepared.params.ref.SHA256, string(pin)).Scan(&ready); err != nil {
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
