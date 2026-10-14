package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Publish only after the blocked child draft commits. No SQL transaction may
// span object IO; final registration must repeat original admission authority.
func publishNativeFaceDraftTransport(ctx context.Context, plan nativeFaceDraftPlan, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var zero v2input.PreparedSemanticTask
	var noProof defectRegistrationProof
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return zero, noProof, err
	}
	if publisher == nil || len(plan.shell.Tasks) != 1 || len(plan.shell.Edges) != 0 {
		return zero, noProof, v2input.ErrInvalid
	}
	run, workflow, err := nativeFacePreparationIDs(plan.detector.request.RunID, plan.prepared.operation)
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
	var prepared v2input.PreparedSemanticTask
	switch frozen := plan.prepared.frozen.(type) {
	case v2input.FrozenSemanticLandmarkTask:
		if plan.prepared.operation != "face.landmarks" {
			return zero, noProof, v2input.ErrInvalid
		}
		prepared, err = v2input.PublishSemanticLandmarkTask(ctx, frozen, publisher)
	case v2input.FrozenSemanticFaceParseTask:
		if plan.prepared.operation != "face.parse" {
			return zero, noProof, v2input.ErrInvalid
		}
		prepared, err = v2input.PublishSemanticFaceParseTask(ctx, frozen, publisher)
	default:
		return zero, noProof, v2input.ErrInvalid
	}
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
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return zero, noProof, err
	}
	return prepared, proof, nil
}

// Complete bounded evidence for the future composed SQL registration barrier.
// It is not permission to insert, repair, seal or activate a graph.
func marshalNativeFaceRegistrationBinding(ctx context.Context, plan nativeFaceDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	if len(plan.shell.Tasks) != 1 || len(plan.shell.Edges) != 0 {
		return nil, v2input.ErrInvalid
	}
	run, workflow, err := nativeFacePreparationIDs(plan.detector.request.RunID, plan.prepared.operation)
	if err != nil || run != plan.shell.Run || workflow != plan.shell.WorkflowID {
		return nil, v2input.ErrInvalid
	}
	return marshalNativeRegistrationBinding(ctx, plan.prepared.frozen, built, plan.prepared.operation)
}
