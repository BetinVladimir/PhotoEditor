package v2render

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type RenderRunNode struct {
	TaskID        uuid.UUID       `json:"task_id"`
	Params        v2input.Ref     `json:"params"`
	ParamsJCS     string          `json:"params_jcs"`
	SourceJCS     string          `json:"source_jcs"`
	ProtectionJCS string          `json:"protection_jcs,omitempty"`
	LayerJCS      string          `json:"layer_jcs,omitempty"`
	InputDigest   string          `json:"input_digest"`
	Profile       json.RawMessage `json:"profile"`
}

type CompiledRenderRun struct {
	Workflow    uuid.UUID
	Config      json.RawMessage
	ConfigSHA   string
	OutputBytes int64
	Tasks       []v2input.DraftTask
	Inventories []v2input.CursorFactory
}

// CompileRenderRunPlan assembles immutable preview-run evidence after scoped
// resolution. SQL must recheck project/source/model authority and quota before
// task staging; this function neither begins a run nor activates its graph.
func CompileRenderRunPlan(run, project, snapshot uuid.UUID, requests []RenderDraftRequest, resolved [][]v2input.Entry) (CompiledRenderRun, error) {
	invalid := func() (CompiledRenderRun, error) { return CompiledRenderRun{}, v2input.ErrInvalid }
	if run == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || len(requests) < 1 || len(requests) > 5000 || len(resolved) != len(requests) {
		return invalid()
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return invalid()
	}
	owner := requests[0]
	result := CompiledRenderRun{Workflow: workflow, Tasks: make([]v2input.DraftTask, 0, len(requests)), Inventories: make([]v2input.CursorFactory, 0, len(requests))}
	nodes := make([]RenderRunNode, 0, len(requests))
	for i, request := range requests {
		if request.Tenant != owner.Tenant || request.Actor != owner.Actor {
			return invalid()
		}
		draft, open, err := CompileRenderDraft(workflow, i, request, resolved[i])
		if err != nil {
			return CompiledRenderRun{}, err
		}
		evidence, err := CompileRenderEvidence(request.Binding, request.RecipeRef, request.RecipeRaw, request.SourceRaw)
		if err != nil {
			return CompiledRenderRun{}, err
		}
		if err := ValidateRenderProtectionEvidence(evidence, request.ProtectionRaw); err != nil {
			return CompiledRenderRun{}, err
		}
		var profile struct {
			Resources LayerResourceEstimate `json:"resource_estimate"`
		}
		if json.Unmarshal(draft.ExecutionProfile, &profile) != nil || profile.Resources.OutputBytes < 1 || result.OutputBytes > 9007199254740991-profile.Resources.OutputBytes {
			return invalid()
		}
		result.OutputBytes += profile.Resources.OutputBytes
		nodes = append(nodes, RenderRunNode{TaskID: draft.ID, Params: request.RecipeRef, ParamsJCS: string(request.RecipeRaw), SourceJCS: string(request.SourceRaw), LayerJCS: string(request.LayerRaw), ProtectionJCS: string(request.ProtectionRaw), InputDigest: draft.InputDigest, Profile: append(json.RawMessage(nil), draft.ExecutionProfile...)})
		result.Tasks = append(result.Tasks, draft)
		result.Inventories = append(result.Inventories, open)
	}
	config, err := canonical.Marshal(struct {
		Project  uuid.UUID `json:"project_id"`
		Snapshot uuid.UUID `json:"input_snapshot_id"`
		Render   struct {
			Session uuid.UUID       `json:"auth_session_id"`
			Nodes   []RenderRunNode `json:"nodes"`
		} `json:"v26_render"`
	}{Project: project, Snapshot: snapshot, Render: struct {
		Session uuid.UUID       `json:"auth_session_id"`
		Nodes   []RenderRunNode `json:"nodes"`
	}{owner.Actor.SessionID, nodes}})
	if err != nil {
		return CompiledRenderRun{}, err
	}
	// Persistence and recovery accept the same bounded immutable document.
	// Reject before beginning a run or publishing any transport objects.
	if len(config) > 1048576 {
		return invalid()
	}
	digest, err := canonical.Hash(json.RawMessage(config))
	if err != nil {
		return CompiledRenderRun{}, err
	}
	result.Config = config
	result.ConfigSHA = digest
	return result, nil
}
