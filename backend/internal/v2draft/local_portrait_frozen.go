package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// FrozenLocalPortraitTask owns the inventory and profile for deterministic retries.
// It carries no SQL authority; the durable owner and graph admission remain required.
type FrozenLocalPortraitTask struct {
	draft         v2input.DraftTask
	entries       []v2input.Entry
	params        json.RawMessage
	binding       v2input.SessionVersionBinding
	reservedBytes int64
}

func FreezeLocalPortraitTask(ctx context.Context, workflow uuid.UUID, ordinal int,
	prepared PreparedLocalPortraitTask, binding v2input.SessionVersionBinding,
	sourceRaw []byte, runtimeSHA string, capacity v2portrait.LocalResourceEstimate) (FrozenLocalPortraitTask, error) {
	var zero FrozenLocalPortraitTask
	plan, err := v2portrait.CompileLocalTaskPlan(ctx, prepared.Published.Plan.Operation, binding, sourceRaw, prepared.Published.Plan.Params, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	ref := prepared.Published.ParamsRef
	if !isDocumentRef(ref) || ref.SHA256 != plan.ParamsSHA || prepared.Published.Plan.ParamsSHA != plan.ParamsSHA ||
		!bytes.Equal(plan.Profile, prepared.Published.Plan.Profile) || plan.ReservedBytes != prepared.Published.Plan.ReservedBytes {
		return zero, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, plan.Operation, ordinal)
	if err != nil {
		return zero, err
	}
	key, _ := v2input.NodeKey(plan.Operation, ordinal)
	// Copy before sorting: the caller retains ownership of prepared evidence.
	inventory := append([]v2input.Entry(nil), prepared.Evidence.Entries...)
	entries, err := uniqueTaskInputs(inventory)
	// This local path declares its complete inventory as direct refs. V2 graph
	// admission permits at most 127 direct refs, independently of chunk capacity.
	if err != nil || len(entries) < 4 || len(entries) > 127 {
		return zero, v2input.ErrInvalid
	}
	var params struct {
		Mask      v2input.Ref `json:"mask"`
		Protected v2input.Ref `json:"protected"`
	}
	if json.Unmarshal(plan.Params, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	required := map[v2input.Ref]string{ref: "Params", binding.Source: "WorkingImage", params.Mask: "MaskRef", params.Protected: "MaskRef"}
	for _, entry := range entries {
		if !isDocumentRef(entry.Ref) || len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return zero, v2input.ErrInvalid
		}
		if contract, ok := required[entry.Ref]; ok {
			if entry.Contract != contract {
				return zero, v2input.ErrInvalid
			}
			delete(required, entry.Ref)
		}
	}
	if len(required) != 0 {
		return zero, v2input.ErrInvalid
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, plan.Operation, "1", entries})
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return FrozenLocalPortraitTask{draft: v2input.DraftTask{ID: task, NodeKey: key, Operation: plan.Operation, OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: ref.ArtifactID, ParamsSHA: ref.SHA256, InputDigest: digest, ExecutionProfile: append(json.RawMessage(nil), plan.Profile...)}, entries: entries, params: append(json.RawMessage(nil), plan.Params...), binding: binding, reservedBytes: plan.ReservedBytes}, nil
}

func (f FrozenLocalPortraitTask) DraftTask() (v2input.DraftTask, error) {
	if f.draft.ID == uuid.Nil || len(f.entries) == 0 {
		return v2input.DraftTask{}, v2input.ErrInvalid
	}
	draft := f.draft
	draft.ExecutionProfile = append(json.RawMessage(nil), draft.ExecutionProfile...)
	return draft, nil
}

type localPortraitCursor struct {
	entries []v2input.Entry
	index   int
}

func (c *localPortraitCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.index == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.index]
	c.index++
	return entry, true, nil
}

// PublishInputClosure requires a committed shell and its scoped transport publisher.
// The returned graph still requires transactional SQL registration and sealing.
func (f FrozenLocalPortraitTask) PublishInputClosure(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	if publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil {
		return v2input.GraphTask{}, err
	}
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &localPortraitCursor{entries: append([]v2input.Entry(nil), f.entries...)}, nil
	}
	cursor, err := open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	built, err := v2input.BuildInputClosure(ctx, draft.ID, cursor, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	if err = ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	refs := make([]v2input.Ref, len(f.entries))
	for i, entry := range f.entries {
		refs[i] = entry.Ref
	}
	inputs := make(map[uuid.UUID]bool, len(refs))
	for _, ref := range refs {
		inputs[ref.ArtifactID] = true
	}
	if inputs[built.Manifest.ArtifactID] {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	for _, ref := range built.Chunks {
		if inputs[ref.ArtifactID] {
			return v2input.GraphTask{}, v2input.ErrInvalid
		}
	}
	return v2input.GraphTask{ID: draft.ID, Built: built, Open: open, Direct: refs}, nil
}
