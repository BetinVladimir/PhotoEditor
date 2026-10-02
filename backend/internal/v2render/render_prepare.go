package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// PrepareRenderInputClosure publishes transport only after full scoped resolution.
// The caller must commit its task shell before calling this object-I/O boundary.
// Graph registration, operation admission and provider qualification remain required.
func PrepareRenderInputClosure(ctx context.Context, actor auth.Identity, tenant, task uuid.UUID, binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw []byte, resolver CompositionInputResolver, publisher v2input.Publisher) (v2input.GraphTask, error) {
	if err := ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	if task == uuid.Nil || publisher == nil {
		return v2input.GraphTask{}, v2input.ErrInvalid
	}
	entries, err := ResolveRenderInputs(ctx, actor, tenant, binding, recipeRef, recipeRaw, sourceRaw, resolver)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	// Own the inventory independently of resolver/caller mutation and each cursor.
	entries = append([]v2input.Entry(nil), entries...)
	open := func(ctx context.Context) (v2input.Cursor, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &compositionCursor{entries: entries}, nil
	}
	cursor, err := open(ctx)
	if err != nil {
		return v2input.GraphTask{}, err
	}
	built, err := v2input.BuildInputClosure(ctx, task, cursor, publisher)
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
	if err = ctx.Err(); err != nil {
		return v2input.GraphTask{}, err
	}
	return v2input.GraphTask{ID: task, Built: built, Open: open, Direct: []v2input.Ref{recipeRef}}, nil
}
