package v2render

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type CompositionDraftRequest struct {
	Ordinal     int
	Preparation CompositionPreparation
	Resolved    []v2input.Entry
}

type CompositionDocumentPlan struct {
	TaskID      uuid.UUID       `json:"task_id"`
	Ordinal     int             `json:"ordinal"`
	Params      v2input.Ref     `json:"params"`
	ParamsJCS   string          `json:"params_jcs"`
	InputDigest string          `json:"input_digest"`
	Width       int64           `json:"width"`
	Height      int64           `json:"height"`
	Profile     json.RawMessage `json:"profile"`
}

type CompositionDraftConfig struct {
 QueueClass string `json:"queue_class,omitempty"`
	ProjectID   uuid.UUID             `json:"project_id"`
	SnapshotID  uuid.UUID             `json:"input_snapshot_id"`
	Composition CompositionDraftNodes `json:"v25_composition"`
}

type CompositionDraftNodes struct {
	AuthSessionID uuid.UUID                 `json:"auth_session_id"`
	Nodes         []CompositionDocumentPlan `json:"nodes"`
}

type CompiledCompositionDraft struct {
	Config      CompositionDraftConfig
	Body        json.RawMessage
	SHA256      string
	OutputBytes int64
	Tasks       []v2input.DraftTask
	Inputs      []FrozenCompositionInputs
}

// CompileCompositionDraftConfig freezes the document/identity/resource plan
// before shell persistence. All params must already be published and resolved
// inventories scoped by the caller. This is compilation, not authorization;
// SQL admission must independently repeat lineage, snapshot and quota checks.
func CompileCompositionDraftConfig(ctx context.Context, project, snapshot, session, run uuid.UUID, requests []CompositionDraftRequest) (CompiledCompositionDraft, error) {
	var empty CompiledCompositionDraft
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if project == uuid.Nil || snapshot == uuid.Nil || session == uuid.Nil || run == uuid.Nil || len(requests) < 1 || len(requests) > 5000 {
		return empty, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return empty, err
	}
	ordered := append([]CompositionDraftRequest(nil), requests...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	result := CompiledCompositionDraft{Config: CompositionDraftConfig{QueueClass:"batch",ProjectID: project, SnapshotID: snapshot, Composition: CompositionDraftNodes{AuthSessionID: session, Nodes: make([]CompositionDocumentPlan, 0, len(ordered))}}, Tasks: make([]v2input.DraftTask, 0, len(ordered)), Inputs: make([]FrozenCompositionInputs, 0, len(ordered))}
	tenant, actor := ordered[0].Preparation.Tenant, ordered[0].Preparation.Actor.UserID
	for i, item := range ordered {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		request := item.Preparation
		if item.Ordinal != i || request.Tenant != tenant || request.Actor.UserID != actor || request.Actor.SessionID != session {
			return empty, v2input.ErrInvalid
		}
		draft, frozen, err := CompileCompositionDraft(workflow, item.Ordinal, request, item.Resolved)
		if err != nil {
			return empty, err
		}
		compiled, err := CompileComposition(request.Layer, request.Base, request.SourceResult, request.ProposalResult, request.ProtectedMask, request.Width, request.Height)
		if err != nil {
			return empty, err
		}
		output := compiled.Budget.Resources.OutputBytes
		if output < 1 || result.OutputBytes > 9007199254740991-output {
			return empty, v2input.ErrInvalid
		}
		result.OutputBytes += output
		result.Config.Composition.Nodes = append(result.Config.Composition.Nodes, CompositionDocumentPlan{TaskID: draft.ID, Ordinal: item.Ordinal, Params: request.ParamsRef, ParamsJCS: string(compiled.JSON), InputDigest: draft.InputDigest, Width: request.Width, Height: request.Height, Profile: append(json.RawMessage(nil), draft.ExecutionProfile...)})
		result.Tasks = append(result.Tasks, draft)
		result.Inputs = append(result.Inputs, frozen)
	}
	body, err := canonical.Marshal(result.Config)
	if err != nil || len(body) > 1048576 {
		return empty, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(result.Config)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	result.Body = body
	result.SHA256 = sha
	return result, nil
}

// CompilePinnedCompositionDraftConfig binds an operator-qualified build digest
// into every task and the hashed frozen plan. Qualification is checked separately.
func CompilePinnedCompositionDraftConfig(ctx context.Context, project, snapshot, session, run uuid.UUID, requests []CompositionDraftRequest, runtimeSHA string) (CompiledCompositionDraft, error) {
	var empty CompiledCompositionDraft
	if len(runtimeSHA) != 64 {
		return empty, v2input.ErrInvalid
	}
	for _, c := range runtimeSHA {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return empty, v2input.ErrInvalid
		}
	}
	result, err := CompileCompositionDraftConfig(ctx, project, snapshot, session, run, requests)
	if err != nil {
		return empty, err
	}
	for i := range result.Tasks {
		var profile map[string]any
		if err = json.Unmarshal(result.Tasks[i].ExecutionProfile, &profile); err != nil {
			return empty, err
		}
		profile["layer_runtime_sha256"] = runtimeSHA
		body, e := canonical.Marshal(profile)
		if e != nil {
			return empty, e
		}
		result.Tasks[i].ExecutionProfile = body
		result.Config.Composition.Nodes[i].Profile = append(json.RawMessage(nil), body...)
	}
	result.Body, err = canonical.Marshal(result.Config)
	if err != nil || len(result.Body) > 1048576 {
		return empty, v2input.ErrInvalid
	}
	result.SHA256, err = canonical.Hash(result.Config)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}

// CompileCompositionDraftConfigWithAlpha freezes an explicit alpha policy.
// The worker must implement that policy before a task can be activated.
func CompileCompositionDraftConfigWithAlpha(ctx context.Context, project, snapshot, session, run uuid.UUID, requests []CompositionDraftRequest, runtimeSHA, alphaMode string) (CompiledCompositionDraft, error) {
	var empty CompiledCompositionDraft
	if alphaMode != "require_float32" && alphaMode != "normalize_uint16" {
		return empty, v2input.ErrInvalid
	}
	result, err := CompilePinnedCompositionDraftConfig(ctx, project, snapshot, session, run, requests, runtimeSHA)
	if err != nil {
		return empty, err
	}
	for i := range result.Tasks {
		var profile map[string]any
		if err = json.Unmarshal(result.Tasks[i].ExecutionProfile, &profile); err != nil {
			return empty, err
		}
		profile["layer_alpha_mode"] = alphaMode
		if alphaMode == "normalize_uint16" {
			estimate, ok := profile["resource_estimate"].(map[string]any)
			if !ok {
				return empty, v2input.ErrInvalid
			}
			extra := 8*result.Config.Composition.Nodes[i].Width*result.Config.Composition.Nodes[i].Height + 8192
			for _, field := range []string{"ram_bytes", "scratch_bytes"} {
				value, ok := estimate[field].(float64)
				if !ok || value < 0 || value > float64(9007199254740991-extra) {
					return empty, v2input.ErrInvalid
				}
				estimate[field] = int64(value) + extra
			}
		}
		body, e := canonical.Marshal(profile)
		if e != nil {
			return empty, e
		}
		result.Tasks[i].ExecutionProfile = body
		result.Config.Composition.Nodes[i].Profile = append(json.RawMessage(nil), body...)
	}
	result.Body, err = canonical.Marshal(result.Config)
	if err != nil || len(result.Body) > 1048576 {
		return empty, v2input.ErrInvalid
	}
	result.SHA256, err = canonical.Hash(result.Config)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
