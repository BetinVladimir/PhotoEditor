package semanticproposal

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func marshalNativeDefectRegistrationBinding(ctx context.Context, plan nativeDefectDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := verifyNativeDefectDraftSeal(ctx, plan); err != nil {
		return nil, err
	}
	return marshalNativeDefectTaskRegistrationBinding(ctx, plan.prepared.frozen, plan.shell, built)
}

// Pure transport binding after distinct producer authority validation by caller.
func marshalNativeDefectTaskRegistrationBinding(ctx context.Context, frozen v2input.FrozenSemanticDefectTask, shell v2input.DraftShell, built v2input.Built) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	task, err := frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	if task.Operation != "defects.propose" || task.OperationVersion != "1" || task.ResourceClass != "cpu" {
		return nil, v2input.ErrInvalid
	}
	if len(shell.Tasks) != 1 || len(shell.Edges) != 0 {
		return nil, v2input.ErrInvalid
	}
	declared, err := canonical.Marshal(shell.Tasks[0])
	if err != nil {
		return nil, err
	}
	owned, err := canonical.Marshal(task)
	if err != nil {
		return nil, err
	}
	if string(declared) != string(owned) {
		return nil, v2input.ErrConflict
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
