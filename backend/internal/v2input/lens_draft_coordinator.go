package v2input

import (
	"context"
	"sort"

	"github.com/google/uuid"
)

type LensDraftStore interface {
	Begin(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string, CompiledLensDraft) error
}

type LensDraftCoordinator struct {
	Store       LensDraftStore
	Preparation LensPreparationCoordinator
}

type PreparedLensDraft struct {
	RunID      uuid.UUID
	WorkflowID uuid.UUID
	Tasks      []PreparedLensTask
}

// Prepare compiles the whole selection before any publication or quota writes.
// The stable key fixes run/workflow/task identities across failures. Each retry
// repeats admission and resumes scoped documents and input registration; success
// leaves a draft with blocked tasks, requiring qualification-gated graph sealing.
func (c LensDraftCoordinator) Prepare(ctx context.Context, tenant, actor, project, snapshot, session uuid.UUID,
	key, policySHA string, requests []LensTaskRequest) (PreparedLensDraft, error) {
	var empty PreparedLensDraft
	if c.Store == nil || c.Preparation.Documents == nil || c.Preparation.Stage == nil || c.Preparation.Inputs == nil ||
		len(requests) < 1 || len(requests) > 5000 || !digestPattern.MatchString(policySHA) {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	run, err := RunID(tenant, actor, key)
	if err != nil {
		return empty, err
	}
	workflow, err := WorkflowID(run)
	if err != nil {
		return empty, err
	}
	ordered := append([]LensTaskRequest(nil), requests...)
	for i := range ordered {
		ordered[i].ProfileJSON = append([]byte(nil), ordered[i].ProfileJSON...)
		ordered[i].SourceJSON = append([]byte(nil), ordered[i].SourceJSON...)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Ordinal < ordered[j].Ordinal })
	frozen := make([]FrozenLensTask, len(ordered))
	seen := map[uuid.UUID]bool{}
	for i, request := range ordered {
		if request.Ordinal != i || request.Workflow != workflow || seen[request.Params.Base.AssetVersionID] {
			return empty, ErrInvalid
		}
		seen[request.Params.Base.AssetVersionID] = true
		frozen[i], err = CompileLensTask(request)
		if err != nil {
			return empty, err
		}
	}
	draft, err := CompileLensDraftConfig(project, snapshot, session, run, frozen)
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	if err = c.Store.Begin(ctx, tenant, actor, run, policySHA, draft); err != nil {
		return empty, err
	}
	result := PreparedLensDraft{RunID: run, WorkflowID: workflow, Tasks: make([]PreparedLensTask, 0, len(ordered))}
	for _, request := range ordered {
		prepared, err := c.Preparation.Resume(ctx, tenant, run, request)
		if err != nil {
			return empty, err
		}
		result.Tasks = append(result.Tasks, prepared)
	}
	return result, nil
}
