package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Publish only after the blocked child draft commits. No SQL transaction may
// span object IO; final registration must repeat original admission authority.
func publishNativeTargetDefectDraftTransport(ctx context.Context, plan nativeTargetDefectDraftPlan, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var zero v2input.PreparedSemanticTask
	var noProof defectRegistrationProof
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, noProof, err
	}
	if publisher == nil || len(plan.shell.Tasks) != 1 || len(plan.shell.Edges) != 0 {
		return zero, noProof, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(plan.anchor.entry.Namespace, []byte("semantic.defect.prepare:reference"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil || plan.shell.Run != run || plan.shell.WorkflowID != workflow {
		return zero, noProof, v2input.ErrInvalid
	}
	task, err := plan.prepared.frozen.DraftTask()
	if err != nil {
		return zero, noProof, err
	}
	original, err := canonical.Marshal(task)
	if err != nil {
		return zero, noProof, err
	}
	declared, err := canonical.Marshal(plan.shell.Tasks[0])
	if err != nil || string(original) != string(declared) {
		return zero, noProof, v2input.ErrConflict
	}
	prepared, err := v2input.PublishSemanticDefectTask(ctx, plan.prepared.frozen, publisher)
	if err != nil {
		return zero, noProof, err
	}
	proof, err := freezeDefectRegistrationProof(ctx, prepared.Graph)
	if err != nil {
		return zero, noProof, err
	}
	if prepared.Graph.ID != task.ID || prepared.Params.ArtifactID != task.ParamsArtifactID || prepared.Params.SHA256 != task.ParamsSHA || prepared.InputDigest != task.InputDigest {
		return zero, noProof, v2input.ErrConflict
	}
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, noProof, err
	}
	return prepared, proof, nil
}

// Live target owner authority brackets object publication. A failed postcheck
// returns no proof; normal draft document lifecycle reclaims orphaned objects.
func (s SQLTransferSourceReader) publishOwnedNativeTargetDefectTransport(ctx context.Context, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, color TransferColorProfile, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var zero v2input.PreparedSemanticTask
	var noProof defectRegistrationProof
	if err := verifyNativeTargetDefectPlanSeal(ctx, plan); err != nil {
		return zero, noProof, err
	}
	a := plan.anchor
	owned, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, a.manifest, a.entry.Ordinal, "defects.propose")
	if err != nil {
		return zero, noProof, err
	}
	if owned.seal != a.seal {
		return zero, noProof, v2input.ErrConflict
	}
	p := plan.prepared
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p.preparation, p.qualification, color); err != nil {
		return zero, noProof, err
	}
	published, proof, err := publishNativeTargetDefectDraftTransport(ctx, plan, publisher)
	if err != nil {
		return zero, noProof, err
	}
	if err := s.recheckNativeTargetDefectQualification(ctx, who, p.preparation, p.qualification, color); err != nil {
		return zero, noProof, err
	}
	return published, proof, ctx.Err()
}
