package v2render

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"sort"
)

// ResolveRenderInputs delegates occurrence/catalog authority to the scoped
// resolver and validates its inventory before any transport publication.
// Descendants beyond the two verified documents remain the resolver's duty.
func ResolveRenderInputs(ctx context.Context, actor auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, recipeRef v2input.Ref, recipeRaw, sourceRaw []byte, resolver CompositionInputResolver) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if actor.UserID == uuid.Nil || actor.SessionID == uuid.Nil || tenant == uuid.Nil || resolver == nil {
		return nil, v2input.ErrInvalid
	}
	roots, err := RenderInputRoots(binding, recipeRef, recipeRaw, sourceRaw)
	if err != nil {
		return nil, err
	}
	required := append([]v2input.Entry(nil), roots...)
	for _, document := range []struct {
		raw  []byte
		root v2input.Entry
	}{{recipeRaw, v2input.Entry{Ref: recipeRef, Contract: "FrozenRenderRecipe"}}, {sourceRaw, v2input.Entry{Ref: binding.Source, Contract: "WorkingImage"}}} {
		children, err := v2draft.DocumentChildren(document.raw, document.root.Contract, document.root.Ref)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			contract := child.Contract
			switch contract {
			case "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField":
				contract = "NpyArray"
			}
			required = append(required, v2input.Entry{Ref: child.Ref, Contract: contract})
		}
	}
	entries, err := resolver.ResolveTaskInputs(ctx, actor, tenant, roots, binding)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(entries) < len(roots) || len(entries) > 4096 {
		return nil, v2input.ErrInvalid
	}
	entries = append([]v2input.Entry(nil), entries...)
	lookup := make(map[uuid.UUID]v2input.Entry, len(entries))
	for _, entry := range entries {
		if !validLayerArtifactRef(entry.Ref) || len(entry.Contract) < 1 || len(entry.Contract) > 120 {
			return nil, v2input.ErrInvalid
		}
		if _, exists := lookup[entry.Ref.ArtifactID]; exists {
			return nil, v2input.ErrInvalid
		}
		lookup[entry.Ref.ArtifactID] = entry
	}
	for _, entry := range required {
		if lookup[entry.Ref.ArtifactID] != entry {
			return nil, v2input.ErrInvalid
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	return entries, nil
}
