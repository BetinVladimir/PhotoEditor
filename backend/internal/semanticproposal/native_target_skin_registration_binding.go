package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func marshalNativeTargetSkinRegistrationBinding(ctx context.Context, plan nativeTargetSkinDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := verifyNativeTargetSkinPlanSeal(ctx, plan); err != nil {
		return nil, err
	}
	frozen := plan.prepared.frozen
	task, err := frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	if task.Operation != "mask.face_skin" || task.OperationVersion != "1" || task.ResourceClass != "cpu" {
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
