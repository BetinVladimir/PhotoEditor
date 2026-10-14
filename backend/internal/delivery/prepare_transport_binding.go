package delivery

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// BindPublishedInputs rebuilds canonical transport from the owned frozen cursor
// without object IO. Supplied cursor callbacks are never invoked. The resulting
// graph owns its receipt slices and always uses the frozen inventory/direct roots.
// Physical readiness and current source authority remain SQL obligations.
func (f FrozenPrepareTask) BindPublishedInputs(ctx context.Context, graph v2input.GraphTask) (v2input.GraphTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	draft, err := f.DraftTask()
	if err != nil {
		return v2input.GraphTask{}, err
	}
 return bindPublishedClosure(ctx, draft.ID, f.closure, graph)
}

func bindPublishedClosure(ctx context.Context, task uuid.UUID, closure v2input.CaptureClosure, graph v2input.GraphTask) (v2input.GraphTask,error) {
 if err:=ctx.Err();err!=nil{return v2input.GraphTask{},err}
 if closure.Open==nil{return v2input.GraphTask{},v2input.ErrInvalid}
	if graph.ID != task || graph.Built.Count < 1 || graph.Built.Count > v2input.MaxEntries || len(graph.Built.Chunks) != (graph.Built.Count+v2input.ChunkSize-1)/v2input.ChunkSize || len(graph.Built.Chunks) > v2input.MaxChunks || !shaPattern.MatchString(graph.Built.SHA256) || len(graph.Direct) != len(closure.Direct) {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	seen := make(map[uuid.UUID]bool)
	for index, ref := range graph.Direct {
		if ref != closure.Direct[index] {
			return v2input.GraphTask{}, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	refs := append(append([]v2input.Ref(nil), graph.Built.Chunks...), graph.Built.Manifest)
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(ref.SHA256) || seen[ref.ArtifactID] {
			return v2input.GraphTask{}, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	cursor, err := closure.Open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	replay := &prepareTransportReplay{task: task, refs: refs}
	built, err := v2input.BuildInputClosure(ctx, task, cursor, replay)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	if replay.next != len(refs) || built.Count != graph.Built.Count || built.SHA256 != graph.Built.SHA256 || built.Manifest != graph.Built.Manifest {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	return v2input.GraphTask{ID: task, Built: built, Open: closure.Open, Direct: append([]v2input.Ref(nil), closure.Direct...)}, nil
}

type prepareTransportReplay struct {
	task uuid.UUID
	refs []v2input.Ref
	next int
}

func (r *prepareTransportReplay) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Ref{}, err
	}
	if task != r.task || ordinal != r.next || r.next >= len(r.refs) {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	expected := "InputClosureChunk"
	if r.next == len(r.refs)-1 {
		expected = "InputClosureManifest"
	}
	ref := r.refs[r.next]
	if contract != expected || digest(body) != ref.SHA256 {
		return v2input.Ref{}, v2input.ErrInvalid
	}
	r.next++
	return ref, nil
}
