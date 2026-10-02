package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// PublishCompositionInputs runs after the blocked shell commits. It snapshots
// the frozen cursor once, verifies its task-bound digest before object writes,
// and owns the graph's retry inventory. SQL must still authorize registration.
func PublishCompositionInputs(ctx context.Context, task uuid.UUID, frozen FrozenCompositionInputs, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if task == uuid.Nil || frozen.Open == nil || publisher == nil || len(frozen.Direct) != 1 || !validLayerArtifactRef(frozen.Direct[0]) {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	params := frozen.Direct[0]
	cursor, err := frozen.Open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	if cursor == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	entries := []v2input.Entry{}
	paramsFound := false
	for {
		entry, ok, err := cursor.Next(ctx)
		if err != nil {
			return v2input.GraphTask{}, err
		}
		if !ok {
			break
		}
		if len(entries) >= 512 || !validLayerArtifactRef(entry.Ref) || len(entry.Contract) < 1 || len(entry.Contract) > 120 || len(entries) > 0 && bytes.Compare(entries[len(entries)-1].Ref.ArtifactID[:], entry.Ref.ArtifactID[:]) >= 0 {
			return v2input.GraphTask{}, v2input.ErrInvalid
		}
		if entry.Ref == params && entry.Contract == "Params" {
			paramsFound = true
		}
		entries = append(entries, entry)
	}
	digest, err := compositionInputDigest(task, entries)
	if err != nil || len(entries) < 2 || !paramsFound || digest != frozen.InputDigest {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &compositionCursor{entries: entries}, nil
	}
	built, err := v2input.BuildInputClosure(ctx, task, &compositionCursor{entries: entries}, publisher)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	transport := map[uuid.UUID]bool{built.Manifest.ArtifactID: true}
	for _, chunk := range built.Chunks {
		transport[chunk.ArtifactID] = true
	}
	for _, entry := range entries {
		if transport[entry.Ref.ArtifactID] {
			return v2input.GraphTask{}, v2input.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	return v2input.GraphTask{ID: task, Built: built, Open: open, Direct: []v2input.Ref{params}}, nil
}
