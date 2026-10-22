package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// The packet is preparatory evidence, never permission to activate a graph.
// SQL must independently validate current authority and atomic admission.
func marshalDetectorRegistrationBinding(ctx context.Context, plan detectorDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyDetectorPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	return marshalNativeRegistrationBinding(ctx, plan.prepared.frozen, built, "face.detect")
}

func marshalPersonRegistrationBinding(ctx context.Context, plan personDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	return marshalNativeRegistrationBinding(ctx, plan.prepared.catalog.frozen, built, "mask.person")
}

type nativeRegistrationTask interface {
	DraftTask() (v2input.DraftTask, error)
	BindPublishedTransport(context.Context, v2input.Built) (v2input.PreparedSemanticTask, error)
}

func marshalNativeRegistrationBinding(ctx context.Context, frozen nativeRegistrationTask, built v2input.Built, operation string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if frozen == nil || (operation != "face.detect" && operation != "mask.person" && operation != "face.landmarks" && operation != "face.parse" && operation != "scene.parse" && operation != "depth.predict") {
		return nil, v2input.ErrInvalid
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	if task.Operation != operation || task.OperationVersion != "1" || task.ResourceClass != "gpu" {
		return nil, v2input.ErrInvalid
	}
	transport, err := frozen.BindPublishedTransport(ctx, built)
	if err != nil {
		return nil, err
	}
	proof, err := freezeDefectRegistrationProof(ctx, transport.Graph)
	if err != nil {
		return nil, err
	}
	if task.ID != transport.Graph.ID || task.ParamsArtifactID != transport.Params.ArtifactID || task.ParamsSHA != transport.Params.SHA256 || task.InputDigest != transport.InputDigest {
		return nil, v2input.ErrConflict
	}
	body, err := canonical.Marshal(defectRegistrationBinding{SchemaVersion: 1, TaskID: task.ID, Params: transport.Params, InputDigest: task.InputDigest, Profile: task.ExecutionProfile, Transport: proof})
	if err != nil {
		return nil, err
	}
	if len(body) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
