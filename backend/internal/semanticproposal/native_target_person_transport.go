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
func publishNativeTargetPersonTransport(ctx context.Context, plan nativeTargetPersonPlan, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var zero v2input.PreparedSemanticTask
	var noProof defectRegistrationProof
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
		return zero, noProof, err
	}
	if publisher == nil || len(plan.person.shell.Tasks) != 1 || len(plan.person.shell.Edges) != 0 {
		return zero, noProof, v2input.ErrInvalid
	}
	run, workflow, err := personPreparationIDs(plan.anchor.entry.Namespace)
	if err != nil || plan.person.shell.Run != run || plan.person.shell.WorkflowID != workflow || plan.person.prepared.parent != plan.anchor.entry.Namespace {
		return zero, noProof, v2input.ErrConflict
	}
	if err := verifyPersonStageIdentity(plan.anchor.entry.Namespace, plan.person.prepared); err != nil {
		return zero, noProof, err
	}
	if err := verifyPersonTaskQualificationBinding(plan.person.prepared.catalog.frozen, plan.person.prepared.qualification); err != nil {
		return zero, noProof, err
	}
	draft, err := plan.person.prepared.catalog.frozen.DraftTask()
	if err != nil {
		return zero, noProof, err
	}
	a, err := canonical.Marshal(draft)
	if err != nil {
		return zero, noProof, err
	}
	b, err := canonical.Marshal(plan.person.shell.Tasks[0])
	if err != nil || string(a) != string(b) {
		return zero, noProof, v2input.ErrConflict
	}
	published, err := v2input.PublishSemanticPersonTask(ctx, plan.person.prepared.catalog.frozen, publisher)
	if err != nil {
		return zero, noProof, err
	}
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
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

func targetPersonRegistrationState(ctx context.Context, tx pgx.Tx, plan nativeTargetPersonPlan, graph v2input.GraphTask) (string, error) {
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
		return "", err
	}
	if tx == nil {
		return "", v2input.ErrInvalid
	}
	task, err := plan.person.prepared.catalog.frozen.DraftTask()
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
	err = tx.QueryRow(ctx, "SELECT photo.semantic_target_person_registration_state($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)", plan.anchor.request.TenantID, plan.person.shell.Run, task.ID, task.ParamsArtifactID, task.ParamsSHA, task.InputDigest, task.ExecutionProfile, json.RawMessage(proof.Rows), json.RawMessage(proof.Manifest), json.RawMessage(proof.Chunks), json.RawMessage(proof.Direct), proof.Count, proof.EntriesSHA).Scan(&state)
	return state, err
}

// Caller owns transaction+rollback and original parent advisory lock. Repeated
// exact registration is accepted; partial/foreign state is never repaired.
func (s SQLTransferSourceReader) registerNativeTargetPersonInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, d detectorDraftPlan, plan nativeTargetPersonPlan, color TransferColorProfile, built v2input.Built) error {
	if err := verifyNativeTargetPersonPlan(ctx, plan); err != nil {
		return err
	}
	prepared, err := plan.person.prepared.catalog.frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return err
	}
	if _, err := s.beginNativeTargetPersonInTransaction(ctx, tx, who, d, plan, color); err != nil {
		return fmt.Errorf("target registration authority: %w", err)
	}
	state, err := targetPersonRegistrationState(ctx, tx, plan, prepared.Graph)
	if err != nil {
		return err
	}
	if state == "replayed" {
		return nil
	}
	if state != "missing" {
		return fmt.Errorf("target registration pre-state %s: %w", state, v2input.ErrConflict)
	}
	if err := v2input.RegisterInTransaction(ctx, tx, plan.anchor.request.TenantID, plan.person.shell.Run, prepared.Graph.ID, built, prepared.Graph.Open, prepared.Graph.Direct); err != nil {
		return err
	}
	state, err = targetPersonRegistrationState(ctx, tx, plan, prepared.Graph)
	if err != nil {
		return err
	}
	if state != "replayed" {
		return fmt.Errorf("target registration post-state %s: %w", state, v2input.ErrConflict)
	}
	return ctx.Err()
}
