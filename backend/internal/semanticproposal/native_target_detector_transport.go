package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Object IO only, after blocked draft commit. The target fingerprint/namespace
// stays distinct from reference-stage plans; publication grants no activation.
func publishNativeTargetDetectorTransport(ctx context.Context, plan nativeTargetDetectorPlan, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var zero v2input.PreparedSemanticTask
	var noProof defectRegistrationProof
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, noProof, err
	}
	if publisher == nil || len(plan.detector.shell.Tasks) != 1 || len(plan.detector.shell.Edges) != 0 {
		return zero, noProof, v2input.ErrInvalid
	}
	run, workflow, err := detectorPreparationIDs(plan.anchor.entry.Namespace)
	if err != nil || plan.detector.shell.Run != run || plan.detector.shell.WorkflowID != workflow || plan.detector.prepared.parent != plan.anchor.entry.Namespace {
		return zero, noProof, v2input.ErrConflict
	}
	if err := verifyDetectorStageIdentity(plan.anchor.entry.Namespace, plan.detector.prepared); err != nil {
		return zero, noProof, err
	}
	if err := verifyDetectorTaskQualificationBinding(plan.detector.prepared.frozen, plan.detector.prepared.qualification); err != nil {
		return zero, noProof, err
	}
	draft, err := plan.detector.prepared.frozen.DraftTask()
	if err != nil {
		return zero, noProof, err
	}
	a, err := canonical.Marshal(draft)
	if err != nil {
		return zero, noProof, err
	}
	b, err := canonical.Marshal(plan.detector.shell.Tasks[0])
	if err != nil || string(a) != string(b) {
		return zero, noProof, v2input.ErrConflict
	}
	published, err := v2input.PublishSemanticDetectorTask(ctx, plan.detector.prepared.frozen, publisher)
	if err != nil {
		return zero, noProof, err
	}
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return zero, noProof, err
	}
	proof, err := freezeDefectRegistrationProof(ctx, published.Graph)
	if err != nil {
		return zero, noProof, err
	}
	if err := ctx.Err(); err != nil {
		return zero, noProof, err
	}
	return published, proof, nil
}

func targetDetectorRegistrationState(ctx context.Context, tx pgx.Tx, plan nativeTargetDetectorPlan, graph v2input.GraphTask) (string, error) {
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return "", err
	}
	if tx == nil {
		return "", v2input.ErrInvalid
	}
	task, err := plan.detector.prepared.frozen.DraftTask()
	if err != nil {
		return "", err
	}
	if graph.ID != task.ID {
		return "", v2input.ErrConflict
	}
	proof, err := freezeDefectRegistrationProof(ctx, graph)
	if err != nil {
		return "", err
	}
	var state string
	err = tx.QueryRow(ctx, "SELECT photo.semantic_target_detector_registration_state($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", plan.anchor.request.TenantID, plan.detector.shell.Run, task.ID, task.ParamsArtifactID, task.ParamsSHA, task.InputDigest, task.ExecutionProfile, json.RawMessage(proof.Rows), json.RawMessage(proof.Manifest), json.RawMessage(proof.Chunks), json.RawMessage(proof.Direct), proof.Count, proof.EntriesSHA).Scan(&state)
	return state, err
}

// Caller owns transaction+rollback and original parent advisory lock. Repeated
// exact registration is accepted; partial/foreign state is never repaired.
func (s SQLTransferSourceReader) registerNativeTargetDetectorInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetDetectorPlan, color TransferColorProfile, built v2input.Built) error {
	if err := verifyNativeTargetDetectorPlan(ctx, plan); err != nil {
		return err
	}
	prepared, err := plan.detector.prepared.frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return err
	}
	if _, err := s.beginNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, color); err != nil {
		return fmt.Errorf("target registration authority: %w", err)
	}
	state, err := targetDetectorRegistrationState(ctx, tx, plan, prepared.Graph)
	if err != nil {
		return err
	}
	if state == "replayed" {
		return nil
	}
	if state != "missing" {
		return fmt.Errorf("target registration pre-state %s: %w", state, v2input.ErrConflict)
	}
	if err := v2input.RegisterInTransaction(ctx, tx, plan.anchor.request.TenantID, plan.detector.shell.Run, prepared.Graph.ID, built, prepared.Graph.Open, prepared.Graph.Direct); err != nil {
		return err
	}
	state, err = targetDetectorRegistrationState(ctx, tx, plan, prepared.Graph)
	if err != nil {
		return err
	}
	if state != "replayed" {
		return fmt.Errorf("target registration post-state %s: %w", state, v2input.ErrConflict)
	}
	return ctx.Err()
}
