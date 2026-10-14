package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// exposureReplayPublisher performs no IO. BuildInputClosure independently
// recomputes document bytes and verifies each provided SHA against those bytes.
type exposureReplayPublisher struct {
	task uuid.UUID
	refs []v2input.Ref
	next int
}

func (p *exposureReplayPublisher) Publish(_ context.Context, task uuid.UUID, ordinal int, contract string, _ []byte) (v2input.Ref, error) {
	if task != p.task || ordinal != p.next || p.next >= len(p.refs) {
		return v2input.Ref{}, ErrInvalid
	}
	expected := "InputClosureChunk"
	if p.next == len(p.refs)-1 {
		expected = "InputClosureManifest"
	}
	if contract != expected {
		return v2input.Ref{}, ErrInvalid
	}
	ref := p.refs[p.next]
	p.next++
	return ref, nil
}

// VerifyExposurePublication rebuilds transport from the original frozen
// compiler, without reading a caller-provided cursor or writing any objects.
// SQL must still prove that these exact transport artifacts are ready and
// belong to the admitted task. Returned cursors/bytes are compiler-owned.
func VerifyExposurePublication(ctx context.Context, frozen FrozenExposureTask, published PreparedExposureTask) (PreparedExposureTask, error) {
	var empty PreparedExposureTask
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	draft, err := frozen.DraftTask()
	if err != nil {
		return empty, err
	}
	expectedDraft, err := canonical.Marshal(draft)
	if err != nil {
		return empty, ErrInvalid
	}
	actualDraft, err := canonical.Marshal(published.Draft)
	if err != nil || !bytes.Equal(expectedDraft, actualDraft) || published.Graph.ID != frozen.task || published.Params != frozen.params ||
		!bytes.Equal(published.ParamsJCS, frozen.closure.ParamsJCS) || published.Graph.Built.Count != len(frozen.closure.Direct) ||
		len(published.Graph.Direct) != len(frozen.closure.Direct) || len(published.Graph.Built.Chunks) != (len(frozen.closure.Direct)+v2input.ChunkSize-1)/v2input.ChunkSize {
		return empty, ErrInvalid
	}
	for i, ref := range frozen.closure.Direct {
		if published.Graph.Direct[i] != ref {
			return empty, ErrInvalid
		}
	}
	refs := append([]v2input.Ref(nil), published.Graph.Built.Chunks...)
	refs = append(refs, published.Graph.Built.Manifest)
	seen := make(map[uuid.UUID]bool, len(refs)+len(frozen.closure.Direct))
	for _, ref := range frozen.closure.Direct {
		seen[ref.ArtifactID] = true
	}
	for _, ref := range refs {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] || !shaPattern.MatchString(ref.SHA256) {
			return empty, ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	cursor, err := frozen.closure.Open(ctx)
	if err != nil {
		return empty, err
	}
	replay := &exposureReplayPublisher{task: frozen.task, refs: refs}
	built, err := v2input.BuildInputClosure(ctx, frozen.task, cursor, replay)
	if err != nil {
		return empty, err
	}
	if replay.next != len(refs) || built.SHA256 != published.Graph.Built.SHA256 {
		return empty, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PreparedExposureTask{Graph: v2input.GraphTask{ID: frozen.task, Built: built, Open: frozen.closure.Open, Direct: append([]v2input.Ref(nil), frozen.closure.Direct...)},
		Draft: draft, Params: frozen.params, ParamsJCS: append(json.RawMessage(nil), frozen.closure.ParamsJCS...)}, nil
}
