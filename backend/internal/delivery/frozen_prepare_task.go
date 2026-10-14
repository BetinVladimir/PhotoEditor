package delivery

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// FrozenPrepareTask binds one shell definition to one owned closure. Freeze
// after authorized dependency resolution, then reuse this value through shell
// commit and transport publication; do not resolve mutable inputs a second time.
type FrozenPrepareTask struct {
	draft   v2input.DraftTask
	closure v2input.CaptureClosure
}

func (p PreparePlan) FreezeTask(resolved []v2input.Entry, limits PrepareResourceLimits) (FrozenPrepareTask, error) {
	closure, err := p.InputClosure(resolved)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	// Direct transport roots are deliberately bounded to Params + frozen input.
	// Memory/scratch must account for the entire indexed inventory instead.
	unique := map[[16]byte]struct{}{p.params.ArtifactID: {}}
	for _, entry := range resolved {
		unique[entry.Ref.ArtifactID] = struct{}{}
	}
	profile, err := p.prepareProfile(len(unique), limits)
	if err != nil {
		return FrozenPrepareTask{}, err
	}
	draft := v2input.DraftTask{ID: p.task, NodeKey: p.key, Operation: "v2.delivery.prepare", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: p.params.ArtifactID, ParamsSHA: p.params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: profile}
	return FrozenPrepareTask{draft: draft, closure: closure}, nil
}

func (f FrozenPrepareTask) DraftTask() (v2input.DraftTask, error) {
	if f.closure.Open == nil || f.draft.InputDigest != f.closure.InputDigest || len(f.draft.ExecutionProfile) == 0 {
		return v2input.DraftTask{}, v2input.ErrInvalid
	}
	draft := f.draft
	draft.ExecutionProfile = append([]byte(nil), draft.ExecutionProfile...)
	return draft, nil
}

// PublishInputs is transport-only. The caller must first commit this exact
// DraftTask, then register/seal this GraphTask under current SQL authority.
func (f FrozenPrepareTask) PublishInputs(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	draft, err := f.DraftTask()
	if err != nil {
		return v2input.GraphTask{}, err
	}
	if publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	cursor, err := f.closure.Open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	built, err := v2input.BuildInputClosure(ctx, draft.ID, cursor, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	return v2input.GraphTask{ID: draft.ID, Built: built, Open: f.closure.Open, Direct: append([]v2input.Ref(nil), f.closure.Direct...)}, nil
}
