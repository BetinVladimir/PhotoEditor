package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type defectRegistrationBinding struct {
	SchemaVersion int                     `json:"schema_version"`
	TaskID        uuid.UUID               `json:"task_id"`
	Params        v2input.Ref             `json:"params"`
	InputDigest   string                  `json:"input_digest"`
	Profile       json.RawMessage         `json:"profile"`
	Transport     defectRegistrationProof `json:"transport"`
}

// Captures only the privately frozen task and its exact published transport.
// Caller cannot supply a substitute profile, input cursor, Params or task ID.
// Catalog authority and actual SQL registration remain separate requirements.
func marshalDefectRegistrationBinding(ctx context.Context, plan defectPreparationDraftPlan, built v2input.Built) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	task, err := plan.frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	transport, err := plan.frozen.BindPublishedTransport(ctx, built)
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
	if len(body) > 4194304 {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return body, nil
}
