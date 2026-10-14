package semanticproposal

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Caller holds the original parent lock. No object IO occurs in this transaction.
// SQL independently checks original run, current authorization, frozen inventory,
// ready documents/recipe heads and every expected DAG identity before writing.
func (p TransferDraftPreparer) checkpointNativeTargetPreparation(ctx context.Context, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest) (nativeTargetPreparationManifest, error) {
	var zero nativeTargetPreparationManifest
	if err := verifyDetectorPlanSeal(ctx, d); err != nil {
		return zero, err
	}
	if p.Scheduler == nil {
		return zero, v2input.ErrInvalid
	}
	if err := verifyNativeTargetPreparationManifest(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest); err != nil {
		return zero, err
	}
	packet := nativeAdmissionEvidence{SchemaVersion: 1, Operation: "face.detect", Tenant: d.request.TenantID, Project: d.published.Inventory.Scope.ProjectID, Actor: who.UserID, Session: who.SessionID, Run: d.shell.Run, ParentRun: d.request.RunID, RequestSHA: d.request.RequestSHA, ConfigText: string(d.shell.Config), Anchor: defectAdmissionAnchor{IntentText: string(d.request.Body), SnapshotText: string(d.anchor.snapshotBody)}, Binding: d.request.Intent.Reference}
	evidence, err := canonical.Marshal(packet)
	if err != nil {
		return zero, err
	}
	tx, err := p.Scheduler.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", d.request.TenantID.String()); err != nil {
		return zero, err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, "SELECT photo.save_v27_target_manifest($1,$2,$3,$4)", d.request.TenantID, d.shell.Run, json.RawMessage(evidence), string(manifest.body)).Scan(&raw); err != nil {
		return zero, err
	}
	var saved struct {
		Body string `json:"body"`
		SHA  string `json:"sha"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &saved) != nil {
		return zero, v2input.ErrConflict
	}
	restored, err := restoreNativeTargetPreparationManifest(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, json.RawMessage(saved.Body), saved.SHA)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, err
	}
	return restored, nil
}
