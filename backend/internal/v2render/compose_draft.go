package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// CompileCompositionDraft freezes the blocked task shell and its immutable
// inventory before transport publication. The caller must have verified parent
// bytes and obtained the complete inventory from scoped closure resolution.
// SQL shell creation/admission must independently revalidate that provenance.
// This pure function does not publish objects, register a task or enqueue it.
func CompileCompositionDraft(workflow uuid.UUID, ordinal int, request CompositionPreparation, resolved []v2input.Entry) (v2input.DraftTask, FrozenCompositionInputs, error) {
	key, err := v2input.NodeKey("layer.compose", ordinal)
	if err != nil {
		return v2input.DraftTask{}, FrozenCompositionInputs{}, err
	}
	task, err := v2input.TaskID(workflow, "layer.compose", ordinal)
	if err != nil || task != request.Task || request.Tenant == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil {
		return v2input.DraftTask{}, FrozenCompositionInputs{}, v2input.ErrInvalid
	}
	compiled, err := CompileComposition(request.Layer, request.Base, request.SourceResult, request.ProposalResult, request.ProtectedMask, request.Width, request.Height)
	if err != nil {
		return v2input.DraftTask{}, FrozenCompositionInputs{}, err
	}
	frozen, err := FreezeCompositionInputs(task, compiled, request.ParamsRef, resolved)
	if err != nil {
		return v2input.DraftTask{}, FrozenCompositionInputs{}, err
	}
	profile, err := canonical.Marshal(struct {
		Backend   string                `json:"backend"`
		Renderer  string                `json:"renderer_version"`
		Resources LayerResourceEstimate `json:"resource_estimate"`
	}{"cpu", "v2.2", compiled.Budget.Resources})
	if err != nil {
		return v2input.DraftTask{}, FrozenCompositionInputs{}, err
	}
	return v2input.DraftTask{ID: task, NodeKey: key, Operation: "layer.compose", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: request.ParamsRef.ArtifactID, ParamsSHA: request.ParamsRef.SHA256, InputDigest: frozen.InputDigest, ExecutionProfile: json.RawMessage(profile)}, frozen, nil
}
