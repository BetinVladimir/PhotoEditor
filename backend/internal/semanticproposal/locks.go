package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// LockInventoryInTransaction repeats scope under locks and rejects drift from
// prepared targets. Caller owns rollback; no object IO may follow in this TX.
func LockInventoryInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, expected CatalogInventory) error {
	if tx == nil || who.UserID != request.ActorID || expected.Scope.ItemCount < 1 || expected.Scope.ItemCount > 5000 || len(expected.Targets) != expected.Scope.ItemCount {
		return v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(request.Intent)
	if err != nil {
		return err
	}
	input, err := v2input.DecodeSemanticEditIntent(raw)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", request.TenantID.String()); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT * FROM photo.lock_semantic_proposal_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", request.TenantID, who.UserID, who.SessionID, input.Reference.AssetVersionID, input.Reference.Source.ArtifactID, input.Reference.Source.SHA256, input.Reference.CanvasHash, input.ReferenceMask.ArtifactID, input.ReferenceMask.SHA256, input.TargetSnapshotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var scope CatalogScope
		var ordinal int
		var binding v2input.SessionVersionBinding
		if err := rows.Scan(&scope.ProjectID, &scope.ItemCount, &ordinal, &binding.AssetVersionID, &binding.Source.ArtifactID, &binding.Source.SHA256, &binding.CanvasHash); err != nil {
			return err
		}
		if scope != expected.Scope || ordinal != count || count >= len(expected.Targets) || binding != expected.Targets[count] {
			return v2input.ErrConflict
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if count != len(expected.Targets) {
		return v2input.ErrConflict
	}
	return nil
}

// LockPublishedIntentInTransaction must follow LockInventoryInTransaction in
// the same scheduler transaction. It binds the ready document to run and bytes.
func LockPublishedIntentInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, published PublishedIntent) error {
	if tx == nil || who.UserID != request.ActorID {
		return v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request.Intent)
	if err != nil {
		return err
	}
	sha, err := canonical.Hash(request.Intent)
	if err != nil || sha != published.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_document($1,$2,$3,$4,$5,$6,$7,$8,$9)", request.TenantID, published.Inventory.Scope.ProjectID, who.UserID, who.SessionID, request.RunID, request.Intent.Reference.AssetVersionID, published.Ref.ArtifactID, published.Ref.SHA256, len(body)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

// LockQualificationInTransaction follows inventory/document locks; trusted
// report and calibration bytes must have been verified before compilation.
func LockQualificationInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan WorkflowPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.tenant != request.TenantID || plan.actor != who.UserID || who.SessionID == uuid.Nil || tx == nil || who.UserID != request.ActorID || plan.RunID() != request.RunID || plan.ProjectID() == uuid.Nil || plan.RequestSHA() != request.RequestSHA {
		return v2input.ErrInvalid
	}
	var config v2input.SemanticDraftConfig
	if json.Unmarshal(plan.ConfigJSON(), &config) != nil || config.Semantic.Qualification == nil || config.ProjectID != plan.ProjectID() || config.Semantic.AuthSessionID != who.SessionID {
		return v2input.ErrInvalid
	}
	if plan.transferQualificationRequired && plan.transferQualification == nil {
		return v2input.ErrInvalid
	}
	if plan.transferQualification != nil {
		proof := *plan.transferQualification
		if proof.pin != *config.Semantic.Qualification {
			return v2input.ErrInvalid
		}
		var expected v2qualification.Expected
		if json.Unmarshal([]byte(proof.expected), &expected) != nil {
			return v2input.ErrInvalid
		}
		reader := SQLTransferSourceReader{ProjectID: plan.ProjectID(), Bucket: proof.bucket}
		return reader.lockTransferQualificationPreparation(ctx, tx, who, request.TenantID, proof, expected)
	}
	pin, err := canonical.Marshal(config.Semantic.Qualification)
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification($1,$2,$3,$4,$5)", request.TenantID, plan.ProjectID(), who.UserID, who.SessionID, json.RawMessage(pin)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

// LockSnapshotInTransaction pins the published snapshot and current recipe heads
// after inventory locks. No object IO is performed while holding the transaction.
func LockSnapshotInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan WorkflowPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil || plan.tenant != request.TenantID || plan.actor != who.UserID || plan.RunID() != request.RunID || plan.RequestSHA() != request.RequestSHA {
		return v2input.ErrInvalid
	}
	snapshot, err := v2input.DecodePinnedSessionInputSnapshot(plan.snapshotRef, plan.snapshotBody)
	if err != nil || snapshot.SnapshotID != plan.SnapshotID() || snapshot.SnapshotID != request.Intent.TargetSnapshotID {
		return v2input.ErrInvalid
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_snapshot($1,$2,$3,$4,$5,$6,$7,$8,$9)", request.TenantID, plan.ProjectID(), who.UserID, who.SessionID, request.RunID, snapshot.SnapshotID, plan.snapshotRef.ArtifactID, plan.snapshotRef.SHA256, string(plan.snapshotBody)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return nil
}

// LockProtectionInTransaction retains canonical producer/array authority until
// commit. Only plans compiled from exact documents contain these private proofs.
func LockProtectionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, plan WorkflowPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || who.UserID != request.ActorID || plan.tenant != request.TenantID || plan.RunID() != request.RunID || len(plan.protection) != 2*len(plan.tasks) || len(plan.tasks) == 0 {
		return v2input.ErrInvalid
	}
	if err := validateNativeTransferPlanScope(who, request, plan); err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	if plan.nativeReferenceRequired {
		native := plan.nativeReference
		reader := SQLTransferSourceReader{ProjectID: plan.project, Bucket: native.source.bucket}
		if err := reader.lockNativeSourceContinuation(ctx, tx, who, request, *native, TransferColorProfile{sha: native.source.iccSHA}); err != nil {
			return err
		}
	}
	seen := map[protectionProof]bool{}
	for ordinal, proof := range plan.protection {
		if plan.nativeReferenceRequired && ordinal%2 == 0 {
			continue
		}
		if seen[proof] {
			continue
		}
		seen[proof] = true
		var ready bool
		err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_protection($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", request.TenantID, plan.ProjectID(), who.UserID, who.SessionID, proof.Binding.AssetVersionID, proof.Binding.Source.ArtifactID, proof.Binding.Source.SHA256, proof.Root.ArtifactID, proof.Root.SHA256, proof.Child.ArtifactID, proof.Child.SHA256).Scan(&ready)
		if err != nil {
			return err
		}
		if !ready {
			return v2input.ErrConflict
		}
	}
	return nil
}
