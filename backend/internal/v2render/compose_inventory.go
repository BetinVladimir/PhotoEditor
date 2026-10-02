package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type FrozenCompositionInputs struct {
	Open        v2input.CursorFactory
	Direct      []v2input.Ref
	InputDigest string
}

// FreezeCompositionInputs consumes a complete scoped resolver inventory. It
// validates root coverage and freezes a copy for transport/admission retries.
// It does not independently prove catalog scope or discover missing descendants.
func FreezeCompositionInputs(task uuid.UUID, compiled CompiledComposition, paramsRef v2input.Ref, resolved []v2input.Entry) (FrozenCompositionInputs, error) {
	roots, err := CompositionInputRoots(compiled, paramsRef)
	if err != nil || task == uuid.Nil || len(resolved) < len(roots) || len(resolved) > 512 {
		return FrozenCompositionInputs{}, v2input.ErrInvalid
	}
	entries := append([]v2input.Entry(nil), resolved...)
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	lookup := map[uuid.UUID]v2input.Entry{}
	for _, entry := range entries {
		if !validLayerArtifactRef(entry.Ref) || len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return FrozenCompositionInputs{}, v2input.ErrInvalid
		}
		if _, exists := lookup[entry.Ref.ArtifactID]; exists {
			return FrozenCompositionInputs{}, v2input.ErrInvalid
		}
		lookup[entry.Ref.ArtifactID] = entry
	}
	for _, root := range roots {
		if lookup[root.Ref.ArtifactID] != root {
			return FrozenCompositionInputs{}, v2input.ErrInvalid
		}
	}
	digest, err := compositionInputDigest(task, entries)
	if err != nil {
		return FrozenCompositionInputs{}, err
	}
	// Params is the sole direct transport handle. All image/mask/evidence refs are
	// present in the manifest, avoiding the legacy direct-ref limit for large DAGs.
	return FrozenCompositionInputs{Open: func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &compositionCursor{entries: entries}, nil
	}, Direct: []v2input.Ref{paramsRef}, InputDigest: digest}, nil
}

func compositionInputDigest(task uuid.UUID, entries []v2input.Entry) (string, error) {
	return canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, "layer.compose", "1", entries})
}

type compositionCursor struct {
	entries []v2input.Entry
	offset  int
}

func (c *compositionCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.offset == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.offset]
	c.offset++
	return entry, true, nil
}
