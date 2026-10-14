package semanticproposal

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

// Persists only a blocked original manual graph after complete source, anchor,
// producer/manual, CPU qualification and accepted Params locks. Roll back errors.
func (s SQLTransferSourceReader) beginNativeInputManualDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	owned, err := s.compileNativeInputManualInitialDraft(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if owned.seal != plan.seal || !reflect.DeepEqual(owned.shell, plan.shell) {
		return zero, v2input.ErrConflict
	}
	stage := owned.prepared.stage
	deps := stage.dependencies.dependencies
	parent := deps.producers.detector
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
	if err := s.lockNativeInputManualStageInTransaction(ctx, tx, who, deps, stage.qualification); err != nil {
		return zero, err
	}
	params := stage.dependencies.params.params
	obj := params.object
	if obj.Bytes != int64(len(deps.selection.params.params)) || obj.SHA256 != params.ref.SHA256 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return zero, v2input.ErrInvalid
	}
	pin, err := canonical.Marshal(map[string]any{"key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType, "bucket": s.Bucket})
	if err != nil || len(pin) > 16384 {
		return zero, v2input.ErrInvalid
	}
	b := parent.request.Intent.Reference
	document := uuid.NewSHA1(parent.request.RunID, []byte("semantic.defect.params:reference"))
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, b.AssetVersionID, b.Source.ArtifactID, b.Source.SHA256, document, params.ref.ArtifactID, params.ref.SHA256, string(pin)).Scan(&ready); err != nil {
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

// Physical initial evidence rechecks happen before SQL. A blocked-draft outcome
// is returned only after successful commit; unknown commit outcomes expose none.
func (s SQLTransferSourceReader) beginNativeInputManualDraft(ctx context.Context, scheduler *pgxpool.Pool, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile) (DraftAdmission, error) {
	var zero, out DraftAdmission
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, err
	}
	if scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	stage := plan.prepared.stage
	q := stage.qualification.qualification
	current, err := s.prepareNativeInputManualInitialQualification(ctx, who, stage.dependencies, color, q.report.report.ref, q.costs)
	if err != nil {
		return zero, err
	}
	if !sameNativeInputManualInitialStage(ctx, current, stage) {
		return zero, v2input.ErrConflict
	}
	err = database.InTransaction(ctx, scheduler, func(tx pgx.Tx) error {
		var err error
		out, err = s.beginNativeInputManualDraftInTransaction(ctx, tx, who, plan, color)
		return err
	})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return out, nil
}
