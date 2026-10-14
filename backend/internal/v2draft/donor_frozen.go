package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"reflect"
)

// FrozenDonorTask owns a deterministic plan/inventory snapshot. It carries no
// SQL grant: calibration qualification, publication provenance and admission
// must be checked by the durable assembly owner.
type FrozenDonorTask struct {
	draft         v2input.DraftTask
	entries       []v2input.Entry
	params        json.RawMessage
	reservedBytes int64
	qualification *v2portrait.LocatedDonorQualification
}

func FreezeDonorTask(ctx context.Context, workflow uuid.UUID, ordinal int, loaded LoadedDonorTaskPlan,
	prepared PreparedDonorInputEvidence, published v2portrait.PublishedDonorTaskPlan) (FrozenDonorTask, error) {
	var zero FrozenDonorTask
	if !reflect.DeepEqual(published.Plan, loaded.Plan) {
		return zero, v2input.ErrInvalid
	}
	bound, err := BindDonorParamsInventory(ctx, loaded, prepared, published.ParamsRef)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, loaded.Plan.Operation, ordinal)
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey(loaded.Plan.Operation, ordinal)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, loaded.Plan.Operation, "1", bound.Entries})
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	profile := append(json.RawMessage(nil), loaded.Plan.Profile...)
	var qualification *v2portrait.LocatedDonorQualification
	if prepared.qualification != nil {
		pin, err := verifyPreparedDonorQualification(loaded, prepared)
		if err != nil {
			return zero, err
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(profile, &fields) != nil || fields == nil {
			return zero, v2input.ErrInvalid
		}
		if _, exists := fields["donor_qualification"]; exists {
			return zero, v2input.ErrInvalid
		}
		fields["donor_qualification"], err = pin.Compile()
		if err != nil {
			return zero, err
		}
		profile, err = canonical.Marshal(fields)
		if err != nil {
			return zero, err
		}
		owned := *prepared.qualification
		qualification = &owned
	}
	return FrozenDonorTask{draft: v2input.DraftTask{ID: task, NodeKey: key, Operation: loaded.Plan.Operation, OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: published.ParamsRef.ArtifactID, ParamsSHA: published.ParamsRef.SHA256, InputDigest: digest, ExecutionProfile: profile}, entries: append([]v2input.Entry(nil), bound.Entries...), params: append(json.RawMessage(nil), loaded.Plan.Params...), reservedBytes: loaded.Plan.ReservedBytes, qualification: qualification}, nil
}

// FreezeQualifiedDonorTask is the assembly entry point requiring original
// qualification evidence. Legacy FreezeDonorTask remains a component compiler.
func FreezeQualifiedDonorTask(ctx context.Context, workflow uuid.UUID, ordinal int, loaded LoadedDonorTaskPlan, prepared PreparedDonorInputEvidence, published v2portrait.PublishedDonorTaskPlan) (FrozenDonorTask, error) {
	if _, err := verifyPreparedDonorQualification(loaded, prepared); err != nil {
		return FrozenDonorTask{}, err
	}
	return FreezeDonorTask(ctx, workflow, ordinal, loaded, prepared, published)
}

func (f FrozenDonorTask) DraftTask() (v2input.DraftTask, error) {
	if f.draft.ID == uuid.Nil || len(f.entries) == 0 {
		return v2input.DraftTask{}, v2input.ErrInvalid
	}
	draft := f.draft
	draft.ExecutionProfile = append(json.RawMessage(nil), draft.ExecutionProfile...)
	return draft, nil
}

type donorInputCursor struct {
	entries []v2input.Entry
	index   int
}

func (c *donorInputCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
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
func (f FrozenDonorTask) PublishInputClosure(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
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
		return &donorInputCursor{entries: append([]v2input.Entry(nil), f.entries...)}, nil
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
